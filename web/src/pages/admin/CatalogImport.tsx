import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, api, errorMessage } from "../../api";
import { useT } from "../../i18n";
import { useAsync } from "../../lib/hooks";
import { formatCachePrice, formatContext, formatInt, formatPrice, formatRelative, parseNumber } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { CatalogImportResult, CatalogModel, Provider } from "../../types";
import { Notice } from "../../components/Card";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Dialog } from "../../components/Dialog";
import { EmptyState, ErrorState, LoadingState } from "../../components/EmptyState";
import { Checkbox, controlClass, Input, SearchInput, Select } from "../../components/Input";
import { Segmented } from "../../components/Segmented";
import { useToast } from "../../components/Toast";

type Filter = "all" | "matched" | "new";

/** Rounds a computed USD/1M price to a sensible number of digits for display. */
const sell = (ref: number, markup: number) => Math.round(ref * (1 + markup / 100) * 1e6) / 1e6;

/**
 * "Import from catalog": pick models from the public model catalog (fetched by the server),
 * map each to one of the provider's upstream models, and create them with a markup over the
 * reference price.
 */
export function CatalogImportDialog({
  open,
  providers,
  providersLoading,
  providersError,
  onClose,
  onImported,
}: {
  open: boolean;
  providers: Provider[];
  providersLoading: boolean;
  providersError?: Error;
  onClose: () => void;
  onImported: () => void;
}) {
  const t = useT();
  const toast = useToast();
  const [providerId, setProviderId] = useState<number | "">("");
  const [q, setQ] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [overrides, setOverrides] = useState<Record<string, string>>({});
  const [markup, setMarkup] = useState("20");
  const [enable, setEnable] = useState(true);
  const [updateExisting, setUpdateExisting] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<CatalogImportResult | null>(null);

  // Reset when (re)opened; default to the first enabled provider.
  useEffect(() => {
    if (!open) return;
    setQ("");
    setFilter("all");
    setMarkup("20");
    setEnable(true);
    setUpdateExisting(false);
    setResult(null);
    setProviderId((cur) => (cur !== "" && providers.some((p) => p.id === cur) ? cur : ((providers.find((p) => p.enabled) ?? providers[0])?.id ?? "")));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Providers may arrive after the dialog opened.
  useEffect(() => {
    if (open && providerId === "" && providers.length) setProviderId((providers.find((p) => p.enabled) ?? providers[0]).id);
  }, [open, providerId, providers]);

  // Results are tagged with their provider so a stale list (or one kept after an error) is never used.
  const catalog = useAsync(
    () => (open && providerId !== "" ? api.admin.catalog(providerId).then((r) => ({ ...r, pid: providerId })) : Promise.resolve(null)),
    [open, providerId],
  );
  const fresh = catalog.data && catalog.data.pid === providerId && !catalog.error && !catalog.loading ? catalog.data : null;
  const items: CatalogModel[] = useMemo(() => fresh?.data ?? [], [fresh]);

  // New catalog → default selection: everything with a match that doesn't exist yet.
  useEffect(() => {
    setSelected(new Set(items.filter((m) => m.upstream_match && !m.exists).map((m) => m.id)));
    setOverrides({});
  }, [items]);

  const upstreamOf = (m: CatalogModel) => m.upstream_match ?? overrides[m.id]?.trim() ?? "";

  const counts = useMemo(
    () => ({ matched: items.filter((m) => m.upstream_match).length, fresh: items.filter((m) => !m.exists).length }),
    [items],
  );
  const filtered = useMemo(() => {
    const n = q.trim().toLowerCase();
    return items.filter(
      (m) =>
        (filter === "all" || (filter === "matched" ? !!m.upstream_match : !m.exists)) &&
        (!n || m.id.toLowerCase().includes(n) || m.name.toLowerCase().includes(n) || (m.upstream_match ?? "").toLowerCase().includes(n)),
    );
  }, [items, q, filter]);

  const toggle = (id: string, on: boolean) =>
    setSelected((s) => {
      const n = new Set(s);
      if (on) n.add(id);
      else n.delete(id);
      return n;
    });

  const selectMatched = () => setSelected(new Set(items.filter((m) => m.upstream_match && !m.exists).map((m) => m.id)));

  const mk = parseNumber(markup);
  const markupInvalid = mk === null || Number.isNaN(mk) || mk < 0;
  const chosen = items.filter((m) => selected.has(m.id));
  const missingUpstream = chosen.filter((m) => !upstreamOf(m)).length;
  const example = chosen[0] ?? filtered[0];
  const canSubmit = providerId !== "" && !!fresh && chosen.length > 0 && missingUpstream === 0 && !markupInvalid;

  const submit = async () => {
    if (!canSubmit) return;
    setBusy(true);
    try {
      const r = await api.admin.importCatalog({
        provider_id: providerId,
        items: chosen.map((m) => ({ id: m.id, upstream_model: upstreamOf(m) })),
        markup_percent: mk as number,
        enable,
        update_existing: updateExisting,
      });
      setResult(r);
      onImported();
      toast.success(t("catalog.done", { created: r.created, updated: r.updated, endpoints: r.endpoints_added }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  const unavailable = catalog.error instanceof ApiError && catalog.error.code === "catalog_unavailable";
  const provider = providers.find((p) => p.id === providerId);

  let body;
  if (result) {
    body = (
      <div className="grid grid-cols-3 gap-3">
        {(
          [
            ["catalog.created", result.created],
            ["catalog.updated", result.updated],
            ["catalog.endpointsAdded", result.endpoints_added],
          ] as const
        ).map(([label, n]) => (
          <div key={label} className="rounded-lg border border-line p-3">
            <div className="text-xs text-muted">{t(label)}</div>
            <div className="mt-1 text-2xl font-semibold tabular-nums">{formatInt(n)}</div>
          </div>
        ))}
      </div>
    );
  } else if (providersError) {
    body = <ErrorState error={providersError} />;
  } else if (!providersLoading && providers.length === 0) {
    body = (
      <EmptyState
        icon="server"
        title={t("adminModels.noProviders")}
        action={
          <Link to="/admin/providers" onClick={onClose} className="text-sm font-medium text-accent hover:underline">
            {t("nav.adminProviders")} →
          </Link>
        }
      />
    );
  } else {
    body = (
      <div className="flex flex-col gap-3">
        <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]">
          <Select
            label={t("adminModels.provider")}
            value={providerId}
            onChange={(e) => setProviderId(e.target.value ? Number(e.target.value) : "")}
            disabled={busy || providersLoading}
            hint={t("catalog.providerHint")}
          >
            {providerId === "" && <option value="">{t("adminModels.selectProvider")}</option>}
            {providers.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
                {p.enabled ? "" : ` (${t("common.disabled")})`}
              </option>
            ))}
          </Select>
          <Input
            label={t("catalog.markup")}
            type="number"
            min={0}
            step="any"
            inputMode="decimal"
            value={markup}
            onChange={(e) => setMarkup(e.target.value)}
            disabled={busy}
            error={markupInvalid ? t("catalog.markupInvalid") : undefined}
          />
        </div>
        {fresh && example && !markupInvalid && (
          <p className="-mt-1 text-xs text-muted tabular-nums">
            {t("catalog.example", {
              model: example.id,
              inRef: formatPrice(example.input_price),
              inSell: formatPrice(sell(example.input_price, mk as number)),
              outRef: formatPrice(example.output_price),
              outSell: formatPrice(sell(example.output_price, mk as number)),
            })}
          </p>
        )}

        {unavailable ? (
          <Notice tone="danger">
            <div>{t("errors.catalog_unavailable")}</div>
            <Button size="xs" icon="refresh" className="mt-2" onClick={catalog.reload}>
              {t("common.retry")}
            </Button>
          </Notice>
        ) : catalog.error ? (
          <ErrorState error={catalog.error} onRetry={catalog.reload} />
        ) : !fresh ? (
          providerId === "" ? null : <LoadingState label={t("catalog.loading")} />
        ) : (
          <>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <SearchInput value={q} onChange={setQ} placeholder={t("catalog.search")} className="flex-1" />
              <Segmented
                value={filter}
                onChange={setFilter}
                label={t("catalog.filter")}
                options={[
                  { value: "all", label: `${t("common.all")} (${items.length})` },
                  { value: "matched", label: `${t("catalog.filterMatched")} (${counts.matched})` },
                  { value: "new", label: `${t("catalog.filterNew")} (${counts.fresh})` },
                ]}
              />
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted">
              <span>
                {t("import.selectedCount", { n: chosen.length, total: items.length })}
                {fresh.fetched_at && <> · {t("catalog.fetched", { time: formatRelative(fresh.fetched_at) })}</>}
              </span>
              <span className="flex gap-3">
                <button type="button" className="font-medium text-accent hover:underline disabled:opacity-50" onClick={selectMatched} disabled={busy}>
                  {t("catalog.selectMatched")}
                </button>
                <button type="button" className="font-medium text-accent hover:underline disabled:opacity-50" onClick={() => setSelected(new Set())} disabled={busy || selected.size === 0}>
                  {t("common.clear")}
                </button>
              </span>
            </div>
            <div className="max-h-[22rem] overflow-y-auto rounded-lg border border-line">
              {items.length === 0 ? (
                <EmptyState icon="cube" title={t("catalog.empty")} />
              ) : filtered.length === 0 ? (
                <div className="px-3 py-8 text-center text-[13px] text-muted">{t("common.noMatch")}</div>
              ) : (
                <ul className="divide-y divide-line">
                  {filtered.map((m) => (
                    <CatalogRow
                      key={m.id}
                      model={m}
                      checked={selected.has(m.id)}
                      onCheck={(v) => toggle(m.id, v)}
                      upstream={overrides[m.id] ?? ""}
                      onUpstream={(v) => {
                        setOverrides((o) => ({ ...o, [m.id]: v }));
                        if (v.trim()) toggle(m.id, true);
                      }}
                      disabled={busy}
                    />
                  ))}
                </ul>
              )}
            </div>
            {missingUpstream > 0 && <p className="text-xs text-red-600 dark:text-red-400">{t("catalog.missingUpstream", { n: missingUpstream })}</p>}
            <div className="flex flex-col gap-2 rounded-lg border border-line px-3 py-2.5">
              <Checkbox checked={enable} onChange={setEnable} disabled={busy} label={t("catalog.enable")} />
              <Checkbox checked={updateExisting} onChange={setUpdateExisting} disabled={busy} label={t("catalog.updateExisting")} />
              <p className="text-xs text-muted">{t("catalog.costNote")}</p>
            </div>
          </>
        )}
      </div>
    );
  }

  return (
    <Dialog
      open={open}
      onClose={onClose}
      dismissible={!busy}
      size="xl"
      title={t("catalog.title")}
      description={result ? (provider ? t("catalog.doneDesc", { name: provider.name }) : undefined) : t("catalog.desc")}
      footer={
        result ? (
          <Button variant="primary" onClick={onClose}>
            {t("common.done")}
          </Button>
        ) : (
          <>
            <Button onClick={onClose} disabled={busy}>
              {t("common.cancel")}
            </Button>
            {/* No <form>: Enter in the search / upstream inputs must not start an import. */}
            <Button variant="primary" onClick={submit} loading={busy} disabled={!canSubmit}>
              {t("import.submit", { n: chosen.length })}
            </Button>
          </>
        )
      }
    >
      {body}
    </Dialog>
  );
}

function CatalogRow({
  model: m,
  checked,
  onCheck,
  upstream,
  onUpstream,
  disabled,
}: {
  model: CatalogModel;
  checked: boolean;
  onCheck: (v: boolean) => void;
  upstream: string;
  onUpstream: (v: string) => void;
  disabled: boolean;
}) {
  const t = useT();
  return (
    <li className={cx("flex items-start gap-2.5 px-3 py-2", checked ? "bg-accent-soft/40" : "hover:bg-subtle/60")}>
      <input
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 rounded border-line accent-[var(--c-accent)]"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onCheck(e.target.checked)}
        aria-label={m.id}
      />
      <div className="grid min-w-0 flex-1 gap-1.5 sm:grid-cols-[minmax(0,1fr)_10.5rem_minmax(0,14rem)] sm:items-center sm:gap-3">
        <div className="min-w-0">
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="truncate text-[13px] font-medium" title={m.description || m.name}>
              {m.name || m.id}
            </span>
            {m.exists && <Badge>{t("catalog.exists")}</Badge>}
          </div>
          <div className="truncate font-mono text-[11px] text-muted">{m.id}</div>
        </div>
        <div className="text-[11px] text-muted tabular-nums sm:text-right">
          <div>
            {formatPrice(m.input_price)} / {formatPrice(m.output_price)}
          </div>
          {(m.cache_read_price != null || m.cache_write_price != null) && (
            <div className="whitespace-nowrap" title={t("adminModels.cachePricesTitle")}>
              {t("catalog.cachePrices", { read: formatCachePrice(m.cache_read_price), write: formatCachePrice(m.cache_write_price) })}
            </div>
          )}
          <div>
            {t("models.context")} {formatContext(m.context_length)}
          </div>
        </div>
        <div className="min-w-0">
          {m.upstream_match ? (
            <div className="truncate text-[11px] text-emerald-700 dark:text-emerald-400" title={m.upstream_match}>
              {t("catalog.matches")} <span className="font-mono">{m.upstream_match}</span>
            </div>
          ) : (
            <input
              className={cx(controlClass, "h-7 px-2 font-mono text-xs", checked && !upstream.trim() && "border-red-500")}
              placeholder={t("catalog.noMatch")}
              value={upstream}
              onChange={(e) => onUpstream(e.target.value)}
              disabled={disabled}
              spellCheck={false}
              aria-label={t("adminModels.upstreamModel")}
            />
          )}
        </div>
      </div>
    </li>
  );
}
