package gateway

import (
	"context"
	"regexp"
	"strings"

	"github.com/local4a/plexrouter/internal/store"
)

var datedSuffix = regexp.MustCompile(`-(\d{8}|\d{4}-\d{2}-\d{2})$`)

// normalizeName reduces a model name to compare across naming styles:
// "anthropic/claude-sonnet-4.5" and "claude-sonnet-4-5-20250929" both become
// "claude-sonnet-4-5".
func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return datedSuffix.ReplaceAllString(strings.ReplaceAll(s, ".", "-"), "")
}

// resolveAlias finds the model a client means when it uses a provider's own
// model name instead of our slug (Claude Code sends "claude-sonnet-4-5-…").
// A model with an endpoint of exactly that upstream name wins; otherwise
// names are compared after normalizeName.
func (g *Gateway) resolveAlias(ctx context.Context, name string) (*store.Model, error) {
	models, err := g.store.ListModels(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, m := range models {
		for _, e := range m.Endpoints {
			if e.UpstreamModel == name {
				return g.store.RouteModel(ctx, m.Slug)
			}
		}
	}
	want := normalizeName(name)
	for _, m := range models {
		if len(m.Endpoints) > 0 && normalizeName(m.Slug) == want {
			return g.store.RouteModel(ctx, m.Slug)
		}
	}
	return nil, store.ErrNotFound
}
