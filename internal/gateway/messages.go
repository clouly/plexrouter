package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/local4a/plexrouter/internal/provider"
	"github.com/local4a/plexrouter/internal/store"
)

// The Anthropic Messages API (/v1/messages), for Claude Code and Anthropic
// SDKs. Anthropic upstreams get the request unchanged; others are reached by
// converting to and from the chat format.

// pingInterval is how often a converted stream sends a keep-alive ping while
// the upstream sends nothing.
var pingInterval = 10 * time.Second

type messagesRequest struct {
	raw    []byte
	body   provider.Object // decoded with json.Number, for pass-through
	model  string          // as the client sent it
	stream bool
	chat   *provider.ChatRequest // converted, for non-Anthropic upstreams
}

func (g *Gateway) readMessagesRequest(w http.ResponseWriter, r *http.Request) (*messagesRequest, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return nil, &apiError{http.StatusRequestEntityTooLarge, "invalid_request", "request body too large or unreadable"}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	mr := &messagesRequest{raw: raw}
	if err := dec.Decode(&mr.body); err != nil {
		return nil, &apiError{http.StatusBadRequest, "invalid_request", "invalid JSON body: " + err.Error()}
	}
	mr.model, _ = mr.body["model"].(string)
	mr.stream, _ = mr.body["stream"].(bool)
	if mr.model == "" {
		return nil, &apiError{http.StatusBadRequest, "invalid_request", "model: field required"}
	}
	if mr.chat, _, err = provider.AnthropicToChat(raw); err != nil {
		return nil, &apiError{http.StatusBadRequest, "invalid_request", "invalid request: " + err.Error()}
	}
	return mr, nil
}

func (g *Gateway) handleMessages(w http.ResponseWriter, r *http.Request) {
	c, err := g.authenticate(r)
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	c.api = "messages"
	mr, err := g.readMessagesRequest(w, r)
	if err == nil {
		err = g.checkLimits(c)
	}
	var cands []candidate
	if err == nil {
		var slugs []string
		if slugs, err = c.allowedSlugs([]string{mr.model}); err == nil {
			cands, err = g.candidates(r.Context(), slugs)
		}
	}
	if err != nil {
		writeAnthropicError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), g.options().Timeout)
	defer cancel()
	err = g.dispatch(ctx, c, cands, mr.stream, func(rt *route) error {
		if mp, ok := rt.adapter.(provider.MessagesProvider); ok {
			return g.messagesPassthrough(ctx, w, r, c, rt, mp, mr)
		}
		return g.messagesViaChat(ctx, w, c, rt, mr)
	})
	if err != nil {
		writeAnthropicError(w, err)
	}
}

// anthropicTokenUsage reads a Messages API usage object.
func anthropicTokenUsage(v any, into *store.TokenUsage) {
	u, ok := v.(map[string]any)
	if !ok {
		return
	}
	n := func(k string) (int, bool) {
		num, ok := u[k].(json.Number)
		if !ok {
			return 0, false
		}
		i, _ := num.Int64()
		return int(i), true
	}
	in, hasIn := n("input_tokens")
	read, _ := n("cache_read_input_tokens")
	write, _ := n("cache_creation_input_tokens")
	if hasIn {
		into.Prompt, into.CacheRead, into.CacheWrite = in+read+write, read, write
	}
	if out, ok := n("output_tokens"); ok {
		into.Completion = out
	}
}

func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func (g *Gateway) messagesPassthrough(ctx context.Context, w http.ResponseWriter, r *http.Request, c *caller, rt *route,
	mp provider.MessagesProvider, mr *messagesRequest) error {
	resp, err := mp.Messages(ctx, mr.raw, rt.cand.endpoint.UpstreamModel, r.Header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var usage store.TokenUsage

	if !mr.stream {
		dec := json.NewDecoder(resp.Body)
		dec.UseNumber()
		var obj provider.Object
		if err := dec.Decode(&obj); err != nil {
			return &provider.Error{Status: http.StatusBadGateway, Message: "invalid JSON from upstream: " + err.Error()}
		}
		anthropicTokenUsage(obj["usage"], &usage)
		obj["model"] = mr.model
		writeJSON(w, obj)
		g.record(c, rt, usage, rt.cand.model.Charge(usage), false, nil)
		return nil
	}

	// Relay the event stream line by line, reading usage on the way and
	// presenting the model name the client asked for.
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	rd := bufio.NewReaderSize(resp.Body, 64<<10)
	var streamErr error
	finished := false
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			if data, ok := bytes.CutPrefix(bytes.TrimRight(line, "\r\n"), []byte("data:")); ok {
				var ev provider.Object
				dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(data)))
				dec.UseNumber()
				if dec.Decode(&ev) == nil {
					switch ev["type"] {
					case "message_start":
						if msg, ok := ev["message"].(map[string]any); ok {
							anthropicTokenUsage(msg["usage"], &usage)
							msg["model"] = mr.model
							b, _ := json.Marshal(ev)
							line = append(append([]byte("data: "), b...), '\n')
						}
					case "message_delta":
						anthropicTokenUsage(ev["usage"], &usage)
					case "message_stop":
						finished = true
					case "error":
						streamErr = errors.New("upstream stream error: " + string(bytes.TrimSpace(data)))
					}
				}
			}
			if _, werr := w.Write(line); werr != nil {
				streamErr = errors.New("client disconnected")
				break
			}
			if flusher != nil && len(bytes.TrimSpace(line)) == 0 {
				flusher.Flush()
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && streamErr == nil {
				streamErr = errors.New("stream interrupted: " + err.Error())
			}
			break
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	if streamErr == nil && !finished {
		streamErr = errors.New("stream ended before message_stop")
	}
	g.record(c, rt, usage, rt.cand.model.Charge(usage), true, streamErr)
	return nil
}

