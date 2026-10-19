import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, errorMessage } from "../api";
import { useAuth } from "../auth";
import { useT } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { formatDate, formatDateTime, formatRelative, formatUSD, parseNumber } from "../lib/format";
import type { ApiKey } from "../types";
import { Card, Notice, PageHeader } from "../components/Card";
import { Badge } from "../components/Badge";
import { Button, IconButton } from "../components/Button";
import { CopyButton } from "../components/CopyButton";
import { Dialog, useConfirm } from "../components/Dialog";
import { EmptyState } from "../components/EmptyState";
import { Input } from "../components/Input";
import { Toggle } from "../components/Toggle";
import { useToast } from "../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../components/Table";

export function KeysPage() {
  const t = useT();
  const { config } = useAuth();
  const toast = useToast();
  const confirm = useConfirm();
  useDocumentTitle(t("nav.keys"), config?.name);
  const keys = useAsync(() => api.keys(), []);
  const [params, setParams] = useSearchParams();
  const [editing, setEditing] = useState<ApiKey | "new" | null>(null);
  const [secret, setSecret] = useState<{ name: string; secret: string } | null>(null);
  const [busyId, setBusyId] = useState<number | null>(null);

  // /keys?new=1 (from the dashboard) opens the create dialog directly.
  useEffect(() => {
    if (params.get("new")) {
      setEditing("new");
      const next = new URLSearchParams(params);
      next.delete("new");
      setParams(next, { replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const list = keys.data?.data ?? [];

  const replace = (k: ApiKey) => keys.setData((d) => d && { data: d.data.map((x) => (x.id === k.id ? k : x)) });

  const toggle = async (k: ApiKey, enabled: boolean) => {
    setBusyId(k.id);
    try {
      const r = await api.updateKey(k.id, { disabled: !enabled });
      replace(r.key);
      toast.success(enabled ? t("keys.enabled") : t("keys.disabledToast"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (k: ApiKey) => {
    const ok = await confirm({
      title: t("keys.deleteTitle"),
      message: t("keys.deleteMessage", { name: k.name }),
      confirmLabel: t("common.delete"),
      danger: true,
    });
    if (!ok) return;
    setBusyId(k.id);
    try {
      await api.deleteKey(k.id);
      keys.setData((d) => d && { data: d.data.filter((x) => x.id !== k.id) });
      toast.success(t("keys.deleted"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("keys.title")}
        description={t("keys.desc")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setEditing("new")}>
            {t("keys.create")}
          </Button>
        }
      />

      <Card flush>
        {!keys.loading && !keys.error && list.length === 0 ? (
          <EmptyState
            icon="key"
            title={t("keys.emptyTitle")}
            description={t("keys.emptyDesc")}
            action={
              <Button variant="primary" icon="plus" size="sm" onClick={() => setEditing("new")}>
                {t("keys.create")}
              </Button>
            }
          />
        ) : (
          <Table stickyLast minWidth={880}>
            <THead>
              <Th>{t("keys.name")}</Th>
              <Th>{t("keys.key")}</Th>
              <Th align="right">{t("keys.usage")}</Th>
              <Th>{t("keys.limit")}</Th>
              <Th>{t("keys.lastUsed")}</Th>
              <Th>{t("common.created")}</Th>
              <Th>{t("common.status")}</Th>
              <Th align="right">{t("common.actions")}</Th>
            </THead>
            <TBody>
              <TableState colSpan={8} loading={keys.loading} error={keys.error} empty={list.length === 0} onRetry={keys.reload} />
              {!keys.error &&
                list.map((k) => (
                  <Tr key={k.id} className={k.disabled ? "opacity-60" : undefined}>
                    <Td className="max-w-56 truncate font-medium" title={k.name}>
                      {k.name}
                    </Td>
                    <Td className="font-mono text-xs whitespace-nowrap text-muted">{k.prefix}</Td>
                    <Td align="right" className="font-medium">
                      {formatUSD(k.usage)}
                    </Td>
                    <Td>
                      <LimitCell usage={k.usage} limit={k.credit_limit} />
                    </Td>
                    <Td className="whitespace-nowrap text-muted" title={k.last_used_at ? formatDateTime(k.last_used_at) : undefined}>
                      {k.last_used_at ? formatRelative(k.last_used_at) : t("keys.never")}
                    </Td>
                    <Td className="whitespace-nowrap text-muted">{formatDate(k.created_at)}</Td>
                    <Td>
                      <div className="flex items-center gap-2">
                        <Toggle size="sm" checked={!k.disabled} disabled={busyId === k.id} onChange={(v) => toggle(k, v)} label={k.disabled ? t("common.enable") : t("common.disable")} />
                        <Badge tone={k.disabled ? "neutral" : "success"}>{k.disabled ? t("common.disabled") : t("common.active")}</Badge>
                      </div>
                    </Td>
                    <Td align="right">
                      <div className="flex justify-end gap-0.5">
                        <IconButton icon="edit" label={t("common.edit")} onClick={() => setEditing(k)} />
                        <IconButton icon="trash" label={t("common.delete")} onClick={() => remove(k)} disabled={busyId === k.id} className="hover:text-red-600" />
                      </div>
                    </Td>
                  </Tr>
                ))}
            </TBody>
          </Table>
        )}
      </Card>

      <KeyDialog
        target={editing}
        onClose={() => setEditing(null)}
        onCreated={(r) => {
          keys.setData((d) => ({ data: [r.key, ...(d?.data ?? [])] }));
          setEditing(null);
          setSecret({ name: r.key.name, secret: r.secret });
        }}
        onUpdated={(k) => {
          replace(k);
          setEditing(null);
          toast.success(t("keys.saved"));
        }}
      />

      <Dialog
        open={!!secret}
        onClose={() => setSecret(null)}
        dismissible={false}
        title={t("keys.secretTitle")}
        description={secret ? t("keys.secretDesc", { name: secret.name }) : undefined}
        footer={
          <Button variant="primary" onClick={() => setSecret(null)}>
            {t("keys.secretDone")}
          </Button>
        }
      >
        {secret && (
          <div className="flex flex-col gap-3">
            <Notice tone="warning">{t("keys.secretWarning")}</Notice>
            <div className="flex items-center gap-2 rounded-lg border border-line bg-code p-2 pl-3">
              <code className="min-w-0 flex-1 font-mono text-[13px] break-all select-all">{secret.secret}</code>
              <CopyButton text={secret.secret} size="sm" />
            </div>
          </div>
        )}
      </Dialog>
    </>
  );
}

function LimitCell({ usage, limit }: { usage: number; limit: number | null }) {
  const t = useT();
  if (limit === null) return <span className="text-muted">{t("keys.unlimited")}</span>;
  const pct = limit > 0 ? Math.min(100, (usage / limit) * 100) : 100;
  return (
    <div className="flex min-w-32 flex-col gap-1">
      <span className="text-xs whitespace-nowrap tabular-nums">
        {formatUSD(usage)} / {formatUSD(limit)}
      </span>
      <div className="h-1 overflow-hidden rounded-full bg-subtle">
        <div className={pct >= 100 ? "h-full bg-red-500" : pct >= 80 ? "h-full bg-amber-500" : "h-full bg-accent"} style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

function KeyDialog({
  target,
  onClose,
  onCreated,
  onUpdated,
}: {
  target: ApiKey | "new" | null;
  onClose: () => void;
  onCreated: (r: { key: ApiKey; secret: string }) => void;
  onUpdated: (k: ApiKey) => void;
}) {
  const t = useT();
  const toast = useToast();
  const isNew = target === "new";
  const [name, setName] = useState("");
  const [limit, setLimit] = useState("");
  const [errors, setErrors] = useState<{ name?: string; limit?: string }>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!target) return;
    setErrors({});
    if (target === "new") {
      setName("");
      setLimit("");
    } else {
      setName(target.name);
      setLimit(target.credit_limit === null ? "" : String(target.credit_limit));
    }
  }, [target]);

  const submit = async () => {
    const lim = parseNumber(limit);
    const errs: typeof errors = {};
    if (!name.trim()) errs.name = t("keys.nameRequired");
    if (lim !== null && (Number.isNaN(lim) || lim < 0)) errs.limit = t("keys.limitInvalid");
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    try {
      if (target === "new") {
        onCreated(await api.createKey({ name: name.trim(), credit_limit: lim }));
      } else if (target) {
        const r = await api.updateKey(target.id, { name: name.trim(), credit_limit: lim });
        onUpdated(r.key);
      }
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={!!target}
      onClose={onClose}
      dismissible={!busy}
      onSubmit={submit}
      title={isNew ? t("keys.createTitle") : t("keys.editTitle")}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" variant="primary" loading={busy}>
            {isNew ? t("keys.create") : t("common.save")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Input label={t("keys.name")} placeholder={t("keys.namePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} error={errors.name} disabled={busy} required maxLength={100} />
        <Input
          label={t("keys.creditLimit")}
          type="number"
          min={0}
          step="any"
          inputMode="decimal"
          placeholder={t("keys.unlimited")}
          value={limit}
          onChange={(e) => setLimit(e.target.value)}
          error={errors.limit}
          hint={t("keys.creditLimitHint")}
          disabled={busy}
        />
      </div>
    </Dialog>
  );
}
