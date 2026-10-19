import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import { useAuth } from "../../auth";
import { useT, type MessageKey } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatInt, formatPercent, formatTokens, formatUSD } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { Alert, AlertKind, DailyStat } from "../../types";
import { Card, PageHeader, StatCard } from "../../components/Card";
import { BarChart } from "../../components/BarChart";
import { Button } from "../../components/Button";
import { ErrorState } from "../../components/EmptyState";
import { Icon } from "../../components/Icon";
import { Segmented } from "../../components/Segmented";

type Metric = "cost" | "requests" | "tokens";

const fmt: Record<Metric, (v: number) => string> = {
  cost: (v) => formatUSD(v),
  requests: formatInt,
  tokens: formatTokens,
};

const ALERT_TITLES: Record<AlertKind, MessageKey> = {
  key_failed: "alerts.keyFailed",
  provider_no_keys: "alerts.providerNoKeys",
  provider_down: "alerts.providerDown",
  model_unroutable: "alerts.modelUnroutable",
  missing_cost: "alerts.missingCost",
};

/** Localized alert text from its params; falls back to the server's English message. */
function alertMessage(t: ReturnType<typeof useT>, a: Alert): string {
  const p = a.params;
  if (!p) return a.message;
  switch (a.kind) {
    case "key_failed":
      return t(p.reason === "quota" ? "alerts.msg.keyQuota" : "alerts.msg.keyAuth", { key: p.key ?? "", provider: p.provider ?? "" });
    case "provider_no_keys":
      return t("alerts.msg.providerNoKeys", { provider: p.provider ?? "" });
    case "provider_down":
      return t("alerts.msg.providerDown", { provider: p.provider ?? "", rate: p.rate ?? 0 });
    case "model_unroutable":
      return t("alerts.msg.modelUnroutable", { model: p.model ?? "" });
    case "missing_cost":
      return t("alerts.msg.missingCost", { n: p.n ?? 0 });
  }
  return a.message;
}

/** Where an alert should take the admin: the provider's keys or the model editor. */
function alertLink(a: Alert): { to: string; label: MessageKey } | null {
  if (a.model_id !== undefined && a.model_id !== null) return { to: `/admin/models?edit=${a.model_id}`, label: "alerts.openModel" };
  if (a.provider_id !== undefined && a.provider_id !== null) {
    return a.kind === "key_failed" || a.kind === "provider_no_keys"
      ? { to: `/admin/providers?keys=${a.provider_id}`, label: "alerts.openKeys" }
      : { to: "/admin/providers", label: "alerts.openProvider" };
  }
  if (a.kind === "missing_cost" || a.kind === "model_unroutable") return { to: "/admin/models", label: "alerts.openModels" };
  return null;
}

