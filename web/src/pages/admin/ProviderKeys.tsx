import { useEffect, useMemo, useRef, useState } from "react";
import { api, errorMessage } from "../../api";
import { useT, type MessageKey } from "../../i18n";
import { useAsync } from "../../lib/hooks";
import { formatDateTime, formatDuration, formatInt, formatRelative, isFuture } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { Provider, ProviderKey, ProviderKeyStatus } from "../../types";
import { Notice } from "../../components/Card";
import { Badge, type BadgeTone } from "../../components/Badge";
import { Button, IconButton } from "../../components/Button";
import { Dialog, useConfirm } from "../../components/Dialog";
import { EmptyState } from "../../components/EmptyState";
import { controlClass, Input, Textarea } from "../../components/Input";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";

const STATUS: Record<ProviderKeyStatus, { label: MessageKey; tone: BadgeTone }> = {
  active: { label: "providerKeys.statusActive", tone: "success" },
  auth_failed: { label: "providerKeys.statusAuthFailed", tone: "danger" },
  quota_exceeded: { label: "providerKeys.statusQuota", tone: "danger" },
  cooling_down: { label: "providerKeys.statusCooling", tone: "warning" },
  disabled: { label: "providerKeys.statusDisabled", tone: "neutral" },
};

const FAILED: ProviderKeyStatus[] = ["auth_failed", "quota_exceeded"];

/** Splits pasted text into distinct non-empty keys (one per line). */
function parseKeys(text: string): string[] {
  return [...new Set(text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean))];
}

