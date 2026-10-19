import { useEffect, useState } from "react";
import { api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatDateTime, formatUSD, parseNumber } from "../../lib/format";
import type { RedeemCode } from "../../types";
import { Card, Notice, PageHeader } from "../../components/Card";
import { Badge } from "../../components/Badge";
import { Button, IconButton } from "../../components/Button";
import { CopyButton } from "../../components/CopyButton";
import { Dialog, useConfirm } from "../../components/Dialog";
import { Input } from "../../components/Input";
import { Pagination } from "../../components/Pagination";
import { useToast } from "../../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";

export function AdminRedeemCodesPage() {
  const t = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminRedeem"), config?.name);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const codes = useAsync(() => api.admin.redeemCodes({ page, page_size: pageSize }), [page, pageSize]);
  const [generating, setGenerating] = useState(false);
  const [generated, setGenerated] = useState<{ amount: number; codes: string[] } | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);

  const rows = codes.data?.data ?? [];
  const total = codes.data?.total ?? 0;

  const remove = async (c: RedeemCode) => {
    const ok = await confirm({
      title: t("redeem.deleteTitle"),
      message: t("redeem.deleteMessage", { code: c.code, amount: formatUSD(c.amount) }),
      confirmLabel: t("common.delete"),
      danger: true,
    });
    if (!ok) return;
    setBusyId(c.id);
    try {
      await api.admin.deleteRedeemCode(c.id);
      toast.success(t("redeem.deleted"));
      if (rows.length === 1 && page > 1) setPage(page - 1);
      else codes.reload();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("redeem.title")}
        description={t("redeem.desc")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setGenerating(true)}>
            {t("redeem.generate")}
          </Button>
        }
      />
      <Card
        flush
        actions={
          <Button size="sm" icon="refresh" onClick={codes.reload} loading={codes.loading && !!codes.data}>
            {t("common.refresh")}
          </Button>
        }
        title={t("redeem.count", { n: total })}
      >
        <Table stickyLast minWidth={860}>
          <THead>
            <Th>{t("redeem.code")}</Th>
            <Th align="right">{t("credits.amount")}</Th>
            <Th>{t("common.status")}</Th>
            <Th>{t("redeem.usedBy")}</Th>
            <Th>{t("redeem.usedAt")}</Th>
            <Th>{t("common.created")}</Th>
            <Th align="right">{t("common.actions")}</Th>
          </THead>
          <TBody>
            <TableState colSpan={7} loading={codes.loading} error={codes.error} empty={rows.length === 0} onRetry={codes.reload} emptyText={t("redeem.empty")} />
            {!codes.error &&
              rows.map((c) => {
                const used = !!c.used_at || !!c.used_by_email;
                return (
                  <Tr key={c.id} className={used ? "text-muted" : undefined}>
                    <Td>
                      <div className="flex items-center gap-0.5">
                        <code className="font-mono text-xs whitespace-nowrap">{c.code}</code>
                        {!used && <CopyButton text={c.code} />}
                      </div>
                    </Td>
                    <Td align="right" className="font-medium">
                      {formatUSD(c.amount)}
                    </Td>
                    <Td>
                      <Badge tone={used ? "neutral" : "success"} dot>
                        {used ? t("redeem.used") : t("redeem.unused")}
                      </Badge>
                    </Td>
                    <Td className="max-w-56 truncate" title={c.used_by_email ?? undefined}>
                      {c.used_by_email || "—"}
                    </Td>
                    <Td className="whitespace-nowrap tabular-nums">{formatDateTime(c.used_at)}</Td>
                    <Td className="whitespace-nowrap text-muted tabular-nums">{formatDateTime(c.created_at)}</Td>
                    <Td align="right">
                      {!used && (
                        <IconButton icon="trash" label={t("common.delete")} onClick={() => remove(c)} disabled={busyId === c.id} className="hover:text-red-600" />
                      )}
                    </Td>
                  </Tr>
                );
              })}
          </TBody>
        </Table>
        {total > 0 && !codes.error && (
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

      <GenerateDialog
        open={generating}
        onClose={() => setGenerating(false)}
        onGenerated={(r) => {
          setGenerating(false);
          setGenerated(r);
          if (page === 1) codes.reload();
          else setPage(1);
        }}
      />

      <Dialog
        open={!!generated}
        onClose={() => setGenerated(null)}
        dismissible={false}
        size="md"
        title={t("redeem.generatedTitle", { n: generated?.codes.length ?? 0 })}
        description={generated ? t("redeem.generatedDesc", { amount: formatUSD(generated.amount) }) : undefined}
        footer={
          <>
            {generated && <CopyButton text={generated.codes.join("\n")} label={t("redeem.copyAll")} size="sm" className="h-9 px-3" />}
            <Button variant="primary" onClick={() => setGenerated(null)}>
              {t("common.done")}
            </Button>
          </>
        }
      >
        {generated && (
          <div className="flex flex-col gap-3">
            <Notice tone="info">{t("redeem.generatedHint")}</Notice>
            <pre className="max-h-72 overflow-auto rounded-lg border border-line bg-code p-3 font-mono text-[12.5px] leading-relaxed select-all">{generated.codes.join("\n")}</pre>
          </div>
        )}
      </Dialog>
    </>
  );
}

function GenerateDialog({ open, onClose, onGenerated }: { open: boolean; onClose: () => void; onGenerated: (r: { amount: number; codes: string[] }) => void }) {
  const t = useT();
  const toast = useToast();
  const [amount, setAmount] = useState("");
  const [count, setCount] = useState("1");
  const [errors, setErrors] = useState<{ amount?: string; count?: string }>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open) return;
    setAmount("");
    setCount("1");
    setErrors({});
  }, [open]);

  const submit = async () => {
    const a = parseNumber(amount);
    const c = parseNumber(count);
    const errs: typeof errors = {};
    if (a === null || Number.isNaN(a) || a <= 0) errs.amount = t("redeem.amountInvalid");
    if (c === null || Number.isNaN(c) || !Number.isInteger(c) || c < 1 || c > 100) errs.count = t("redeem.countInvalid");
    setErrors(errs);
    if (Object.keys(errs).length || a === null || c === null) return;
    setBusy(true);
    try {
      const r = await api.admin.createRedeemCodes({ amount: a, count: c });
      toast.success(t("redeem.generatedToast", { n: r.codes.length }));
      onGenerated({ amount: a, codes: r.codes });
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const a = parseNumber(amount);
  const c = parseNumber(count);
  const totalValue = a && c && !Number.isNaN(a) && !Number.isNaN(c) ? a * c : null;

  return (
    <Dialog
      open={open}
      onClose={onClose}
      dismissible={!busy}
      onSubmit={submit}
      size="sm"
      title={t("redeem.generateTitle")}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" variant="primary" loading={busy}>
            {t("redeem.generate")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Input
          label={t("redeem.amountLabel")}
          type="number"
          min={0}
          step="any"
          inputMode="decimal"
          placeholder="10"
          value={amount}
          onChange={(e) => setAmount(e.target.value)}
          error={errors.amount}
          disabled={busy}
          required
        />
        <Input
          label={t("redeem.countLabel")}
          type="number"
          min={1}
          max={100}
          step={1}
          inputMode="numeric"
          value={count}
          onChange={(e) => setCount(e.target.value)}
          error={errors.count}
          hint={t("redeem.countHint")}
          disabled={busy}
          required
        />
        {totalValue !== null && totalValue > 0 && <p className="text-xs text-muted">{t("redeem.totalValue", { value: formatUSD(totalValue) })}</p>}
      </div>
    </Dialog>
  );
}
