import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api";
import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatInt, formatPercent, formatTokens, formatUSD } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { ReportRow } from "../../types";
import { Card, Notice, PageHeader, StatCard } from "../../components/Card";
import { DualBarChart, type DualBarDatum } from "../../components/BarChart";
import { Button } from "../../components/Button";
import { ErrorState } from "../../components/EmptyState";
import { Segmented } from "../../components/Segmented";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";

const RANGES = [7, 30, 90, 365] as const;
type Range = (typeof RANGES)[number];

/**
 * Margin over the requests whose upstream cost is known. `profit` and `upstream_cost` cover the
 * same requests, so their revenue is profit + upstream_cost.
 */
function margin(r: ReportRow): number | null {
  const known = r.profit + r.upstream_cost;
  return known > 0 ? r.profit / known : null;
}

function Money({ value, signed }: { value: number; signed?: boolean }) {
  return <span className={cx(signed && value < 0 && "text-red-600 dark:text-red-400")}>{formatUSD(value)}</span>;
}

export function AdminReportsPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminReports"), config?.name);
  const [days, setDays] = useState<Range>(30);
  const report = useAsync(() => api.admin.report(days), [days]);
  const r = report.data;
  const loading = report.loading && !r;
  const totals = r?.totals;

  // A year of daily bars is too dense; show weekly buckets instead.
  const weekly = days > 90;
  const chart: DualBarDatum[] = useMemo(() => {
    const daily = r?.daily ?? [];
    if (!weekly) return daily.map((d) => ({ label: d.date, a: d.revenue, b: d.upstream_cost }));
    const out: DualBarDatum[] = [];
    // Bucket from the end so the last bar is the most recent full week.
    for (let end = daily.length; end > 0; end -= 7) {
      const chunk = daily.slice(Math.max(0, end - 7), end);
      out.unshift({
        label: chunk[0].date,
        end: chunk[chunk.length - 1].date,
        a: chunk.reduce((s, d) => s + d.revenue, 0),
        b: chunk.reduce((s, d) => s + d.upstream_cost, 0),
      });
    }
    return out;
  }, [r, weekly]);

  const byModel = useMemo(() => [...(r?.by_model ?? [])].sort((a, b) => b.revenue - a.revenue), [r]);
  const byProvider = useMemo(() => [...(r?.by_provider ?? [])].sort((a, b) => b.revenue - a.revenue), [r]);
  const m = totals ? margin(totals) : null;

  return (
    <>
      <PageHeader
        title={t("reports.title")}
        description={t("reports.desc")}
        actions={
          <>
            <Segmented
              value={days}
              onChange={setDays}
              label={t("dashboard.range")}
              options={RANGES.map((n) => ({ value: n, label: t("reports.days", { n }) }))}
            />
            <Button size="sm" icon="refresh" onClick={report.reload} loading={report.loading && !!r}>
              {t("common.refresh")}
            </Button>
          </>
        }
      />
      {report.error && !r ? (
        <Card>
          <ErrorState error={report.error} onRetry={report.reload} />
        </Card>
      ) : (
        <>
          {totals && totals.unknown_cost_requests > 0 && (
            <Notice tone="warning" className="mb-4">
              {t("reports.unknownCost", { n: totals.unknown_cost_requests })}{" "}
              <Link to="/admin/models" className="font-medium underline">
                {t("reports.goToModels")}
              </Link>
            </Notice>
          )}
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <StatCard label={t("reports.revenue")} value={formatUSD(totals?.revenue)} loading={loading} hint={t("reports.revenueHint")} />
            <StatCard label={t("reports.upstreamCost")} value={formatUSD(totals?.upstream_cost)} loading={loading} hint={t("reports.upstreamCostHint")} />
            <StatCard
              label={t("reports.profit")}
              value={totals ? <Money value={totals.profit} signed /> : "—"}
              loading={loading}
              hint={m !== null ? t("adminOverview.margin", { pct: formatPercent(m) }) : t("reports.noCostData")}
            />
            <StatCard
              label={t("dashboard.requests")}
              value={formatInt(totals?.requests)}
              loading={loading}
              hint={totals ? t("dashboard.tokensExact", { n: totals.tokens }) : undefined}
            />
          </div>

          <Card
            className="mt-4"
            title={t("reports.chartTitle")}
            description={weekly ? t("reports.chartWeekly") : t("reports.chartDaily")}
          >
            {loading ? (
              <div className="h-[252px] animate-pulse rounded-lg bg-subtle" />
            ) : (
              <DualBarChart
                height={200}
                data={chart}
                labels={[t("reports.revenue"), t("reports.upstreamCost")]}
                format={(v) => formatUSD(v)}
                emptyText={t("dashboard.noUsage")}
              />
            )}
          </Card>

          <div className="mt-4 grid gap-4 2xl:grid-cols-2">
            <Card flush title={t("reports.byModel")}>
              <Table minWidth={780}>
                <THead>
                  <Th>{t("logs.model")}</Th>
                  <Th align="right">{t("dashboard.requests")}</Th>
                  <Th align="right">{t("dashboard.tokens")}</Th>
                  <Th align="right">{t("reports.revenue")}</Th>
                  <Th align="right">{t("reports.upstreamCost")}</Th>
                  <Th align="right">{t("reports.profit")}</Th>
                  <Th align="right">{t("reports.margin")}</Th>
                  <Th align="right" title={t("reports.noCostHint")}>
                    {t("reports.noCostCol")}
                  </Th>
                </THead>
                <TBody>
                  <TableState colSpan={8} loading={report.loading && !r} error={r ? undefined : report.error} empty={byModel.length === 0} emptyText={t("dashboard.noUsage")} />
                  {byModel.map((row) => {
                    const mg = margin(row);
                    return (
                      <Tr key={row.model}>
                        <Td className="max-w-64 truncate font-mono text-xs" title={row.model}>
                          {row.model}
                        </Td>
                        <Td align="right">{formatInt(row.requests)}</Td>
                        <Td align="right" className="text-muted">
                          {formatTokens(row.tokens)}
                        </Td>
                        <Td align="right" className="font-medium">
                          {formatUSD(row.revenue)}
                        </Td>
                        <Td align="right">{formatUSD(row.upstream_cost)}</Td>
                        <Td align="right">
                          <Money value={row.profit} signed />
                        </Td>
                        <Td align="right" className={cx(mg !== null && mg < 0 && "text-red-600 dark:text-red-400")}>
                          {formatPercent(mg)}
                        </Td>
                        <Td align="right" className={cx(row.unknown_cost_requests > 0 ? "text-amber-600 dark:text-amber-400" : "text-muted")}>
                          {formatInt(row.unknown_cost_requests)}
                        </Td>
                      </Tr>
                    );
                  })}
                </TBody>
              </Table>
            </Card>

            <Card flush title={t("reports.byProvider")}>
              <Table minWidth={720}>
                <THead>
                  <Th>{t("logs.provider")}</Th>
                  <Th align="right">{t("dashboard.requests")}</Th>
                  <Th align="right">{t("reports.successRate")}</Th>
                  <Th align="right">{t("reports.revenue")}</Th>
                  <Th align="right">{t("reports.upstreamCost")}</Th>
                  <Th align="right">{t("reports.profit")}</Th>
                  <Th align="right">{t("reports.margin")}</Th>
                </THead>
                <TBody>
                  <TableState colSpan={7} loading={report.loading && !r} error={r ? undefined : report.error} empty={byProvider.length === 0} emptyText={t("dashboard.noUsage")} />
                  {byProvider.map((row) => {
                    const mg = margin(row);
                    const sr = row.success_rate;
                    return (
                      <Tr key={row.provider_id}>
                        <Td className="font-medium whitespace-nowrap">{row.provider || `#${row.provider_id}`}</Td>
                        <Td align="right">{formatInt(row.requests)}</Td>
                        <Td
                          align="right"
                          className={cx(sr === null ? "text-muted" : sr < 0.8 ? "text-red-600 dark:text-red-400" : sr < 0.95 ? "text-amber-600 dark:text-amber-400" : "")}
                        >
                          {formatPercent(sr)}
                        </Td>
                        <Td align="right" className="font-medium">
                          {formatUSD(row.revenue)}
                        </Td>
                        <Td align="right">{formatUSD(row.upstream_cost)}</Td>
                        <Td align="right">
                          <Money value={row.profit} signed />
                        </Td>
                        <Td align="right" className={cx(mg !== null && mg < 0 && "text-red-600 dark:text-red-400")}>
                          {formatPercent(mg)}
                        </Td>
                      </Tr>
                    );
                  })}
                </TBody>
              </Table>
            </Card>
          </div>
        </>
      )}
    </>
  );
}
