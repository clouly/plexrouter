import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { formatContext, formatPercent, formatPrice, formatTime, isFuture, parseNumber } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { AdminModel, Endpoint, EndpointInput, ModelInput, Provider } from "../../types";
import { Card, Notice, PageHeader } from "../../components/Card";
import { Badge, type BadgeTone } from "../../components/Badge";
import { Button, IconButton } from "../../components/Button";
import { CopyButton } from "../../components/CopyButton";
import { Dialog, useConfirm } from "../../components/Dialog";
import { EmptyState } from "../../components/EmptyState";
import { controlClass, Input, SearchInput, Textarea } from "../../components/Input";
import { Segmented } from "../../components/Segmented";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";
import { CatalogImportDialog } from "./CatalogImport";

type StatusFilter = "all" | "enabled" | "disabled";

/** Sell price vs. cost for one side (input or output): 0.2 = +20 %. */
function markupSide(price: number, cost: number): number {
  if (cost > 0) return price / cost - 1;
  return price > 0 ? Infinity : 0;
}

/**
 * Markup of a model's sell prices over its cheapest enabled endpoint with known costs
 * (cheapest = lowest input + output cost). `worst` is the lower of the input/output markups.
 * Returns null when no enabled endpoint has both costs set.
 */
export function modelMarkup(m: AdminModel): { endpoint: Endpoint; input: number; output: number; worst: number } | null {
  let best: Endpoint | null = null;
  for (const e of m.endpoints) {
    if (!e.enabled || e.input_cost == null || e.output_cost == null) continue;
    if (!best || e.input_cost + e.output_cost < (best.input_cost ?? 0) + (best.output_cost ?? 0)) best = e;
  }
  if (!best) return null;
  const input = markupSide(m.input_price, best.input_cost ?? 0);
  const output = markupSide(m.output_price, best.output_cost ?? 0);
  return { endpoint: best, input, output, worst: Math.min(input, output) };
}

function markupTone(v: number): BadgeTone {
  return v < 0 ? "danger" : v < 0.05 ? "warning" : "success";
}

