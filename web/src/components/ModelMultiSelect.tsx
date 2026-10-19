import { useId, useMemo, useState } from "react";
import { useT } from "../i18n";
import { cx } from "../lib/cx";
import { errorMessage } from "../api";
import type { PublicModel } from "../types";
import { controlClass, Field } from "./Input";
import { Icon } from "./Icon";
import { Spinner } from "./Spinner";

/**
 * Inline multi-select over model slugs: chips for the selection, a search box and a scrollable
 * checkbox list (no popover, so it works inside dialogs and on phones). Empty selection = all.
 */
export function ModelMultiSelect({
  label,
  hint,
  models,
  loading,
  error,
  value,
  onChange,
  disabled,
}: {
  label: string;
  hint?: string;
  models: PublicModel[];
  loading?: boolean;
  error?: Error;
  value: string[];
  onChange: (v: string[]) => void;
  disabled?: boolean;
}) {
  const t = useT();
  const id = useId();
  const [q, setQ] = useState("");
  const selected = useMemo(() => new Set(value), [value]);
  const filtered = useMemo(() => {
    const n = q.trim().toLowerCase();
    const list = [...models].sort((a, b) => a.id.localeCompare(b.id));
    return n ? list.filter((m) => m.id.toLowerCase().includes(n) || m.name.toLowerCase().includes(n)) : list;
  }, [models, q]);

  const toggle = (slug: string) => onChange(selected.has(slug) ? value.filter((v) => v !== slug) : [...value, slug]);

  return (
    <Field label={label} hint={hint} htmlFor={id}>
      <div className="flex min-h-7 flex-wrap items-center gap-1">
        {value.length === 0 ? (
          <span className="rounded-md border border-dashed border-line px-2 py-0.5 text-xs text-muted">{t("keys.allModels")}</span>
        ) : (
          <>
            {value.map((slug) => (
              <span key={slug} className="inline-flex max-w-full items-center gap-1 rounded-md border border-accent/20 bg-accent-soft py-0.5 pr-0.5 pl-1.5 font-mono text-[11px] text-accent">
                <span className="truncate">{slug}</span>
                <button
                  type="button"
                  className="rounded p-0.5 hover:bg-accent/15 disabled:opacity-50"
                  onClick={() => toggle(slug)}
                  disabled={disabled}
                  aria-label={`${t("common.remove")} ${slug}`}
                >
                  <Icon name="x" className="size-3" />
                </button>
              </span>
            ))}
            <button type="button" className="ml-1 text-xs font-medium text-accent hover:underline disabled:opacity-50" onClick={() => onChange([])} disabled={disabled}>
              {t("common.clear")}
            </button>
          </>
        )}
      </div>
      <div className="overflow-hidden rounded-lg border border-line">
        <div className="relative border-b border-line">
          <Icon name="search" className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted" />
          <input
            id={id}
            type="search"
            className={cx(controlClass, "h-8 rounded-none border-0 pl-8 text-[13px] shadow-none focus:ring-0")}
            placeholder={t("models.search")}
            value={q}
            onChange={(e) => setQ(e.target.value)}
            // Enter would submit the surrounding dialog form.
            onKeyDown={(e) => e.key === "Enter" && e.preventDefault()}
            disabled={disabled}
          />
        </div>
        <div className="max-h-44 overflow-y-auto">
          {error ? (
            <div className="px-3 py-4 text-center text-xs text-red-600 dark:text-red-400">{errorMessage(error)}</div>
          ) : loading && models.length === 0 ? (
            <div className="flex items-center justify-center gap-2 px-3 py-4 text-xs text-muted">
              <Spinner /> {t("common.loading")}
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-3 py-4 text-center text-xs text-muted">{models.length ? t("common.noMatch") : t("models.emptyTitle")}</div>
          ) : (
            <ul className="divide-y divide-line">
              {filtered.map((m) => (
                <li key={m.id}>
                  <label className={cx("flex items-center gap-2 px-3 py-1.5", disabled ? "opacity-60" : "cursor-pointer hover:bg-subtle/60")}>
                    <input
                      type="checkbox"
                      className="size-3.5 shrink-0 rounded border-line accent-[var(--c-accent)]"
                      checked={selected.has(m.id)}
                      onChange={() => toggle(m.id)}
                      disabled={disabled}
                    />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{m.id}</span>
                    <span className="hidden max-w-[40%] truncate text-[11px] text-muted sm:inline">{m.name}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </Field>
  );
}
