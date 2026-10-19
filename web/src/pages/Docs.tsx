import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useAuth } from "../auth";
import { useT, type MessageKey } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { gatewayBaseUrl } from "../lib/gateway";
import { PageHeader } from "../components/Card";
import { CodeBlock, CodeTabs, InlineCode } from "../components/CodeBlock";
import { CopyButton } from "../components/CopyButton";

const MODEL = "openai/gpt-4o-mini";
const FALLBACK = ["anthropic/claude-3.5-haiku", "google/gemini-2.0-flash"];
const EMBED_MODEL = "openai/text-embedding-3-small";
/** Used in the Anthropic examples when the catalog has no Claude model (or can't be loaded). */
const CLAUDE_MODEL = "anthropic/claude-sonnet-4.5";

/** A Claude model from the live catalog for the Anthropic examples, else any chat model, else the default. */
function pickClaudeModel(ids: string[]): string {
  return ids.find((id) => /claude/i.test(id)) ?? ids.find((id) => !/embed/i.test(id)) ?? CLAUDE_MODEL;
}

function examples(base: string, origin: string, claude: string) {
  const chat = [
    {
      label: "curl",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${MODEL}",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello!"}
    ]
  }'`,
    },
    {
      label: "Python",
      code: `# pip install openai
import os
from openai import OpenAI

client = OpenAI(
    base_url="${base}",
    api_key=os.environ["PLEXROUTER_API_KEY"],  # sk-plex-...
)

resp = client.chat.completions.create(
    model="${MODEL}",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(resp.choices[0].message.content)
print(resp.usage)  # includes "cost" in USD`,
    },
    {
      label: "Node.js",
      code: `// npm install openai
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${base}",
  apiKey: process.env.PLEXROUTER_API_KEY, // sk-plex-...
});

const resp = await client.chat.completions.create({
  model: "${MODEL}",
  messages: [{ role: "user", content: "Hello!" }],
});
console.log(resp.choices[0].message.content);
console.log(resp.usage); // includes "cost" in USD`,
    },
  ];

  const stream = [
    {
      label: "curl",
      code: `curl -N ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${MODEL}",
    "stream": true,
    "messages": [{"role": "user", "content": "Write a haiku about routers."}]
  }'`,
    },
    {
      label: "Python",
      code: `stream = client.chat.completions.create(
    model="${MODEL}",
    messages=[{"role": "user", "content": "Write a haiku about routers."}],
    stream=True,
)
for chunk in stream:
    if chunk.choices and chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)`,
    },
    {
      label: "Node.js",
      code: `const stream = await client.chat.completions.create({
  model: "${MODEL}",
  messages: [{ role: "user", content: "Write a haiku about routers." }],
  stream: true,
});
for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? "");
}`,
    },
  ];

  const fallback = [
    {
      label: "curl",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${MODEL}",
    "models": ${JSON.stringify(FALLBACK)},
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`,
    },
    {
      label: "Python",
      code: `resp = client.chat.completions.create(
    model="${MODEL}",
    messages=[{"role": "user", "content": "Hello!"}],
    # Non-standard fields go through extra_body with the openai SDK.
    extra_body={"models": ${JSON.stringify(FALLBACK)}},
)
print(resp.model)  # the model that actually answered`,
    },
    {
      label: "Node.js",
      code: `const resp = await client.chat.completions.create({
  model: "${MODEL}",
  messages: [{ role: "user", content: "Hello!" }],
  // Extra body fields are passed through as-is.
  // @ts-expect-error -- "models" is a PlexRouter extension
  models: ${JSON.stringify(FALLBACK)},
});
console.log(resp.model); // the model that actually answered`,
    },
  ];

  const embeddings = [
    {
      label: "curl",
      code: `curl ${base}/embeddings \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${EMBED_MODEL}",
    "input": ["The quick brown fox", "jumps over the lazy dog"]
  }'`,
    },
    {
      label: "Python",
      code: `emb = client.embeddings.create(
    model="${EMBED_MODEL}",
    input=["The quick brown fox", "jumps over the lazy dog"],
)
print(len(emb.data[0].embedding))`,
    },
    {
      label: "Node.js",
      code: `const emb = await client.embeddings.create({
  model: "${EMBED_MODEL}",
  input: ["The quick brown fox", "jumps over the lazy dog"],
});
console.log(emb.data[0].embedding.length);`,
    },
  ];

  const models = `curl ${base}/models

# Response (OpenAI-style list; prices are USD per token, as strings)
{
  "object": "list",
  "data": [
    {
      "id": "${MODEL}",
      "object": "model",
      "created": 1760870400,
      "owned_by": "openai",
      "name": "GPT-4o mini",
      "context_length": 128000,
      "pricing": {
        "prompt": "0.00000015",
        "completion": "0.0000006",
        "input_cache_read": "0.000000075"
      }
    }
  ]
}`;

  const key = `curl ${base}/key \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY"

# Response — usage, limit and balance are in USD; limit is null when unlimited
{
  "data": {
    "name": "my-app",
    "usage": 1.2345,
    "limit": 10,
    "balance": 42.5
  }
}`;

  const claudeCode = `# Point Claude Code at this gateway (it appends /v1/messages itself)
export ANTHROPIC_BASE_URL="${origin}"
export ANTHROPIC_AUTH_TOKEN="sk-plex-..."

# Optional: pick the model — any model ID from the catalog, from any provider
export ANTHROPIC_MODEL="${claude}"
# export ANTHROPIC_MODEL="${MODEL}"
# Model for background tasks (titles, summaries); defaults to the main model
# export ANTHROPIC_DEFAULT_HAIKU_MODEL="${claude}"

# Optional: show this gateway's Claude models in the /model picker
# export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1

claude`;

  const messages = [
    {
      label: "curl",
      code: `curl ${origin}/v1/messages \\
  -H "x-api-key: $PLEXROUTER_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "${claude}",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`,
    },
    {
      label: "Python",
      code: `# pip install anthropic
import os
import anthropic

client = anthropic.Anthropic(
    base_url="${origin}",
    api_key=os.environ["PLEXROUTER_API_KEY"],  # sk-plex-...
)

message = client.messages.create(
    model="${claude}",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello!"}],
)
print(message.content[0].text)
print(message.usage)`,
    },
    {
      label: "TypeScript",
      code: `// npm install @anthropic-ai/sdk
import Anthropic from "@anthropic-ai/sdk";

const client = new Anthropic({
  baseURL: "${origin}",
  apiKey: process.env.PLEXROUTER_API_KEY, // sk-plex-...
});

const message = await client.messages.create({
  model: "${claude}",
  max_tokens: 1024,
  messages: [{ role: "user", content: "Hello!" }],
});
for (const block of message.content) {
  if (block.type === "text") console.log(block.text);
}`,
    },
  ];

  const messagesStream = [
    {
      label: "Python",
      code: `with client.messages.stream(
    model="${claude}",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Write a haiku about routers."}],
) as stream:
    for text in stream.text_stream:
        print(text, end="", flush=True)`,
    },
    {
      label: "TypeScript",
      code: `const stream = client.messages.stream({
  model: "${claude}",
  max_tokens: 1024,
  messages: [{ role: "user", content: "Write a haiku about routers." }],
});
stream.on("text", (text) => process.stdout.write(text));
await stream.finalMessage();`,
    },
    {
      label: "curl",
      code: `curl -N ${origin}/v1/messages \\
  -H "x-api-key: $PLEXROUTER_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "${claude}",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role": "user", "content": "Write a haiku about routers."}]
  }'`,
    },
  ];

  const countTokens = `curl ${origin}/v1/messages/count_tokens \\
  -H "x-api-key: $PLEXROUTER_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "content-type: application/json" \\
  -d '{
    "model": "${claude}",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Response
{ "input_tokens": 9 }`;

  const reasoning = [
    {
      label: "reasoning_effort",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${claude}",
    "reasoning_effort": "high",
    "messages": [{"role": "user", "content": "Which is larger, 9.11 or 9.9?"}]
  }'`,
    },
    {
      label: "reasoning",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${claude}",
    "reasoning": {"effort": "medium", "max_tokens": 4000, "exclude": false},
    "messages": [{"role": "user", "content": "Which is larger, 9.11 or 9.9?"}]
  }'`,
    },
    {
      label: "Python",
      code: `resp = client.chat.completions.create(
    model="${claude}",
    messages=[{"role": "user", "content": "Which is larger, 9.11 or 9.9?"}],
    reasoning_effort="high",  # or extra_body={"reasoning": {"max_tokens": 4000}}
)
msg = resp.choices[0].message
print(getattr(msg, "reasoning", None))  # the model's reasoning text
print(msg.content)                      # the final answer`,
    },
  ];

  const reasoningResponse = `{
  "choices": [{
    "message": {
      "role": "assistant",
      "reasoning": "Compare the decimals digit by digit: 9.9 = 9.90 and 9.90 > 9.11…",
      "content": "9.9 is larger than 9.11."
    }
  }]
}
// Streaming: reasoning arrives in choices[0].delta.reasoning before delta.content`;

  const caching = [
    {
      label: "curl",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${claude}",
    "messages": [
      {
        "role": "system",
        "content": [
          {
            "type": "text",
            "text": "You answer questions about this handbook: <long document…>",
            "cache_control": {"type": "ephemeral"}
          }
        ]
      },
      {"role": "user", "content": "What is the refund policy?"}
    ]
  }'`,
    },
    {
      label: "Python",
      code: `HANDBOOK = open("handbook.md").read()  # long, reused prompt prefix

resp = client.chat.completions.create(
    model="${claude}",
    messages=[
        {
            "role": "system",
            "content": [
                {
                    "type": "text",
                    "text": "You answer questions about this handbook:\\n" + HANDBOOK,
                    "cache_control": {"type": "ephemeral"},
                }
            ],
        },
        {"role": "user", "content": "What is the refund policy?"},
    ],
)
print(resp.usage.prompt_tokens_details)`,
    },
  ];

  const cachingUsage = `"usage": {
  "prompt_tokens": 12045,          // includes cached tokens
  "completion_tokens": 230,
  "total_tokens": 12275,
  "prompt_tokens_details": {
    "cached_tokens": 12000,        // read from the cache (billed at the cache read price)
    "cache_write_tokens": 0        // written to the cache (billed at the cache write price)
  },
  "cost": 0.004935
}`;

  const structured = [
    {
      label: "curl",
      code: `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${MODEL}",
    "messages": [{"role": "user", "content": "Extract: Ada Lovelace, born 1815 in London."}],
    "response_format": {
      "type": "json_schema",
      "json_schema": {
        "name": "person",
        "schema": {
          "type": "object",
          "properties": {
            "name": {"type": "string"},
            "birth_year": {"type": "integer"},
            "city": {"type": "string"}
          },
          "required": ["name", "birth_year", "city"],
          "additionalProperties": false
        }
      }
    }
  }'`,
    },
    {
      label: "Python",
      code: `import json

resp = client.chat.completions.create(
    model="${MODEL}",
    messages=[{"role": "user", "content": "Extract: Ada Lovelace, born 1815 in London."}],
    response_format={
        "type": "json_schema",
        "json_schema": {
            "name": "person",
            "schema": {
                "type": "object",
                "properties": {
                    "name": {"type": "string"},
                    "birth_year": {"type": "integer"},
                    "city": {"type": "string"},
                },
                "required": ["name", "birth_year", "city"],
                "additionalProperties": False,
            },
        },
    },
)
person = json.loads(resp.choices[0].message.content)
print(person["name"], person["birth_year"])`,
    },
  ];

  return {
    chat,
    stream,
    fallback,
    embeddings,
    models,
    key,
    claudeCode,
    messages,
    messagesStream,
    countTokens,
    reasoning,
    reasoningResponse,
    caching,
    cachingUsage,
    structured,
  };
}

