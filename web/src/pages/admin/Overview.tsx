import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatInt, formatTokens, formatUSD } from "../../lib/format";
import type { DailyStat } from "../../types";
import { Card, PageHeader, StatCard } from "../../components/Card";
import { BarChart } from "../../components/BarChart";
import { Button } from "../../components/Button";
import { ErrorState } from "../../components/EmptyState";
import { Segmented } from "../../components/Segmented";

type Metric = "cost" | "requests" | "tokens";

const fmt: Record<Metric, (v: number) => string> = {
  cost: (v) => formatUSD(v),
  requests: formatInt,
  tokens: formatTokens,
};

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
          <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
            <StatCard label={t("adminOverview.users")} value={formatInt(s?.users)} loading={loading} hint={<Link to="/admin/users" className="text-accent hover:underline">{t("common.manage")}</Link>} />
            <StatCard label={t("adminOverview.providers")} value={formatInt(s?.providers)} loading={loading} hint={<Link to="/admin/providers" className="text-accent hover:underline">{t("common.manage")}</Link>} />
            <StatCard label={t("adminOverview.models")} value={formatInt(s?.models)} loading={loading} hint={<Link to="/admin/models" className="text-accent hover:underline">{t("common.manage")}</Link>} />
            <StatCard label={t("adminOverview.totalBalance")} value={formatUSD(s?.total_balance)} loading={loading} hint={t("adminOverview.totalBalanceHint")} />
            <StatCard label={t("adminOverview.requestsToday")} value={formatInt(s?.requests_today)} loading={loading} />
            <StatCard label={t("adminOverview.costToday")} value={formatUSD(s?.cost_today)} loading={loading} />
          </div>

          <Card
            className="mt-4"
            title={t("adminOverview.last14")}
            description={s ? t("adminOverview.last14Total", { value: fmt[metric](sum(metric)) }) : undefined}
            actions={
              <Segmented
                value={metric}
                onChange={setMetric}
                label={t("adminOverview.metric")}
                options={[
                  { value: "cost", label: t("logs.cost") },
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
        </>
      )}
    </>
  );
}
