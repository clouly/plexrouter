import type { ReactNode } from "react";
import { Link } from "react-router-dom";
import { useAuth } from "../auth";
import { useT, type MessageKey } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { gatewayBaseUrl } from "../lib/gateway";
import { PageHeader } from "../components/Card";
import { CodeBlock, CodeTabs, InlineCode } from "../components/CodeBlock";
import { CopyButton } from "../components/CopyButton";

const MODEL = "openai/gpt-4o-mini";
const FALLBACK = ["anthropic/claude-3.5-haiku", "google/gemini-2.0-flash"];
const EMBED_MODEL = "openai/text-embedding-3-small";

function examples(base: string) {
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
      "pricing": { "prompt": "0.00000015", "completion": "0.0000006" }
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

  return { chat, stream, fallback, embeddings, models, key };
}

const toc: { id: string; label: MessageKey }[] = [
  { id: "overview", label: "docs.overview" },
  { id: "chat", label: "docs.chat" },
  { id: "streaming", label: "docs.streaming" },
  { id: "fallback", label: "docs.fallback" },
  { id: "models", label: "docs.listModels" },
  { id: "key", label: "docs.keyInfo" },
  { id: "embeddings", label: "docs.embeddings" },
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
  const ex = examples(base);

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

          <Section id="models" title={t("docs.listModels")}>
            <Endpoint method="GET" path="/v1/models" />
            <p>{t("docs.listModelsText")}</p>
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