const toc: { id: string; label: MessageKey }[] = [
  { id: "overview", label: "docs.overview" },
  { id: "chat", label: "docs.chat" },
  { id: "streaming", label: "docs.streaming" },
  { id: "fallback", label: "docs.fallback" },
  { id: "claude-code", label: "docs.claudeCode" },
  { id: "messages", label: "docs.messages" },
  { id: "reasoning", label: "docs.reasoning" },
  { id: "caching", label: "docs.caching" },
  { id: "structured", label: "docs.structured" },
  { id: "models", label: "docs.listModels" },
  { id: "key", label: "docs.keyInfo" },
  { id: "embeddings", label: "docs.embeddings" },
  { id: "limits", label: "docs.limits" },
];

const LIMIT_ERRORS: { status: string; code: string; text: MessageKey }[] = [
  { status: "429", code: "rate_limited", text: "docs.limitsRateLimited" },
  { status: "403", code: "model_not_allowed", text: "docs.limitsModelNotAllowed" },
];

function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-20 border-t border-line pt-6 first:border-t-0 first:pt-0">
      <h2 className="mb-2 text-base font-semibold tracking-tight">
        <a href={`#${id}`} className="hover:text-accent">
          {title}
        </a>
      </h2>
      <div className="flex flex-col gap-3 text-[13.5px] leading-relaxed text-muted [&_strong]:text-fg">{children}</div>
    </section>
  );
}