func writeAnthropicEvent(w io.Writer, ev provider.AnthropicEvent) error {
	b, err := json.Marshal(ev.Data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, b)
	return err
}

func (g *Gateway) messagesViaChat(ctx context.Context, w http.ResponseWriter, c *caller, rt *route, mr *messagesRequest) error {
	req := mr.chat
	promptEstimate := estimatePrompt(req)
	if !mr.stream {
		resp, err := rt.adapter.Chat(ctx, req, rt.cand.endpoint.UpstreamModel)
		if err != nil {
			return err
		}
		usage := store.TokenUsage{Prompt: promptEstimate, Completion: estimateTokens(len(contentOf(resp)))}
		if u := provider.UsageOf(resp); u != nil {
			usage = tokenUsage(u)
		} else {
			resp["usage"] = map[string]any{"prompt_tokens": usage.Prompt, "completion_tokens": usage.Completion}
		}
		writeJSON(w, provider.ChatToAnthropic(resp, mr.model))
		g.record(c, rt, usage, rt.cand.model.Charge(usage), false, nil)
		return nil
	}

	st, err := rt.adapter.ChatStream(ctx, req, rt.cand.endpoint.UpstreamModel)
	if err != nil {
		return err
	}
	defer st.Close()
	setSSEHeaders(w)
	flusher, _ := w.(http.Flusher)
	conv := provider.NewAnthropicStreamConverter(mr.model)
	var outBytes int
	var streamErr error
	write := func(evs []provider.AnthropicEvent) bool {
		for _, ev := range evs {
			if err := writeAnthropicEvent(w, ev); err != nil {
				streamErr = errors.New("client disconnected")
				return false
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	// Read chunks in the background so the client gets ping events while the
	// upstream is silent (e.g. reasoning before the first token); Claude Code
	// drops a stream that goes quiet for too long.
	type next struct {
		ch  provider.Object
		err error
	}
	chunks := make(chan next)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			ch, err := st.Next()
			select {
			case chunks <- next{ch, err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
loop:
	for {
		select {
		case <-ping.C:
			if !write([]provider.AnthropicEvent{{Name: "ping", Data: provider.Object{"type": "ping"}}}) {
				break loop
			}
		case n := <-chunks:
			if errors.Is(n.err, io.EOF) {
				write(conv.Finish())
				break loop
			}
			if n.err != nil {
				streamErr = n.err
				write([]provider.AnthropicEvent{{Name: "error", Data: provider.Object{"type": "error",
					"error": provider.Object{"type": "api_error", "message": n.err.Error()}}}})
				break loop
			}
			outBytes += len(contentOf(n.ch))
			if !write(conv.Chunk(n.ch)) {
				break loop
			}
			ping.Reset(pingInterval)
		}
	}
	usage := store.TokenUsage{Prompt: promptEstimate, Completion: estimateTokens(outBytes)}
	if u := conv.Usage(); u != nil {
		usage = tokenUsage(u)
	}
	g.record(c, rt, usage, rt.cand.model.Charge(usage), true, streamErr)
	return nil
}

// handleCountTokens serves /v1/messages/count_tokens: exact for Anthropic
// upstreams, estimated otherwise. It is neither logged nor billed.
func (g *Gateway) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	c, err := g.authenticate(r)
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	mr, err := g.readMessagesRequest(w, r)
	var cands []candidate
	if err == nil {
		var slugs []string
		if slugs, err = c.allowedSlugs([]string{mr.model}); err == nil {
			cands, err = g.candidates(r.Context(), slugs)
		}
	}
	if err != nil {
		writeAnthropicError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), g.options().Timeout)
	defer cancel()
	for _, cand := range g.health.order(cands) {
		p, keys, err := g.targets(ctx, cand.endpoint.ProviderID)
		if err != nil {
			continue
		}
		ad, err := g.adapterWith(p, keys[0])
		if err != nil {
			continue
		}
		if mp, ok := ad.(provider.MessagesProvider); ok {
			body := make(provider.Object, len(mr.body))
			for k, v := range mr.body {
				if k != "stream" && k != "max_tokens" {
					body[k] = v
				}
			}
			if res, err := mp.CountTokens(ctx, body, cand.endpoint.UpstreamModel, r.Header); err == nil {
				writeJSON(w, res)
				return
			}
		}
		break
	}
	n := estimatePrompt(mr.chat)
	for _, t := range mr.chat.Tools {
		n += estimateTokens(len(t.Function.Name) + len(t.Function.Description) + len(t.Function.Parameters))
	}
	writeJSON(w, map[string]any{"input_tokens": n})
}
