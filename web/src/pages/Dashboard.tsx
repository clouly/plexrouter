import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api";
import { useAuth } from "../auth";
import { useT } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { formatInt, formatTokens, formatUSD } from "../lib/format";
import { gatewayBaseUrl } from "../lib/gateway";
import { Card, PageHeader, StatCard } from "../components/Card";
import { BarChart } from "../components/BarChart";
import { CodeBlock } from "../components/CodeBlock";
import { CopyButton } from "../components/CopyButton";
import { ErrorState } from "../components/EmptyState";
import { Icon } from "../components/Icon";
import { Segmented } from "../components/Segmented";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../components/Table";

export function DashboardPage() {
  const t = useT();
  const { user, config } = useAuth();
  useDocumentTitle(t("nav.dashboard"), config?.name);
  const [days, setDays] = useState(7);
  const stats = useAsync(() => api.usageStats(days), [days]);
  const s = stats.data;
  const baseUrl = gatewayBaseUrl();
  const loading = stats.loading && !s;

  const topModels = [...(s?.by_model ?? [])].sort((a, b) => b.cost - a.cost || b.requests - a.requests).slice(0, 10);
  const exampleModel = topModels[0]?.model || "openai/gpt-4o-mini";

  const snippet = `curl ${baseUrl}/chat/completions \\
  -H "Authorization: Bearer $PLEXROUTER_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${exampleModel}",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`;

  return (
    <>
      <PageHeader
        title={t("dashboard.title")}
        description={user ? t("dashboard.welcome", { name: user.name || user.email }) : undefined}
        actions={
          <Segmented
            label={t("dashboard.range")}
            value={days}
            onChange={setDays}
            options={[
              { value: 7, label: t("dashboard.last7") },
              { value: 30, label: t("dashboard.last30") },
            ]}
          />
        }
      />

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <StatCard
          label={t("dashboard.balance")}
          value={formatUSD(user?.balance)}
          hint={
            <Link to="/credits" className="text-accent hover:underline">
              {t("dashboard.addCredits")}
            </Link>
          }
        />
        <StatCard label={t("dashboard.spend")} value={formatUSD(s?.total_cost)} hint={t("dashboard.inLastDays", { n: days })} loading={loading} />
        <StatCard label={t("dashboard.requests")} value={formatInt(s?.total_requests)} hint={t("dashboard.inLastDays", { n: days })} loading={loading} />
        <StatCard
          label={t("dashboard.tokens")}
          value={formatTokens(s?.total_tokens)}
          hint={s ? t("dashboard.tokensExact", { n: formatInt(s.total_tokens) }) : undefined}
          loading={loading}
        />
      </div>

      <div className="mt-4 grid gap-4 lg:grid-cols-5">
        <Card className="lg:col-span-3" title={t("dashboard.dailySpend")} description={t("dashboard.dailySpendDesc", { n: days })}>
          {stats.error ? (
            <ErrorState error={stats.error} onRetry={stats.reload} />
          ) : loading ? (
            <div className="h-[212px] animate-pulse rounded-lg bg-subtle" />
          ) : (
            <BarChart data={(s?.daily ?? []).map((d) => ({ label: d.date, value: d.cost }))} format={(v) => formatUSD(v)} emptyText={t("dashboard.noUsage")} />
          )}
        </Card>

        <Card className="lg:col-span-2" title={t("dashboard.quickStart")} description={t("dashboard.quickStartDesc")}>
          <div className="flex flex-col gap-3">
            <div>
              <div className="mb-1 text-xs font-medium text-muted">{t("docs.baseUrl")}</div>
              <div className="flex items-center gap-1 rounded-lg border border-line bg-code py-1 pr-1 pl-3">
                <code className="min-w-0 flex-1 truncate font-mono text-[13px]">{baseUrl}</code>
                <CopyButton text={baseUrl} />
              </div>
            </div>
            <CodeBlock code={snippet} lang="bash" />
            <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
              <Link to="/keys?new=1" className="inline-flex items-center gap-1 font-medium text-accent hover:underline">
                <Icon name="key" className="size-3.5" />
                {t("dashboard.createKey")}
              </Link>
              <Link to="/docs" className="inline-flex items-center gap-1 font-medium text-accent hover:underline">
                <Icon name="book" className="size-3.5" />
                {t("dashboard.readDocs")}
              </Link>
            </div>
          </div>
        </Card>
      </div>

      <Card
        flush
        className="mt-4"
        title={t("dashboard.topModels")}
        description={t("dashboard.topModelsDesc", { n: days })}
        actions={
          <Link to="/usage" className="inline-flex items-center gap-1 text-[13px] font-medium text-accent hover:underline">
            {t("dashboard.viewLogs")} <Icon name="arrowRight" className="size-3.5" />
          </Link>
        }
      >
        <Table minWidth={560}>
          <THead>
            <Th>{t("logs.model")}</Th>
            <Th align="right">{t("dashboard.requests")}</Th>
            <Th align="right">{t("dashboard.tokens")}</Th>
            <Th align="right">{t("logs.cost")}</Th>
            <Th className="w-40">{t("dashboard.share")}</Th>
          </THead>
          <TBody>
            <TableState colSpan={5} loading={stats.loading} error={stats.error} empty={topModels.length === 0} onRetry={stats.reload} emptyText={t("dashboard.noUsage")} />
            {!stats.error &&
              topModels.map((m) => {
                const share = s && s.total_cost > 0 ? (m.cost / s.total_cost) * 100 : 0;
                return (
                  <Tr key={m.model}>
                    <Td className="font-mono text-xs">{m.model}</Td>
                    <Td align="right">{formatInt(m.requests)}</Td>
                    <Td align="right">{formatTokens(m.tokens)}</Td>
                    <Td align="right" className="font-medium">
                      {formatUSD(m.cost)}
                    </Td>
                    <Td>
                      <div className="flex items-center gap-2">
                        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-subtle">
                          <div className="h-full rounded-full bg-accent" style={{ width: `${Math.min(100, share)}%` }} />
                        </div>
                        <span className="w-10 text-right text-xs text-muted tabular-nums">{share.toFixed(share > 0 && share < 10 ? 1 : 0)}%</span>
                      </div>
                    </Td>
                  </Tr>
                );
              })}
          </TBody>
        </Table>
      </Card>
    </>
  );
}