/** Renders `backticked` parts of a translated string as inline code. */
function Rich({ text }: { text: string }) {
  return (
    <>
      {text.split(/`([^`]+)`/).map((part, i) => (i % 2 ? <InlineCode key={i}>{part}</InlineCode> : part))}
    </>
  );
}

function Endpoint({ method, path }: { method: string; path: string }) {
  return (
    <div className="inline-flex w-fit max-w-full items-center gap-2 rounded-md border border-line bg-surface px-2 py-1 font-mono text-xs text-fg">
      <span className="font-semibold text-accent">{method}</span>
      <span className="truncate">{path}</span>
    </div>
  );
}

export function DocsPage() {
  const t = useT();
  const { config, user } = useAuth();
  useDocumentTitle(t("nav.docs"), config?.name);
  const base = gatewayBaseUrl();
  const origin = window.location.origin;
  // Best effort: real model IDs make the Anthropic examples copy-pasteable.
  const catalog = useAsync(() => api.models().then((r) => r.data.map((m) => m.id)).catch(() => [] as string[]), []);
  const claude = pickClaudeModel(catalog.data ?? []);
  const ex = examples(base, origin, claude);

  return (
    <>
      <PageHeader title={t("docs.title")} description={t("docs.desc", { name: config?.name || "PlexRouter" })} />
      <div className="flex gap-8">
        <div className="flex min-w-0 flex-1 flex-col gap-8">
          <Section id="overview" title={t("docs.overview")}>
            <p>{t("docs.overviewText")}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="min-w-0">
                <div className="mb-1 text-xs font-medium text-fg">{t("docs.baseUrl")}</div>
                <div className="flex items-center gap-1 rounded-lg border border-line bg-code py-1 pr-1 pl-3">
                  <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-fg">{base}</code>
                  <CopyButton text={base} />
                </div>
                <p className="mt-1 text-xs">{t("docs.baseUrlAlt", { url: `${window.location.origin}/api/v1` })}</p>
              </div>
              <div className="min-w-0">
                <div className="mb-1 text-xs font-medium text-fg">{t("docs.authHeader")}</div>
                <div className="flex items-center gap-1 rounded-lg border border-line bg-code py-1 pr-1 pl-3">
                  <code className="min-w-0 flex-1 truncate font-mono text-[13px] text-fg">Authorization: Bearer sk-plex-...</code>
                  <CopyButton text="Authorization: Bearer sk-plex-..." />
                </div>
                <p className="mt-1 text-xs">
                  {user ? (
                    <Link to="/keys" className="text-accent hover:underline">
                      {t("docs.getKey")}
                    </Link>
                  ) : (
                    <Link to="/login" className="text-accent hover:underline">
                      {t("docs.loginForKey")}
                    </Link>
                  )}
                </p>
              </div>
            </div>
            <p>
              {t("docs.modelSlugs")}{" "}
              <Link to="/models" className="text-accent hover:underline">
                {t("docs.browseModels")}
              </Link>
            </p>
            <p>
              <Rich text={t("docs.modelAliases")} />
            </p>
            <CodeBlock lang="bash" code={`export PLEXROUTER_API_KEY="sk-plex-..."`} />
          </Section>

          <Section id="chat" title={t("docs.chat")}>
            <Endpoint method="POST" path="/v1/chat/completions" />
            <p>{t("docs.chatText")}</p>
            <CodeTabs tabs={ex.chat} />
            <p>
              {t("docs.costText")} <InlineCode>usage.cost</InlineCode>
            </p>
          </Section>

          <Section id="streaming" title={t("docs.streaming")}>
            <p>
              {t("docs.streamingText")} <InlineCode>"stream": true</InlineCode>
            </p>
            <CodeTabs tabs={ex.stream} />
          </Section>

          <Section id="fallback" title={t("docs.fallback")}>
            <p>
              {t("docs.fallbackText1")} <InlineCode>models</InlineCode> {t("docs.fallbackText2")}
            </p>
            <CodeTabs tabs={ex.fallback} />
          </Section>

          <Section id="claude-code" title={t("docs.claudeCode")}>
            <p>
              <Rich text={t("docs.claudeCodeText")} />
            </p>
            <CodeBlock lang="bash" code={ex.claudeCode} />
            <ul className="list-disc space-y-1 pl-5">
              <li>
                <Rich text={t("docs.claudeCodeModels")} />
              </li>
              <li>
                <Rich text={t("docs.claudeCodeAliases")} />
              </li>
              <li>
                <Rich text={t("docs.claudeCodeOtherModels")} />
              </li>
              <li>
                <Rich text={t("docs.claudeCodeBilling")} />
              </li>
            </ul>
          </Section>

          <Section id="messages" title={t("docs.messages")}>
            <div className="flex flex-wrap gap-2">
              <Endpoint method="POST" path="/v1/messages" />
              <Endpoint method="POST" path="/v1/messages/count_tokens" />
            </div>
            <p>
              <Rich text={t("docs.messagesText")} />
            </p>
            <p>
              <Rich text={t("docs.messagesAuth")} />
            </p>
            <CodeTabs tabs={ex.messages} />
            <p>
              <Rich text={t("docs.messagesStreaming")} />
            </p>
            <CodeTabs tabs={ex.messagesStream} />
            <p>
              <Rich text={t("docs.countTokensText")} />
            </p>
            <CodeBlock lang="bash" code={ex.countTokens} />
          </Section>

          <Section id="reasoning" title={t("docs.reasoning")}>
            <p>
              <Rich text={t("docs.reasoningText")} />
            </p>
            <CodeTabs tabs={ex.reasoning} />
            <p>
              <Rich text={t("docs.reasoningOutput")} />
            </p>
            <CodeBlock lang="json" code={ex.reasoningResponse} />
          </Section>

          <Section id="caching" title={t("docs.caching")}>
            <p>
              <Rich text={t("docs.cachingText")} />
            </p>
            <CodeTabs tabs={ex.caching} />
            <p>
              <Rich text={t("docs.cachingUsage")} />
            </p>
            <CodeBlock lang="json" code={ex.cachingUsage} />
            <p>
              <Rich text={t("docs.cachingBilling")} />{" "}
              <Link to="/models" className="text-accent hover:underline">
                {t("docs.browseModels")}
              </Link>
            </p>
          </Section>

          <Section id="structured" title={t("docs.structured")}>
            <p>
              <Rich text={t("docs.structuredText")} />
            </p>
            <CodeTabs tabs={ex.structured} />
          </Section>

          <Section id="models" title={t("docs.listModels")}>
            <Endpoint method="GET" path="/v1/models" />
            <p>{t("docs.listModelsText")}</p>
            <p>
              <Rich text={t("docs.listModelsCache")} />
            </p>
            <CodeBlock lang="bash" code={ex.models} />
          </Section>

          <Section id="key" title={t("docs.keyInfo")}>
            <Endpoint method="GET" path="/v1/key" />
            <p>{t("docs.keyInfoText")}</p>
            <CodeBlock lang="bash" code={ex.key} />
          </Section>

          <Section id="embeddings" title={t("docs.embeddings")}>
            <Endpoint method="POST" path="/v1/embeddings" />
            <p>{t("docs.embeddingsText")}</p>
            <CodeTabs tabs={ex.embeddings} />
          </Section>

          <Section id="limits" title={t("docs.limits")}>
            <p>{t("docs.limitsText")}</p>
            <div className="overflow-x-auto rounded-lg border border-line">
              <table className="w-full min-w-[480px] border-collapse text-left text-[13px]">
                <thead className="border-b border-line bg-subtle/60 text-xs text-muted">
                  <tr>
                    <th className="px-3 py-2 font-medium">{t("docs.limitsStatus")}</th>
                    <th className="px-3 py-2 font-medium">{t("docs.limitsCode")}</th>
                    <th className="px-3 py-2 font-medium">{t("docs.limitsMeaning")}</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-line">
                  {LIMIT_ERRORS.map((e) => (
                    <tr key={e.code}>
                      <td className="px-3 py-2 align-top font-mono text-xs text-fg">{e.status}</td>
                      <td className="px-3 py-2 align-top">
                        <InlineCode>{e.code}</InlineCode>
                      </td>
                      <td className="px-3 py-2 align-top">{t(e.text)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p>{t("docs.limitsRetryAfter")}</p>
            <CodeBlock lang="http" code={`HTTP/1.1 429 Too Many Requests\nRetry-After: 12\nContent-Type: application/json`} />
          </Section>
        </div>

        <nav className="sticky top-20 hidden w-44 shrink-0 self-start xl:block" aria-label={t("docs.onThisPage")}>
          <div className="mb-2 text-[11px] font-semibold tracking-wider text-muted uppercase">{t("docs.onThisPage")}</div>
          <ul className="flex flex-col gap-1 border-l border-line text-[13px]">
            {toc.map((s) => (
              <li key={s.id}>
                <a href={`#${s.id}`} className="-ml-px block border-l border-transparent py-0.5 pl-3 text-muted hover:border-fg hover:text-fg">
                  {t(s.label)}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </>
  );
}
