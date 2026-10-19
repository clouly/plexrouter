import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { useT, type MessageKey } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatDate, formatInt, formatLatency, formatPercent } from "../../lib/format";
import { cx } from "../../lib/cx";
import { PROVIDER_TYPES, type Provider, type ProviderHealthStatus, type ProviderTestResult, type ProviderType } from "../../types";
import { Card, Notice, PageHeader } from "../../components/Card";
import { Badge, type BadgeTone } from "../../components/Badge";
import { Button, IconButton } from "../../components/Button";
import { Dialog, useConfirm } from "../../components/Dialog";
import { EmptyState, ErrorState, LoadingState } from "../../components/EmptyState";
import { Checkbox, Input, SearchInput, Select } from "../../components/Input";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";
import { ProviderKeysDialog } from "./ProviderKeys";

export const DEFAULT_BASE_URLS: Record<ProviderType, string> = {
  openai: "https://api.openai.com/v1",
  anthropic: "https://api.anthropic.com",
  gemini: "https://generativelanguage.googleapis.com",
  openai_compatible: "",
};

export const PROVIDER_TYPE_LABELS: Record<ProviderType, MessageKey> = {
  openai: "providers.typeOpenai",
  anthropic: "providers.typeAnthropic",
  gemini: "providers.typeGemini",
  openai_compatible: "providers.typeCompatible",
};

export const HEALTH: Record<ProviderHealthStatus, { label: MessageKey; tone: BadgeTone }> = {
  healthy: { label: "providers.healthHealthy", tone: "success" },
  degraded: { label: "providers.healthDegraded", tone: "warning" },
  down: { label: "providers.healthDown", tone: "danger" },
  idle: { label: "providers.healthIdle", tone: "neutral" },
  no_keys: { label: "providers.healthNoKeys", tone: "danger" },
};

/** Default slug prefix for imports: provider name lowercased, whitespace → dashes. */
function slugPrefix(name: string): string {
  return name.trim().toLowerCase().replace(/\s+/g, "-");
}

