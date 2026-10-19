import { useMemo, useState } from "react";
import { api } from "../api";
import { useAuth } from "../auth";
import { useT, type MessageKey } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { formatContext, formatPrice } from "../lib/format";
import type { PublicModel } from "../types";
import { Card, PageHeader } from "../components/Card";
import { CopyButton } from "../components/CopyButton";
import { EmptyState, ErrorState, LoadingState } from "../components/EmptyState";
import { Checkbox, SearchInput, Select } from "../components/Input";

type SortKey = "name" | "input_asc" | "input_desc" | "output_asc" | "output_desc" | "context_desc";

const sorts: { key: SortKey; label: MessageKey }[] = [
  { key: "name", label: "models.sortName" },
  { key: "input_asc", label: "models.sortInputAsc" },
  { key: "input_desc", label: "models.sortInputDesc" },
  { key: "output_asc", label: "models.sortOutputAsc" },
  { key: "output_desc", label: "models.sortOutputDesc" },
  { key: "context_desc", label: "models.sortContextDesc" },
];

function compare(a: PublicModel, b: PublicModel, key: SortKey): number {
  switch (key) {
    case "input_asc":
      return a.input_price - b.input_price;
    case "input_desc":
      return b.input_price - a.input_price;
    case "output_asc":
      return a.output_price - b.output_price;
    case "output_desc":
      return b.output_price - a.output_price;
    case "context_desc":
      return b.context_length - a.context_length;
    default:
      return 0;
  }
}

export function ModelsPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("nav.models"), config?.name);
  const models = useAsync(() => api.models(), []);
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<SortKey>("name");
  const [cacheOnly, setCacheOnly] = useState(false);

  const list = useMemo(() => {
    const needle = q.trim().toLowerCase();
    const all = models.data?.data ?? [];
    const filtered = all.filter(
      (m) =>
        (!cacheOnly || m.cache_read_price != null) &&
        (!needle || m.id.toLowerCase().includes(needle) || m.name.toLowerCase().includes(needle) || m.description.toLowerCase().includes(needle)),
    );
    return [...filtered].sort((a, b) => compare(a, b, sort) || (a.name || a.id).localeCompare(b.name || b.id));
  }, [models.data, q, sort, cacheOnly]);

  const total = models.data?.data.length ?? 0;

  return (
    <>
      <PageHeader title={t("models.title")} description={t("models.desc")} />
      <div className="mb-3 flex flex-col gap-2 sm:flex-row sm:items-center">
        <SearchInput value={q} onChange={setQ} placeholder={t("models.search")} className="flex-1" />
        <Select value={sort} onChange={(e) => setSort(e.target.value as SortKey)} aria-label={t("models.sortBy")} wrapperClassName="sm:w-56">
          {sorts.map((s) => (
            <option key={s.key} value={s.key}>
              {t(s.label)}
            </option>
          ))}
        </Select>
      </div>
      <div className="mb-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
        <Checkbox checked={cacheOnly} onChange={setCacheOnly} label={t("models.filterCaching")} />
        {models.data && (
          <span className="text-xs text-muted">
            {q.trim() || cacheOnly ? t("models.countFiltered", { n: list.length, total }) : t("models.count", { n: total })}
          </span>
        )}
      </div>
      <Card flush>
        {models.error ? (
          <ErrorState error={models.error} onRetry={models.reload} />
        ) : models.loading && !models.data ? (
          <LoadingState />
        ) : list.length === 0 ? (
          <EmptyState icon="cube" title={total === 0 ? t("models.emptyTitle") : t("models.noMatch")} description={total === 0 ? t("models.emptyDesc") : undefined} />
        ) : (
          <ul className="divide-y divide-line">
            {list.map((m) => (
              <ModelRow key={m.id} m={m} />
            ))}
          </ul>
        )}
      </Card>
    </>
  );
}

function ModelRow({ m }: { m: PublicModel }) {
  const t = useT();
  return (
    <li className="flex flex-col gap-3 px-4 py-3.5 md:flex-row md:items-start md:gap-6">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <h3 className="font-semibold">{m.name || m.id}</h3>
          <span className="inline-flex min-w-0 items-center gap-0.5">
            <code className="truncate font-mono text-xs text-muted">{m.id}</code>
            <CopyButton text={m.id} label={t("models.copySlug")} />
          </span>
        </div>
        {m.description && <p className="mt-1 line-clamp-2 text-[13px] text-muted">{m.description}</p>}
      </div>
      <dl className="grid shrink-0 grid-cols-3 gap-x-5 gap-y-1 text-xs md:w-[22rem]">
        <div>
          <dt className="text-muted">{t("models.context")}</dt>
          <dd className="mt-0.5 font-medium tabular-nums">{formatContext(m.context_length)}</dd>
        </div>
        <div>
          <dt className="text-muted">{t("models.input")}</dt>
          <dd className="mt-0.5 font-medium tabular-nums">{formatPrice(m.input_price)}</dd>
        </div>
        <div>
          <dt className="text-muted">{t("models.output")}</dt>
          <dd className="mt-0.5 font-medium tabular-nums">{formatPrice(m.output_price)}</dd>
        </div>
        {(m.cache_read_price != null || m.cache_write_price != null) && (
          <div className="col-span-3 flex flex-wrap gap-x-3 text-[11px] text-muted tabular-nums" title={t("models.cacheHint")}>
            <span className="font-medium text-fg">{t("models.cache")}</span>
            {m.cache_read_price != null && <span>{t("models.cacheRead", { price: formatPrice(m.cache_read_price) })}</span>}
            {m.cache_write_price != null && <span>{t("models.cacheWrite", { price: formatPrice(m.cache_write_price) })}</span>}
          </div>
        )}
        <div className="col-span-3 text-[11px] text-muted">
          {t("models.perMillion")}
          {m.providers > 1 && <> · {t("models.providers", { n: m.providers })}</>}
        </div>
      </dl>
    </li>
  );
}