/** Dialog listing a provider's upstream API keys, with add / rename / enable / delete. */
export function ProviderKeysDialog({ provider, onClose, onChanged }: { provider: Provider | null; onClose: () => void; onChanged: () => void }) {
  const t = useT();
  const toast = useToast();
  const confirm = useConfirm();
  // Tag results with the provider they belong to, so a previous provider's list never shows.
  const keys = useAsync(
    () => (provider ? api.admin.providerKeys(provider.id).then((r) => ({ ...r, pid: provider.id })) : Promise.resolve({ data: [] as ProviderKey[], pid: 0 })),
    [provider?.id],
  );
  const [busyId, setBusyId] = useState<number | null>(null);
  const [renaming, setRenaming] = useState<{ id: number; name: string } | null>(null);
  const [expanded, setExpanded] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState("");
  const [name, setName] = useState("");
  const [addBusy, setAddBusy] = useState(false);
  const [now, setNow] = useState(Date.now());
  const savingName = useRef(false); // Enter and blur can both trigger a save

  const current = !!provider && keys.data?.pid === provider.id;
  const list = current ? (keys.data?.data ?? []) : [];
  const loading = keys.loading || (!!provider && !current && !keys.error);
  // The add form starts open when the provider has no keys yet.
  const showAdd = adding || (current && !keys.loading && !keys.error && list.length === 0);

  useEffect(() => {
    if (!provider) return;
    setRenaming(null);
    setExpanded(null);
    setText("");
    setName("");
    setAdding(false);
  }, [provider]);

  // Tick once a second while some key is cooling down, so the remaining time counts down.
  const cooling = useMemo(() => list.some((k) => k.status === "cooling_down" && isFuture(k.cooldown_until)), [list]);
  useEffect(() => {
    if (!cooling) return;
    setNow(Date.now());
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [cooling]);

  const replace = (k: ProviderKey) => keys.setData((d) => d && { ...d, data: d.data.map((x) => (x.id === k.id ? k : x)) });

  const setEnabled = async (k: ProviderKey, enabled: boolean) => {
    if (!provider) return;
    setBusyId(k.id);
    try {
      const r = await api.admin.updateProviderKey(provider.id, k.id, { enabled });
      replace(r.key);
      onChanged();
      const wasFailed = FAILED.includes(k.status) || k.status === "cooling_down";
      toast.success(enabled ? (wasFailed ? t("providerKeys.reenabled", { name: k.name }) : t("providerKeys.enabledToast")) : t("providerKeys.disabledToast"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const saveName = async () => {
    if (!provider || !renaming || savingName.current) return;
    const k = list.find((x) => x.id === renaming.id);
    const next = renaming.name.trim();
    if (!k || !next || next === k.name) {
      setRenaming(null);
      return;
    }
    savingName.current = true;
    setBusyId(k.id);
    try {
      replace((await api.admin.updateProviderKey(provider.id, k.id, { name: next })).key);
      setRenaming(null);
      toast.success(t("providerKeys.renamed"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      savingName.current = false;
      setBusyId(null);
    }
  };

  const remove = async (k: ProviderKey) => {
    if (!provider) return;
    const ok = await confirm({
      title: t("providerKeys.deleteTitle"),
      message: t("providerKeys.deleteMessage", { name: k.name || k.key_hint, provider: provider.name }),
      confirmLabel: t("common.delete"),
      danger: true,
    });
    if (!ok) return;
    setBusyId(k.id);
    try {
      await api.admin.deleteProviderKey(provider.id, k.id);
      keys.setData((d) => d && { ...d, data: d.data.filter((x) => x.id !== k.id) });
      onChanged();
      toast.success(t("providerKeys.deleted"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const parsed = parseKeys(text);

  const add = async () => {
    if (!provider || parsed.length === 0) return;
    setAddBusy(true);
    try {
      const r = await api.admin.addProviderKeys(provider.id, { api_keys: parsed, ...(name.trim() ? { name: name.trim() } : {}) });
      toast.success(t("providerKeys.added", { n: r.data?.length ?? parsed.length }));
      setText("");
      setName("");
      setAdding(false);
      keys.reload();
      onChanged();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setAddBusy(false);
    }
  };

  const busyAny = addBusy || busyId !== null;

  return (
    <Dialog
      open={!!provider}
      onClose={onClose}
      dismissible={!addBusy}
      size="xl"
      title={t("providerKeys.title", { name: provider?.name ?? "" })}
      description={t("providerKeys.desc")}
      footer={<Button onClick={onClose}>{t("common.close")}</Button>}
    >
      <div className="flex flex-col gap-4">
        {showAdd ? (
          <div className="flex flex-col gap-3 rounded-lg border border-line p-3">
            <Textarea
              label={t("providerKeys.addLabel")}
              hint={t("providerKeys.addHint")}
              placeholder={"sk-...\nsk-..."}
              value={text}
              onChange={(e) => setText(e.target.value)}
              disabled={addBusy}
              rows={3}
              spellCheck={false}
              autoComplete="off"
              className="font-mono text-[13px]"
            />
            <Input
              label={t("providerKeys.nameOptional")}
              placeholder={t("providerKeys.namePlaceholder")}
              value={name}
              onChange={(e) => setName(e.target.value)}
              disabled={addBusy}
              maxLength={100}
              hint={parsed.length > 1 ? t("providerKeys.nameNumbered") : undefined}
            />
            <div className="flex flex-wrap justify-end gap-2">
              {list.length > 0 && (
                <Button size="sm" onClick={() => setAdding(false)} disabled={addBusy}>
                  {t("common.cancel")}
                </Button>
              )}
              <Button size="sm" variant="primary" icon="plus" onClick={add} loading={addBusy} disabled={parsed.length === 0}>
                {t("providerKeys.addSubmit", { n: parsed.length })}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-xs text-muted">
              {t("providerKeys.count", { n: list.length, active: list.filter((k) => k.enabled && k.status === "active").length })}
            </span>
            <Button size="sm" icon="plus" onClick={() => setAdding(true)}>
              {t("providerKeys.add")}
            </Button>
          </div>
        )}

        {!loading && !keys.error && list.length === 0 ? (
          <div className="rounded-lg border border-dashed border-line">
            <EmptyState icon="key" title={t("providerKeys.emptyTitle")} description={t("providerKeys.emptyDesc")} />
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-line">
            <Table stickyLast minWidth={800}>
              <THead>
                <Th>{t("providerKeys.name")}</Th>
                <Th>{t("providerKeys.key")}</Th>
                <Th>{t("common.status")}</Th>
                <Th align="right">{t("providerKeys.requests24h")}</Th>
                <Th>{t("providerKeys.lastUsed")}</Th>
                <Th>{t("common.enabled")}</Th>
                <Th align="right">{t("common.actions")}</Th>
              </THead>
              <TBody>
                <TableState colSpan={7} loading={loading} error={keys.error} empty={list.length === 0} onRetry={keys.reload} />
                {!keys.error &&
                  list.map((k) => {
                    const busy = busyId === k.id;
                    const failed = FAILED.includes(k.status);
                    const coolingLeft = k.status === "cooling_down" && k.cooldown_until ? (new Date(k.cooldown_until).getTime() - now) / 1000 : 0;
                    // A cooldown that has run out is effectively active until the next refresh.
                    const status: ProviderKeyStatus = k.status === "cooling_down" && coolingLeft <= 0 ? (k.enabled ? "active" : "disabled") : k.status;
                    const st = STATUS[status] ?? STATUS.disabled;
                    const isRenaming = renaming?.id === k.id;
                    const errOpen = expanded === k.id;
                    return (
                      <Tr key={k.id} className={cx(!k.enabled && "opacity-70")}>
                        <Td className="max-w-56">
                          {isRenaming ? (
                            <input
                              autoFocus
                              className={cx(controlClass, "h-7 min-w-36 text-[13px]")}
                              value={renaming.name}
                              maxLength={100}
                              disabled={busy}
                              aria-label={t("providerKeys.name")}
                              onChange={(e) => setRenaming({ id: k.id, name: e.target.value })}
                              onBlur={saveName}
                              onKeyDown={(e) => {
                                if (e.key === "Enter") {
                                  e.preventDefault();
                                  saveName();
                                } else if (e.key === "Escape") {
                                  e.preventDefault();
                                  e.stopPropagation(); // don't close the dialog
                                  setRenaming(null);
                                }
                              }}
                            />
                          ) : (
                            <button
                              type="button"
                              className="group flex max-w-full items-center gap-1 text-left font-medium"
                              onClick={() => setRenaming({ id: k.id, name: k.name })}
                              title={t("providerKeys.rename")}
                            >
                              <span className="truncate">{k.name || <span className="text-muted">—</span>}</span>
                            </button>
                          )}
                          <div className="text-[11px] text-muted">{t("providerKeys.addedOn", { date: formatRelative(k.created_at) })}</div>
                        </Td>
                        <Td className="font-mono text-xs whitespace-nowrap text-muted">{k.key_hint || "—"}</Td>
                        <Td className="max-w-64">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Badge tone={st.tone} dot title={k.last_error || undefined}>
                              {t(st.label)}
                            </Badge>
                            {status === "cooling_down" && (
                              <span className="text-[11px] whitespace-nowrap text-muted tabular-nums">
                                {t("providerKeys.cooldownLeft", { time: formatDuration(coolingLeft) })}
                              </span>
                            )}
                            {failed && (
                              <button
                                type="button"
                                className="text-[11px] font-medium text-accent hover:underline disabled:opacity-50"
                                onClick={() => setEnabled(k, true)}
                                disabled={busy}
                              >
                                {t("providerKeys.reenable")}
                              </button>
                            )}
                          </div>
                          {k.last_error && (
                            <button
                              type="button"
                              onClick={() => setExpanded((x) => (x === k.id ? null : k.id))}
                              title={errOpen ? undefined : k.last_error}
                              className={cx(
                                "mt-1 block w-full text-left text-[11px]",
                                failed ? "text-red-600 dark:text-red-400" : "text-muted",
                                errOpen ? "font-mono break-all whitespace-pre-wrap" : "truncate",
                              )}
                            >
                              {k.last_error_at && <span className="font-sans">{formatRelative(k.last_error_at)} · </span>}
                              {k.last_error}
                            </button>
                          )}
                        </Td>
                        <Td align="right" className="whitespace-nowrap">
                          {formatInt(k.requests_24h)}
                          {k.errors_24h > 0 && (
                            <div className="text-[11px] text-red-600 dark:text-red-400">{t("providerKeys.errors", { n: k.errors_24h })}</div>
                          )}
                        </Td>
                        <Td className="whitespace-nowrap text-muted" title={k.last_used_at ? formatDateTime(k.last_used_at) : undefined}>
                          {k.last_used_at ? formatRelative(k.last_used_at) : t("keys.never")}
                        </Td>
                        <Td>
                          <Toggle
                            size="sm"
                            checked={k.enabled && !failed}
                            disabled={busy || addBusy}
                            onChange={(v) => setEnabled(k, v)}
                            label={k.enabled && !failed ? t("common.disable") : t("common.enable")}
                          />
                        </Td>
                        <Td align="right">
                          <div className="flex justify-end gap-0.5">
                            <IconButton icon="edit" label={t("providerKeys.rename")} onClick={() => setRenaming({ id: k.id, name: k.name })} disabled={busyAny} />
                            <IconButton icon="trash" label={t("common.delete")} onClick={() => remove(k)} disabled={busyAny} className="hover:text-red-600" />
                          </div>
                        </Td>
                      </Tr>
                    );
                  })}
              </TBody>
            </Table>
          </div>
        )}
        {list.some((k) => FAILED.includes(k.status)) && <Notice tone="warning">{t("providerKeys.failedHint")}</Notice>}
      </div>
    </Dialog>
  );
}