export function AdminProvidersPage() {
  const t = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminProviders"), config?.name);
  const providers = useAsync(() => api.admin.providers(), []);
  const [editing, setEditing] = useState<Provider | "new" | null>(null);
  const [importing, setImporting] = useState<Provider | null>(null);
  const [keysFor, setKeysFor] = useState<Provider | null>(null);
  const [params, setParams] = useSearchParams();
  const [busyId, setBusyId] = useState<number | null>(null);
  const [testing, setTesting] = useState<number | null>(null);
  const [results, setResults] = useState<Record<number, ProviderTestResult>>({});

  const list = providers.data?.data ?? [];
  const replace = (p: Provider) => providers.setData((d) => d && { data: d.data.map((x) => (x.id === p.id ? p : x)) });

  // /admin/providers?keys=<id> (from overview alerts) opens that provider's keys directly.
  const keysParam = params.get("keys");
  useEffect(() => {
    if (!keysParam || !providers.data) return;
    const p = providers.data.data.find((x) => String(x.id) === keysParam);
    if (p) setKeysFor(p);
    const next = new URLSearchParams(params);
    next.delete("keys");
    setParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keysParam, providers.data]);

  const toggle = async (p: Provider, enabled: boolean) => {
    setBusyId(p.id);
    try {
      replace((await api.admin.updateProvider(p.id, { enabled })).provider);
      toast.success(enabled ? t("providers.enabledToast") : t("providers.disabledToast"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const test = async (p: Provider) => {
    setTesting(p.id);
    try {
      const r = await api.admin.testProvider(p.id);
      setResults((m) => ({ ...m, [p.id]: r }));
      if (r.ok) toast.success(t("providers.testOk", { name: p.name, latency: formatLatency(r.latency_ms) }));
      else toast.error(t("providers.testFailed", { name: p.name, message: r.message }));
    } catch (e) {
      setResults((m) => ({ ...m, [p.id]: { ok: false, latency_ms: 0, message: errorMessage(e) } }));
      toast.error(errorMessage(e));
    } finally {
      setTesting(null);
    }
  };

  const remove = async (p: Provider) => {
    const ok = await confirm({
      title: t("providers.deleteTitle"),
      message: t("providers.deleteMessage", { name: p.name }),
      confirmLabel: t("common.delete"),
      danger: true,
    });
    if (!ok) return;
    setBusyId(p.id);
    try {
      await api.admin.deleteProvider(p.id);
      providers.setData((d) => d && { data: d.data.filter((x) => x.id !== p.id) });
      toast.success(t("providers.deleted"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("providers.title")}
        description={t("providers.desc")}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setEditing("new")}>
            {t("providers.add")}
          </Button>
        }
      />
      <Card flush>
        {!providers.loading && !providers.error && list.length === 0 ? (
          <EmptyState
            icon="server"
            title={t("providers.emptyTitle")}
            description={t("providers.emptyDesc")}
            action={
              <Button size="sm" variant="primary" icon="plus" onClick={() => setEditing("new")}>
                {t("providers.add")}
              </Button>
            }
          />
        ) : (
          <Table stickyLast minWidth={1240}>
            <THead>
              <Th>{t("providers.name")}</Th>
              <Th>{t("providers.type")}</Th>
              <Th>{t("providers.baseUrl")}</Th>
              <Th>{t("providers.keys")}</Th>
              <Th>{t("providers.health")}</Th>
              <Th>{t("providers.lastTest")}</Th>
              <Th>{t("common.enabled")}</Th>
              <Th>{t("common.created")}</Th>
              <Th align="right">{t("common.actions")}</Th>
            </THead>
            <TBody>
              <TableState colSpan={9} loading={providers.loading} error={providers.error} empty={list.length === 0} onRetry={providers.reload} />
              {!providers.error &&
                list.map((p) => {
                  const r = results[p.id];
                  return (
                    <Tr key={p.id} className={cx(!p.enabled && "opacity-70")}>
                      <Td className="font-medium whitespace-nowrap">{p.name}</Td>
                      <Td>
                        <Badge>{t(PROVIDER_TYPE_LABELS[p.type] ?? "providers.typeCompatible")}</Badge>
                      </Td>
                      <Td className="max-w-64 truncate font-mono text-xs" title={p.base_url || DEFAULT_BASE_URLS[p.type]}>
                        {p.base_url || (
                          <span className="text-muted">
                            {DEFAULT_BASE_URLS[p.type] || "—"} <span className="font-sans">({t("providers.default")})</span>
                          </span>
                        )}
                      </Td>
                      <Td>
                        <KeysCell provider={p} onOpen={() => setKeysFor(p)} />
                      </Td>
                      <Td>
                        <HealthCell provider={p} />
                      </Td>
                      <Td>
                        {testing === p.id ? (
                          <span className="text-xs text-muted">{t("providers.testing")}</span>
                        ) : r ? (
                          <Badge tone={r.ok ? "success" : "danger"} dot title={r.message}>
                            {r.ok ? `${t("providers.ok")} · ${formatLatency(r.latency_ms)}` : t("providers.failed")}
                          </Badge>
                        ) : (
                          <span className="text-xs text-muted">—</span>
                        )}
                        {r && !r.ok && r.message && testing !== p.id && (
                          <div className="mt-1 max-w-56 truncate text-[11px] text-red-600 dark:text-red-400" title={r.message}>
                            {r.message}
                          </div>
                        )}
                      </Td>
                      <Td>
                        <Toggle size="sm" checked={p.enabled} disabled={busyId === p.id} onChange={(v) => toggle(p, v)} label={p.enabled ? t("common.disable") : t("common.enable")} />
                      </Td>
                      <Td className="whitespace-nowrap text-muted">{formatDate(p.created_at)}</Td>
                      <Td align="right">
                        <div className="flex items-center justify-end gap-1">
                          {/* Labels collapse to icons on phones so the pinned actions column stays narrow. */}
                          <Button size="xs" icon="play" onClick={() => test(p)} loading={testing === p.id} title={t("providers.test")} aria-label={t("providers.test")}>
                            <span className="hidden md:inline">{t("providers.test")}</span>
                          </Button>
                          <Button size="xs" icon="key" onClick={() => setKeysFor(p)} title={t("providers.manageKeys")} aria-label={t("providers.manageKeys")}>
                            <span className="hidden md:inline">{t("providers.keys")}</span>
                          </Button>
                          <Button size="xs" icon="download" onClick={() => setImporting(p)} title={t("providers.importModels")} aria-label={t("providers.importModels")}>
                            <span className="hidden md:inline">{t("providers.importModels")}</span>
                          </Button>
                          <IconButton icon="edit" label={t("common.edit")} onClick={() => setEditing(p)} />
                          <IconButton icon="trash" label={t("common.delete")} onClick={() => remove(p)} disabled={busyId === p.id} className="hover:text-red-600" />
                        </div>
                      </Td>
                    </Tr>
                  );
                })}
            </TBody>
          </Table>
        )}
      </Card>

      <ProviderDialog
        target={editing}
        onClose={() => setEditing(null)}
        onSaved={(p, isNew) => {
          if (isNew) providers.setData((d) => ({ data: [...(d?.data ?? []), p] }));
          else replace(p);
          setEditing(null);
          toast.success(isNew ? t("providers.created") : t("providers.saved"));
        }}
      />
      <ImportDialog provider={importing} onClose={() => setImporting(null)} />
      <ProviderKeysDialog provider={keysFor} onClose={() => setKeysFor(null)} onChanged={providers.reload} />
    </>
  );
}

function KeysCell({ provider: p, onOpen }: { provider: Provider; onOpen: () => void }) {
  const t = useT();
  const total = p.keys_total ?? 0;
  const active = p.keys_active ?? 0;
  const warn = active === 0 && (total > 0 || p.health?.status === "no_keys");
  return (
    <button type="button" onClick={onOpen} className="flex flex-col items-start gap-1 text-left" title={t("providers.manageKeys")}>
      <span className="text-[13px] whitespace-nowrap tabular-nums hover:text-accent">{t("providers.keysActive", { active, total })}</span>
      {warn && <Badge tone="warning">{t("providers.noActiveKeys")}</Badge>}
    </button>
  );
}

function HealthCell({ provider: p }: { provider: Provider }) {
  const t = useT();
  const h = p.health;
  if (!h) return <span className="text-xs text-muted">—</span>;
  const meta = HEALTH[h.status] ?? HEALTH.idle;
  const line =
    h.requests_24h > 0
      ? [
          h.success_rate_24h !== null ? formatPercent(h.success_rate_24h) : null,
          h.avg_latency_ms_24h !== null ? formatLatency(h.avg_latency_ms_24h) : null,
          t("providers.healthRequests", { n: formatInt(h.requests_24h) }),
        ]
          .filter(Boolean)
          .join(" · ")
      : t("providers.healthNoRequests");
  return (
    <div className="flex flex-col items-start gap-1" title={t("providers.healthTooltip")}>
      <Badge tone={meta.tone} dot>
        {t(meta.label)}
      </Badge>
      <span className="text-[11px] whitespace-nowrap text-muted tabular-nums">{line}</span>
    </div>
  );
}

function ProviderDialog({ target, onClose, onSaved }: { target: Provider | "new" | null; onClose: () => void; onSaved: (p: Provider, isNew: boolean) => void }) {
  const t = useT();
  const toast = useToast();
  const isNew = target === "new";
  const [name, setName] = useState("");
  const [type, setType] = useState<ProviderType>("openai");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [errors, setErrors] = useState<{ name?: string; baseUrl?: string }>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!target) return;
    setErrors({});
    setApiKey("");
    if (target === "new") {
      setName("");
      setType("openai");
      setBaseUrl("");
      setEnabled(true);
    } else {
      setName(target.name);
      setType(target.type);
      setBaseUrl(target.base_url);
      setEnabled(target.enabled);
    }
  }, [target]);

  const compatible = type === "openai_compatible";

  const submit = async () => {
    const errs: typeof errors = {};
    const url = baseUrl.trim();
    if (!name.trim()) errs.name = t("providers.nameRequired");
    if (compatible && !url) errs.baseUrl = t("providers.baseUrlRequired");
    else if (url && !/^https?:\/\/\S+$/i.test(url)) errs.baseUrl = t("providers.baseUrlInvalid");
    setErrors(errs);
    if (Object.keys(errs).length) return;
    setBusy(true);
    try {
      if (isNew) {
        const key = apiKey.trim();
        const r = await api.admin.createProvider({ name: name.trim(), type, base_url: url, enabled, ...(key ? { api_key: key } : {}) });
        onSaved(r.provider, true);
      } else if (target) {
        const r = await api.admin.updateProvider(target.id, { name: name.trim(), type, base_url: url, enabled });
        onSaved(r.provider, false);
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
      title={isNew ? t("providers.addTitle") : t("providers.editTitle")}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" variant="primary" loading={busy}>
            {isNew ? t("common.create") : t("common.save")}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        <Input label={t("providers.name")} placeholder={t("providers.namePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} error={errors.name} disabled={busy} required />
        <Select label={t("providers.type")} value={type} onChange={(e) => setType(e.target.value as ProviderType)} disabled={busy}>
          {PROVIDER_TYPES.map((ty) => (
            <option key={ty} value={ty}>
              {t(PROVIDER_TYPE_LABELS[ty])}
            </option>
          ))}
        </Select>
        <Input
          label={t("providers.baseUrl")}
          required={compatible}
          type="url"
          placeholder={compatible ? "https://api.deepseek.com/v1" : DEFAULT_BASE_URLS[type]}
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          error={errors.baseUrl}
          hint={compatible ? t("providers.baseUrlCompatibleHint") : t("providers.baseUrlHint")}
          disabled={busy}
          className="font-mono text-[13px]"
          spellCheck={false}
        />
        {isNew ? (
          <Input
            label={t("providers.apiKey")}
            type="password"
            autoComplete="new-password"
            placeholder="sk-..."
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            hint={compatible ? t("providers.apiKeyOptional") : t("providers.apiKeyFirst")}
            disabled={busy}
            className="font-mono text-[13px]"
            spellCheck={false}
          />
        ) : (
          <p className="text-xs text-muted">{t("providers.keysManagedHint")}</p>
        )}
        <div className="flex items-center justify-between gap-4 rounded-lg border border-line px-3 py-2.5">
          <div>
            <div className="text-[13px] font-medium">{t("common.enabled")}</div>
            <div className="text-xs text-muted">{t("providers.enabledHint")}</div>
          </div>
          <Toggle checked={enabled} onChange={setEnabled} disabled={busy} label={t("common.enabled")} />
        </div>
      </div>
    </Dialog>
  );
}

function ImportDialog({ provider, onClose }: { provider: Provider | null; onClose: () => void }) {
  const t = useT();
  const toast = useToast();
  const upstream = useAsync(() => (provider ? api.admin.upstreamModels(provider.id) : Promise.resolve({ data: [] })), [provider?.id]);
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [prefix, setPrefix] = useState("");
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ created: number; skipped: number } | null>(null);

  useEffect(() => {
    if (!provider) return;
    setQ("");
    setSelected(new Set());
    setPrefix(slugPrefix(provider.name));
    setResult(null);
  }, [provider]);

  const ids = useMemo(() => [...new Set((upstream.data?.data ?? []).map((m) => m.id))].sort((a, b) => a.localeCompare(b)), [upstream.data]);
  const filtered = useMemo(() => {
    const n = q.trim().toLowerCase();
    return n ? ids.filter((id) => id.toLowerCase().includes(n)) : ids;
  }, [ids, q]);
  const allFilteredSelected = filtered.length > 0 && filtered.every((id) => selected.has(id));
  const cleanPrefix = prefix.trim().replace(/\/+$/, "");

  const toggleOne = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const toggleAll = () =>
    setSelected((s) => {
      const n = new Set(s);
      if (allFilteredSelected) filtered.forEach((id) => n.delete(id));
      else filtered.forEach((id) => n.add(id));
      return n;
    });

  const submit = async () => {
    if (!provider || selected.size === 0 || !cleanPrefix) return;
    setBusy(true);
    try {
      const r = await api.admin.importModels({ provider_id: provider.id, upstream_models: [...selected], slug_prefix: cleanPrefix });
      setResult(r);
      toast.success(t("import.done", { created: r.created, skipped: r.skipped }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const example = [...selected][0] ?? filtered[0] ?? "model-id";

  return (
    <Dialog
      open={!!provider}
      onClose={onClose}
      dismissible={!busy}
      size="lg"
      onSubmit={result ? undefined : submit}
      title={t("import.title", { name: provider?.name ?? "" })}
      description={result ? undefined : t("import.desc")}
      footer={
        result ? (
          <>
            <Link to="/admin/models" onClick={onClose} className="inline-flex h-9 items-center rounded-lg border border-line bg-surface px-3.5 text-sm font-medium hover:bg-subtle">
              {t("import.goToModels")}
            </Link>
            <Button variant="primary" onClick={onClose}>
              {t("common.done")}
            </Button>
          </>
        ) : (
          <>
            <Button onClick={onClose} disabled={busy}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" variant="primary" loading={busy} disabled={selected.size === 0 || !cleanPrefix}>
              {t("import.submit", { n: selected.size })}
            </Button>
          </>
        )
      }
    >
      {result ? (
        <div className="flex flex-col gap-3">
          <div className="grid grid-cols-2 gap-3">
            <div className="rounded-lg border border-line p-3">
              <div className="text-xs text-muted">{t("import.created")}</div>
              <div className="mt-1 text-2xl font-semibold tabular-nums">{formatInt(result.created)}</div>
            </div>
            <div className="rounded-lg border border-line p-3">
              <div className="text-xs text-muted">{t("import.skipped")}</div>
              <div className="mt-1 text-2xl font-semibold tabular-nums">{formatInt(result.skipped)}</div>
            </div>
          </div>
          <Notice tone="warning">{t("import.afterHint")}</Notice>
        </div>
      ) : upstream.error ? (
        <ErrorState error={upstream.error} onRetry={upstream.reload} />
      ) : upstream.loading ? (
        <LoadingState label={t("import.loading")} />
      ) : (
        <div className="flex flex-col gap-3">
          <Input
            label={t("import.prefix")}
            value={prefix}
            onChange={(e) => setPrefix(e.target.value)}
            disabled={busy}
            className="font-mono text-[13px]"
            spellCheck={false}
            error={!cleanPrefix ? t("import.prefixRequired") : undefined}
            hint={t("import.prefixHint", { example: `${cleanPrefix || "prefix"}/${example}` })}
          />
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <SearchInput value={q} onChange={setQ} placeholder={t("import.search")} className="flex-1" />
            <Button size="sm" onClick={toggleAll} disabled={filtered.length === 0 || busy}>
              {allFilteredSelected ? t("import.deselectAll", { n: filtered.length }) : t("import.selectAll", { n: filtered.length })}
            </Button>
          </div>
          <div className="flex items-center justify-between text-xs text-muted">
            <span>{t("import.selectedCount", { n: selected.size, total: ids.length })}</span>
            {selected.size > 0 && (
              <button type="button" className="font-medium text-accent hover:underline" onClick={() => setSelected(new Set())} disabled={busy}>
                {t("import.clear")}
              </button>
            )}
          </div>
          <div className="max-h-72 overflow-y-auto rounded-lg border border-line">
            {ids.length === 0 ? (
              <EmptyState icon="cube" title={t("import.none")} />
            ) : filtered.length === 0 ? (
              <div className="px-3 py-8 text-center text-[13px] text-muted">{t("common.noMatch")}</div>
            ) : (
              <ul className="divide-y divide-line">
                {filtered.map((id) => (
                  <li key={id} className="px-3 py-1.5 hover:bg-subtle/60">
                    <Checkbox checked={selected.has(id)} onChange={(v) => toggleOne(id, v)} disabled={busy} label={<span className="font-mono text-xs break-all">{id}</span>} />
                  </li>
                ))}
              </ul>
            )}
          </div>
          <p className="text-xs text-muted">{t("import.hint")}</p>
        </div>
      )}
    </Dialog>
  );
}