export function AdminOverviewPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminOverview"), config?.name);
  const stats = useAsync(() => api.admin.stats(), []);
  const [metric, setMetric] = useState<Metric>("cost");
  const s = stats.data;
  const loading = stats.loading && !s;

  const daily: DailyStat[] = s?.daily ?? [];
  const sum = (k: Metric) => daily.reduce((a, d) => a + d[k], 0);
  const profit = s?.profit_today ?? null;
  // Profit only covers requests with a known upstream cost, so this is approximate when some
  // endpoints have no cost set (the Reports page shows the exact figures).
  const margin = s && s.cost_today > 0 && profit !== null ? profit / s.cost_today : null;
  const alerts = s?.alerts ?? [];

  const manage = (to: string) => (
    <Link to={to} className="text-accent hover:underline">
      {t("common.manage")}
    </Link>
  );

  return (
    <>
      <PageHeader
        title={t("adminOverview.title")}
        description={t("adminOverview.desc")}
        actions={
          <Button size="sm" icon="refresh" onClick={stats.reload} loading={stats.loading && !!s}>
            {t("common.refresh")}
          </Button>
        }
      />
      {stats.error && !s ? (
        <Card>
          <ErrorState error={stats.error} onRetry={stats.reload} />
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
            <StatCard label={t("adminOverview.requestsToday")} value={formatInt(s?.requests_today)} loading={loading} />
            <StatCard label={t("adminOverview.revenueToday")} value={formatUSD(s?.cost_today)} loading={loading} hint={t("adminOverview.revenueHint")} />
            <div className="col-span-2 sm:col-span-1">
              <StatCard
                label={t("adminOverview.profitToday")}
                value={<span className={cx(profit !== null && profit < 0 && "text-red-600 dark:text-red-400")}>{formatUSD(profit)}</span>}
                loading={loading}
                hint={
                  margin !== null ? (
                    <>
                      {t("adminOverview.margin", { pct: formatPercent(margin) })} ·{" "}
                      <Link to="/admin/reports" className="text-accent hover:underline">
                        {t("adminOverview.viewReports")}
                      </Link>
                    </>
                  ) : (
                    <Link to="/admin/reports" className="text-accent hover:underline">
                      {t("adminOverview.viewReports")}
                    </Link>
                  )
                }
              />
            </div>
          </div>
          <div className="mt-3 grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard label={t("adminOverview.users")} value={formatInt(s?.users)} loading={loading} hint={manage("/admin/users")} />
            <StatCard label={t("adminOverview.providers")} value={formatInt(s?.providers)} loading={loading} hint={manage("/admin/providers")} />
            <StatCard label={t("adminOverview.models")} value={formatInt(s?.models)} loading={loading} hint={manage("/admin/models")} />
            <StatCard label={t("adminOverview.totalBalance")} value={formatUSD(s?.total_balance)} loading={loading} hint={t("adminOverview.totalBalanceHint")} />
          </div>

          <div className="mt-4 grid gap-4 xl:grid-cols-3">
            <Card
              className="xl:col-span-2"
              title={t("adminOverview.last14")}
              description={s ? t("adminOverview.last14Total", { value: fmt[metric](sum(metric)) }) : undefined}
              actions={
                <Segmented
                  value={metric}
                  onChange={setMetric}
                  label={t("adminOverview.metric")}
                  options={[
                    { value: "cost", label: t("reports.revenue") },
                    { value: "requests", label: t("dashboard.requests") },
                    { value: "tokens", label: t("dashboard.tokens") },
                  ]}
                />
              }
            >
              {loading ? (
                <div className="h-[232px] animate-pulse rounded-lg bg-subtle" />
              ) : (
                <BarChart height={200} data={daily.map((d) => ({ label: d.date, value: d[metric] }))} format={fmt[metric]} emptyText={t("dashboard.noUsage")} />
              )}
            </Card>
            <AlertsCard alerts={alerts} loading={loading} />
          </div>
        </>
      )}
    </>
  );
}

function AlertsCard({ alerts, loading }: { alerts: Alert[]; loading: boolean }) {
  const t = useT();
  const errors = alerts.filter((a) => a.level === "error").length;
  // Errors first, keep the server's order otherwise.
  const sorted = [...alerts].sort((a, b) => (a.level === b.level ? 0 : a.level === "error" ? -1 : 1));
  return (
    <Card
      flush
      title={t("alerts.title")}
      description={alerts.length ? t("alerts.summary", { n: alerts.length, errors }) : undefined}
    >
      {loading ? (
        <div className="flex flex-col gap-2 p-4">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-10 animate-pulse rounded-lg bg-subtle" />
          ))}
        </div>
      ) : alerts.length === 0 ? (
        <div className="flex flex-col items-center gap-2 px-4 py-10 text-center">
          <div className="flex size-10 items-center justify-center rounded-full bg-emerald-50 text-emerald-600 dark:bg-emerald-950/40 dark:text-emerald-400">
            <Icon name="checkCircle" className="size-5" />
          </div>
          <div className="text-sm font-medium">{t("alerts.allGood")}</div>
          <div className="max-w-xs text-[13px] text-muted">{t("alerts.allGoodDesc")}</div>
        </div>
      ) : (
        <ul className="max-h-[300px] divide-y divide-line overflow-y-auto">
          {sorted.map((a, i) => {
            const link = alertLink(a);
            const err = a.level === "error";
            return (
              <li key={i} className="flex gap-2.5 px-4 py-2.5">
                <Icon
                  name={err ? "alert" : "info"}
                  className={cx("mt-0.5 size-4", err ? "text-red-600 dark:text-red-400" : "text-amber-600 dark:text-amber-400")}
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[13px] font-medium">{t(ALERT_TITLES[a.kind] ?? "alerts.other")}</div>
                  <div className="text-xs break-words text-muted">{alertMessage(t, a)}</div>
                  {link && (
                    <Link to={link.to} className="mt-0.5 inline-block text-xs font-medium text-accent hover:underline">
                      {t(link.label)} →
                    </Link>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}
