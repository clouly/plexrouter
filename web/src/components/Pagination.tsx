import { useT } from "../i18n";
import { formatInt } from "../lib/format";
import { IconButton } from "./Button";

export function Pagination({
  page,
  pageSize,
  total,
  onChange,
  onPageSizeChange,
}: {
  page: number;
  pageSize: number;
  total: number;
  onChange: (page: number) => void;
  onPageSizeChange?: (size: number) => void;
}) {
  const t = useT();
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(total, page * pageSize);
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-line px-4 py-2.5 text-xs text-muted">
      <div>{t("pagination.range", { from: formatInt(from), to: formatInt(to), total: formatInt(total) })}</div>
      <div className="flex items-center gap-2">
        {onPageSizeChange && (
          <select
            className="h-7 rounded-md border border-line bg-surface px-1.5 text-xs text-fg"
            value={pageSize}
            onChange={(e) => onPageSizeChange(Number(e.target.value))}
            aria-label={t("pagination.pageSize")}
          >
            {[10, 20, 50, 100].map((n) => (
              <option key={n} value={n}>
                {t("pagination.perPage", { n })}
              </option>
            ))}
          </select>
        )}
        <IconButton icon="chevronLeft" label={t("pagination.prev")} disabled={page <= 1} onClick={() => onChange(page - 1)} />
        <span className="tabular-nums text-fg">
          {page} / {pages}
        </span>
        <IconButton icon="chevronRight" label={t("pagination.next")} disabled={page >= pages} onClick={() => onChange(page + 1)} />
      </div>
    </div>
  );
}