export function AdminModelsPage() {
  const t = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminModels"), config?.name);
  const models = useAsync(() => api.admin.models(), []);
  const providers = useAsync(() => api.admin.providers(), []);
  const [editing, setEditing] = useState<AdminModel | "new" | null>(null);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [params, setParams] = useSearchParams();
  const [q, setQ] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");

  const all = models.data?.data ?? [];
  const list = useMemo(() => {
    const n = q.trim().toLowerCase();
    return all
      .filter((m) => (status === "all" ? true : status === "enabled" ? m.enabled : !m.enabled))
      .filter((m) => !n || m.slug.toLowerCase().includes(n) || m.name.toLowerCase().includes(n))
      .sort((a, b) => a.slug.localeCompare(b.slug));
  }, [all, q, status]);
  const disabledCount = all.filter((m) => !m.enabled).length;

  const replace = (m: AdminModel) => models.setData((d) => d && { data: d.data.map((x) => (x.id === m.id ? m : x)) });

  // /admin/models?edit=<id> (from overview alerts) opens that model's editor.
  const editParam = params.get("edit");
  useEffect(() => {
    if (!editParam || !models.data) return;
    const m = models.data.data.find((x) => String(x.id) === editParam);
    if (m) setEditing(m);
    const next = new URLSearchParams(params);
    next.delete("edit");
    setParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editParam, models.data]);

  const toggle = async (m: AdminModel, enabled: boolean) => {
    setBusyId(m.id);
    try {
      replace((await api.admin.updateModel(m.id, { enabled })).model);
      toast.success(enabled ? t("adminModels.enabledToast", { slug: m.slug }) : t("adminModels.disabledToast", { slug: m.slug }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const remove = async (m: AdminModel) => {
    const ok = await confirm({
      title: t("adminModels.deleteTitle"),
      message: t("adminModels.deleteMessage", { slug: m.slug }),
      confirmLabel: t("common.delete"),
      danger: true,
    });
    if (!ok) return;
    setBusyId(m.id);
    try {
      await api.admin.deleteModel(m.id);
      models.setData((d) => d && { data: d.data.filter((x) => x.id !== m.id) });
      toast.success(t("adminModels.deleted"));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <>
      <PageHeader
        title={t("adminModels.title")}
        description={t("adminModels.desc")}
        actions={
          <>
            <Button icon="plus" onClick={() => setEditing("new")}>
              {t("adminModels.add")}
            </Button>
            <Button variant="primary" icon="download" onClick={() => setCatalogOpen(true)}>
              {t("catalog.open")}
            </Button>
          </>
        }
      />
      <Notice className="mb-4">{t("adminModels.catalogHint")}</Notice>
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={setQ} placeholder={t("adminModels.search")} className="flex-1" />
        <Segmented
          value={status}
          onChange={setStatus}
          label={t("common.status")}
          options={[
            { value: "all", label: `${t("common.all")} (${all.length})` },
            { value: "enabled", label: t("common.enabled") },
            { value: "disabled", label: `${t("common.disabled")} (${disabledCount})` },
          ]}
        />
      </div>
      <Card flush>
        {!models.loading && !models.error && all.length === 0 ? (
          <EmptyState
            icon="cube"
            title={t("adminModels.emptyTitle")}
            description={t("adminModels.emptyDesc")}
            action={
              <div className="flex flex-wrap justify-center gap-2">
                <Button size="sm" variant="primary" icon="download" onClick={() => setCatalogOpen(true)}>
                  {t("catalog.open")}
                </Button>
                <Button size="sm" icon="plus" onClick={() => setEditing("new")}>
                  {t("adminModels.add")}
                </Button>
              </div>
            }
          />
        ) : (
          <Table stickyLast minWidth={1000}>
            <THead>
              <Th>{t("adminModels.model")}</Th>
              <Th align="right">{t("models.context")}</Th>
              <Th align="right">{t("adminModels.inputPrice")}</Th>
              <Th align="right">{t("adminModels.outputPrice")}</Th>
              <Th title={t("adminModels.markupHint")}>{t("adminModels.markup")}</Th>
              <Th>{t("adminModels.endpoints")}</Th>
              <Th>{t("common.enabled")}</Th>
              <Th align="right">{t("common.actions")}</Th>
            </THead>
            <TBody>
              <TableState
                colSpan={8}
                loading={models.loading}
                error={models.error}
                empty={list.length === 0}
                onRetry={models.reload}
                emptyText={all.length ? t("common.noMatch") : undefined}
              />
              {!models.error &&
                list.map((m) => {
                  const unpriced = m.input_price === 0 && m.output_price === 0;
                  const activeEndpoints = m.endpoints.filter((e) => e.enabled).length;
                  const mk = modelMarkup(m);
                  const tripped = m.endpoints.filter((e) => isFuture(e.circuit_open_until));
                  return (
                    <Tr key={m.id} className={cx(!m.enabled && "opacity-75")}>
                      <Td>
                        <div className="flex items-center gap-0.5">
                          <span className="font-mono text-xs whitespace-nowrap">{m.slug}</span>
                          <CopyButton text={m.slug} />
                        </div>
                        <div className="max-w-72 truncate text-xs text-muted" title={m.name}>
                          {m.name}
                        </div>
                      </Td>
                      <Td align="right" className="text-muted">
                        {formatContext(m.context_length)}
                      </Td>
                      <Td align="right">{formatPrice(m.input_price)}</Td>
                      <Td align="right">
                        <div className="flex items-center justify-end gap-1.5">
                          {unpriced && <Badge tone="warning">{t("adminModels.unpriced")}</Badge>}
                          {formatPrice(m.output_price)}
                        </div>
                      </Td>
                      <Td>
                        {mk ? (
                          <Badge
                            tone={markupTone(mk.worst)}
                            title={t("adminModels.markupTooltip", {
                              provider: mk.endpoint.provider_name,
                              input: formatPercent(mk.input, { sign: true, digits: 0 }),
                              output: formatPercent(mk.output, { sign: true, digits: 0 }),
                            })}
                          >
                            {formatPercent(mk.worst, { sign: true, digits: 0 })}
                          </Badge>
                        ) : m.endpoints.length > 0 ? (
                          <Badge title={t("adminModels.noCostHint")}>{t("adminModels.noCost")}</Badge>
                        ) : (
                          <span className="text-xs text-muted">—</span>
                        )}
                      </Td>
                      <Td>
                        <span
                          className={cx("text-[13px] tabular-nums", activeEndpoints === 0 && "text-red-600 dark:text-red-400")}
                          title={m.endpoints.map((e) => `${e.provider_name}: ${e.upstream_model} (p${e.priority}${e.enabled ? "" : ", off"})`).join("\n")}
                        >
                          {t("adminModels.endpointCount", { active: activeEndpoints, total: m.endpoints.length })}
                        </span>
                        {m.endpoints.length > 0 && (
                          <div className="max-w-48 truncate text-[11px] text-muted">{[...new Set(m.endpoints.map((e) => e.provider_name))].join(", ")}</div>
                        )}
                        {tripped.length > 0 && (
                          <Badge
                            tone="warning"
                            className="mt-1"
                            title={tripped.map((e) => `${e.provider_name}: ${e.upstream_model} → ${formatTime(e.circuit_open_until)}`).join("\n")}
                          >
                            {t("adminModels.circuitCount", { n: tripped.length })}
                          </Badge>
                        )}
                      </Td>
                      <Td>
                        <Toggle size="sm" checked={m.enabled} disabled={busyId === m.id} onChange={(v) => toggle(m, v)} label={m.enabled ? t("common.disable") : t("common.enable")} />
                      </Td>
                      <Td align="right">
                        <div className="flex justify-end gap-0.5">
                          <IconButton icon="edit" label={t("common.edit")} onClick={() => setEditing(m)} />
                          <IconButton icon="trash" label={t("common.delete")} onClick={() => remove(m)} disabled={busyId === m.id} className="hover:text-red-600" />
                        </div>
                      </Td>
                    </Tr>
                  );
                })}
            </TBody>
          </Table>
        )}
      </Card>

      <ModelDialog
        target={editing}
        providers={providers.data?.data ?? []}
        providersError={providers.error}
        onClose={() => setEditing(null)}
        onSaved={(m, isNew) => {
          if (isNew) models.setData((d) => ({ data: [...(d?.data ?? []), m] }));
          else replace(m);
          setEditing(null);
          toast.success(isNew ? t("adminModels.created") : t("adminModels.saved"));
        }}
      />
      <CatalogImportDialog
        open={catalogOpen}
        providers={providers.data?.data ?? []}
        providersLoading={providers.loading}
        providersError={providers.error}
        onClose={() => setCatalogOpen(false)}
        onImported={models.reload}
      />
    </>
  );
}

interface EndpointRow {
  key: number;
  provider_id: number | "";
  upstream_model: string;
  priority: string;
  enabled: boolean;
  input_cost: string; // "" = unknown
  output_cost: string;
  circuit_open_until: string | null;
}

const costText = (v: number | null | undefined) => (v === null || v === undefined ? "" : String(v));

interface FormErrors {
  slug?: string;
  name?: string;
  context?: string;
  input?: string;
  output?: string;
  endpoints?: string;
}

let rowKey = 1;

function ModelDialog({
  target,
  providers,
  providersError,
  onClose,
  onSaved,
}: {
  target: AdminModel | "new" | null;
  providers: Provider[];
  providersError?: Error;
  onClose: () => void;
  onSaved: (m: AdminModel, isNew: boolean) => void;
}) {
  const t = useT();
  const toast = useToast();
  const isNew = target === "new";
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [context, setContext] = useState("");
  const [input, setInput] = useState("");
  const [output, setOutput] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [rows, setRows] = useState<EndpointRow[]>([]);
  const [errors, setErrors] = useState<FormErrors>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!target) return;
    setErrors({});
    if (target === "new") {
      setSlug("");
      setName("");
      setDescription("");
      setContext("");
      setInput("");
      setOutput("");
      setEnabled(true);
      setRows([]);
    } else {
      setSlug(target.slug);
      setName(target.name);
      setDescription(target.description);
      setContext(target.context_length ? String(target.context_length) : "");
      setInput(String(target.input_price));
      setOutput(String(target.output_price));
      setEnabled(target.enabled);
      setRows(
        [...target.endpoints]
          .sort((a, b) => a.priority - b.priority)
          .map((e) => ({
            key: rowKey++,
            provider_id: e.provider_id,
            upstream_model: e.upstream_model,
            priority: String(e.priority),
            enabled: e.enabled,
            input_cost: costText(e.input_cost),
            output_cost: costText(e.output_cost),
            circuit_open_until: e.circuit_open_until ?? null,
          })),
      );
    }
  }, [target]);

  const updateRow = (key: number, patch: Partial<EndpointRow>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const addRow = () =>
    setRows((rs) => [
      ...rs,
      {
        key: rowKey++,
        provider_id: providers[0]?.id ?? "",
        upstream_model: rs.length === 0 && slug.includes("/") ? slug.slice(slug.indexOf("/") + 1) : "",
        priority: String(rs.length),
        enabled: true,
        input_cost: "",
        output_cost: "",
        circuit_open_until: null,
      },
    ]);

  const submit = async () => {
    const errs: FormErrors = {};
    const s = slug.trim();
    if (!s) errs.slug = t("adminModels.slugRequired");
    else if (/\s/.test(s)) errs.slug = t("adminModels.slugInvalid");
    if (!name.trim()) errs.name = t("adminModels.nameRequired");
    const ctx = parseNumber(context);
    if (ctx !== null && (Number.isNaN(ctx) || ctx < 0 || !Number.isInteger(ctx))) errs.context = t("adminModels.contextInvalid");
    const ip = parseNumber(input);
    const op = parseNumber(output);
    if (ip === null || Number.isNaN(ip) || ip < 0) errs.input = t("adminModels.priceInvalid");
    if (op === null || Number.isNaN(op) || op < 0) errs.output = t("adminModels.priceInvalid");
    const endpoints: EndpointInput[] = [];
    const badCost = (v: number | null) => v !== null && (Number.isNaN(v) || v < 0);
    for (const r of rows) {
      const pr = parseNumber(r.priority);
      const ic = parseNumber(r.input_cost);
      const oc = parseNumber(r.output_cost);
      if (r.provider_id === "" || !r.upstream_model.trim() || (pr !== null && (Number.isNaN(pr) || !Number.isInteger(pr)))) {
        errs.endpoints = t("adminModels.endpointInvalid");
        break;
      }
      if (badCost(ic) || badCost(oc)) {
        errs.endpoints = t("adminModels.costInvalid");
        break;
      }
      endpoints.push({
        provider_id: r.provider_id,
        upstream_model: r.upstream_model.trim(),
        priority: pr ?? 0,
        enabled: r.enabled,
        input_cost: ic,
        output_cost: oc,
      });
    }
    setErrors(errs);
    if (Object.keys(errs).length) return;

    const body: ModelInput = {
      slug: s,
      name: name.trim(),
      description: description.trim(),
      context_length: ctx ?? 0,
      input_price: ip as number,
      output_price: op as number,
      enabled,
      endpoints,
    };
    setBusy(true);
    try {
      if (isNew) onSaved((await api.admin.createModel(body)).model, true);
      else if (target) onSaved((await api.admin.updateModel(target.id, body)).model, false);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const zeroPriceEnabled = enabled && parseNumber(input) === 0 && parseNumber(output) === 0;

  return (
    <Dialog
      open={!!target}
      onClose={onClose}
      dismissible={!busy}
      onSubmit={submit}
      size="xl"
      title={isNew ? t("adminModels.addTitle") : t("adminModels.editTitle")}
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
        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t("adminModels.slug")}
            placeholder="openai/gpt-4o-mini"
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            error={errors.slug}
            hint={t("adminModels.slugHint")}
            disabled={busy}
            required
            className="font-mono text-[13px]"
            spellCheck={false}
          />
          <Input label={t("adminModels.name")} placeholder="GPT-4o mini" value={name} onChange={(e) => setName(e.target.value)} error={errors.name} disabled={busy} required />
        </div>
        <Textarea label={t("adminModels.description")} value={description} onChange={(e) => setDescription(e.target.value)} disabled={busy} rows={2} />
        <div className="grid gap-4 sm:grid-cols-3">
          <Input
            label={t("adminModels.contextLength")}
            type="number"
            min={0}
            step={1}
            inputMode="numeric"
            placeholder="128000"
            value={context}
            onChange={(e) => setContext(e.target.value)}
            error={errors.context}
            disabled={busy}
          />
          <Input
            label={t("adminModels.inputPrice")}
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="0.15"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            error={errors.input}
            hint={t("adminModels.perMillionUsd")}
            disabled={busy}
            required
          />
          <Input
            label={t("adminModels.outputPrice")}
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            placeholder="0.60"
            value={output}
            onChange={(e) => setOutput(e.target.value)}
            error={errors.output}
            hint={t("adminModels.perMillionUsd")}
            disabled={busy}
            required
          />
        </div>
        <div className="flex items-center justify-between gap-4 rounded-lg border border-line px-3 py-2.5">
          <div>
            <div className="text-[13px] font-medium">{t("common.enabled")}</div>
            <div className="text-xs text-muted">{t("adminModels.enabledHint")}</div>
          </div>
          <Toggle checked={enabled} onChange={setEnabled} disabled={busy} label={t("common.enabled")} />
        </div>
        {zeroPriceEnabled && <Notice tone="warning">{t("adminModels.zeroPriceWarning")}</Notice>}

        <div>
          <div className="mb-1.5 flex items-center justify-between gap-2">
            <div>
              <div className="text-[13px] font-medium">{t("adminModels.endpoints")}</div>
              <div className="text-xs text-muted">{t("adminModels.endpointsHint")}</div>
            </div>
            <Button size="sm" icon="plus" onClick={addRow} disabled={busy || providers.length === 0}>
              {t("adminModels.addEndpoint")}
            </Button>
          </div>
          {providersError && <Notice tone="danger">{providersError.message}</Notice>}
          {!providersError && providers.length === 0 && <Notice tone="warning">{t("adminModels.noProviders")}</Notice>}
          {errors.endpoints && <p className="mb-1.5 text-xs text-red-600 dark:text-red-400">{errors.endpoints}</p>}
          {rows.length === 0 ? (
            <div className="rounded-lg border border-dashed border-line px-3 py-5 text-center text-[13px] text-muted">{t("adminModels.noEndpoints")}</div>
          ) : (
            <div className="flex flex-col gap-2">
              <div className="hidden grid-cols-[minmax(0,10rem)_minmax(0,1fr)_5.5rem_5.5rem_4.5rem_2.5rem_2rem] gap-2 px-0.5 text-[11px] font-medium text-muted sm:grid">
                <span>{t("adminModels.provider")}</span>
                <span>{t("adminModels.upstreamModel")}</span>
                <span title={t("adminModels.costHint")}>{t("adminModels.costIn")}</span>
                <span title={t("adminModels.costHint")}>{t("adminModels.costOut")}</span>
                <span>{t("adminModels.priority")}</span>
                <span>{t("adminModels.on")}</span>
                <span />
              </div>
              {rows.map((r) => {
                const known = r.provider_id === "" || providers.some((p) => p.id === r.provider_id);
                const circuit = isFuture(r.circuit_open_until);
                return (
                  <div
                    key={r.key}
                    className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_4.5rem_auto_auto] items-center gap-2 rounded-lg border border-line p-2 sm:grid-cols-[minmax(0,10rem)_minmax(0,1fr)_5.5rem_5.5rem_4.5rem_2.5rem_2rem] sm:border-0 sm:p-0"
                  >
                    <select
                      className={cx(controlClass, "col-span-5 h-8 text-[13px] sm:col-span-1")}
                      value={r.provider_id}
                      onChange={(e) => updateRow(r.key, { provider_id: e.target.value ? Number(e.target.value) : "" })}
                      disabled={busy}
                      aria-label={t("adminModels.provider")}
                    >
                      <option value="">{t("adminModels.selectProvider")}</option>
                      {!known && <option value={r.provider_id}>#{r.provider_id}</option>}
                      {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                          {p.name}
                          {p.enabled ? "" : ` (${t("common.disabled")})`}
                        </option>
                      ))}
                    </select>
                    <input
                      className={cx(controlClass, "col-span-5 h-8 font-mono text-[13px] sm:col-span-1")}
                      placeholder={t("adminModels.upstreamPlaceholder")}
                      value={r.upstream_model}
                      onChange={(e) => updateRow(r.key, { upstream_model: e.target.value })}
                      disabled={busy}
                      aria-label={t("adminModels.upstreamModel")}
                      spellCheck={false}
                    />
                    <input
                      className={cx(controlClass, "h-8 px-2 text-[13px]")}
                      type="number"
                      min={0}
                      step="any"
                      inputMode="decimal"
                      placeholder={t("adminModels.costIn")}
                      value={r.input_cost}
                      onChange={(e) => updateRow(r.key, { input_cost: e.target.value })}
                      disabled={busy}
                      aria-label={t("adminModels.costIn")}
                      title={t("adminModels.costHint")}
                    />
                    <input
                      className={cx(controlClass, "h-8 px-2 text-[13px]")}
                      type="number"
                      min={0}
                      step="any"
                      inputMode="decimal"
                      placeholder={t("adminModels.costOut")}
                      value={r.output_cost}
                      onChange={(e) => updateRow(r.key, { output_cost: e.target.value })}
                      disabled={busy}
                      aria-label={t("adminModels.costOut")}
                      title={t("adminModels.costHint")}
                    />
                    <input
                      className={cx(controlClass, "h-8 text-[13px]")}
                      type="number"
                      step={1}
                      value={r.priority}
                      onChange={(e) => updateRow(r.key, { priority: e.target.value })}
                      disabled={busy}
                      aria-label={t("adminModels.priority")}
                      title={t("adminModels.priorityHint")}
                    />
                    <Toggle size="sm" checked={r.enabled} onChange={(v) => updateRow(r.key, { enabled: v })} disabled={busy} label={t("common.enabled")} />
                    <IconButton icon="trash" label={t("common.remove")} onClick={() => setRows((rs) => rs.filter((x) => x.key !== r.key))} disabled={busy} className="hover:text-red-600" />
                    {circuit && (
                      <div className="col-span-full -mt-1 sm:mt-0">
                        <Badge tone="warning" dot title={t("adminModels.circuitHint")}>
                          {t("adminModels.circuitOpen", { time: formatTime(r.circuit_open_until) })}
                        </Badge>
                      </div>
                    )}
                  </div>
                );
              })}
              <p className="text-[11px] text-muted">
                {t("adminModels.priorityHint")} {t("adminModels.costHint")}
              </p>
            </div>
          )}
        </div>
      </div>
    </Dialog>
  );
}
