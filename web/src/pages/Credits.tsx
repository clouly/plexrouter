import { useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { useAuth } from "../auth";
import { useT, type MessageKey } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { formatDateTime, formatUSD } from "../lib/format";
import { cx } from "../lib/cx";
import type { TransactionType } from "../types";
import { Card, Notice, PageHeader } from "../components/Card";
import { Badge, type BadgeTone } from "../components/Badge";
import { Button } from "../components/Button";
import { Input } from "../components/Input";
import { Pagination } from "../components/Pagination";
import { useToast } from "../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../components/Table";

const typeLabel: Record<TransactionType, MessageKey> = {
  signup: "credits.typeSignup",
  redeem: "credits.typeRedeem",
  adjust: "credits.typeAdjust",
};
const typeTone: Record<TransactionType, BadgeTone> = { signup: "accent", redeem: "success", adjust: "neutral" };

export function CreditsPage() {
  const t = useT();
  const toast = useToast();
  const { user, config, setUser, refreshUser } = useAuth();
  useDocumentTitle(t("nav.credits"), config?.name);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const txs = useAsync(() => api.transactions({ page, page_size: pageSize }), [page, pageSize]);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const redeem = async (e: FormEvent) => {
    e.preventDefault();
    const c = code.trim();
    if (!c) {
      setError(t("credits.codeRequired"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const r = await api.redeem(c);
      if (user) setUser({ ...user, balance: r.balance });
      void refreshUser();
      toast.success(t("credits.redeemed", { amount: formatUSD(r.amount), balance: formatUSD(r.balance) }));
      setCode("");
      if (page === 1) txs.reload();
      else setPage(1);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const rows = txs.data?.data ?? [];
  const total = txs.data?.total ?? 0;

  return (
    <>
      <PageHeader title={t("credits.title")} description={t("credits.desc")} />
      <div className="grid gap-4 md:grid-cols-5">
        <div className="flex min-w-0 flex-col justify-between rounded-xl border border-line bg-surface p-5 shadow-xs md:col-span-2">
          <div className="text-xs font-medium text-muted">{t("credits.balance")}</div>
          <div className="mt-2 truncate text-4xl font-semibold tracking-tight tabular-nums">{formatUSD(user?.balance)}</div>
          <p className="mt-3 text-xs text-muted">{t("credits.balanceHint")}</p>
        </div>
        <Card className="md:col-span-3" title={t("credits.redeemTitle")} description={t("credits.redeemDesc")}>
          <form onSubmit={redeem} className="flex flex-col gap-3" noValidate>
            {error && <Notice tone="danger">{error}</Notice>}
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                wrapperClassName="flex-1"
                className="font-mono"
                placeholder={t("credits.codePlaceholder")}
                aria-label={t("credits.code")}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                disabled={busy}
                autoComplete="off"
                spellCheck={false}
              />
              <Button type="submit" variant="primary" loading={busy} icon="ticket">
                {t("credits.redeem")}
              </Button>
            </div>
          </form>
        </Card>
      </div>

      <Card flush className="mt-4" title={t("credits.history")}>
        <Table minWidth={560}>
          <THead>
            <Th>{t("logs.time")}</Th>
            <Th>{t("credits.type")}</Th>
            <Th align="right">{t("credits.amount")}</Th>
            <Th>{t("credits.note")}</Th>
          </THead>
          <TBody>
            <TableState colSpan={4} loading={txs.loading} error={txs.error} empty={rows.length === 0} onRetry={txs.reload} emptyText={t("credits.empty")} />
            {!txs.error &&
              rows.map((x) => (
                <Tr key={x.id}>
                  <Td className="whitespace-nowrap text-muted tabular-nums">{formatDateTime(x.created_at)}</Td>
                  <Td>
                    <Badge tone={typeTone[x.type] ?? "neutral"}>{typeLabel[x.type] ? t(typeLabel[x.type]) : x.type}</Badge>
                  </Td>
                  <Td align="right" className={cx("font-medium whitespace-nowrap", x.amount >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-red-600 dark:text-red-400")}>
                    {formatUSD(x.amount, { sign: true })}
                  </Td>
                  <Td className="text-muted">{x.note || "—"}</Td>
                </Tr>
              ))}
          </TBody>
        </Table>
        {total > 0 && !txs.error && (
          <Pagination
            page={page}
            pageSize={pageSize}
            total={total}
            onChange={setPage}
            onPageSizeChange={(n) => {
              setPageSize(n);
              setPage(1);
            }}
          />
        )}
      </Card>
    </>
  );
}
