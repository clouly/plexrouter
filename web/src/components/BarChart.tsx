import { useState } from "react";
import { cx } from "../lib/cx";
import { formatDay } from "../lib/format";
import { useT } from "../i18n";

export interface BarDatum {
  label: string; // YYYY-MM-DD
  value: number;
}

/**
 * Minimal dependency-free bar chart drawn with divs.
 * Hovering a bar shows its value; x labels are thinned out to avoid overlap.
 */
export function BarChart({
  data,
  format,
  height = 180,
  emptyText,
}: {
  data: BarDatum[];
  format: (v: number) => string;
  height?: number;
  emptyText?: string;
}) {
  const t = useT();
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(0, ...data.map((d) => d.value));
  const nice = niceMax(max);
  const labelEvery = data.length > 20 ? Math.ceil(data.length / 8) : data.length > 10 ? 2 : 1;
  const active = hover !== null ? data[hover] : null;

  return (
    <div className="relative select-none">
      <div className="mb-2 flex h-5 items-center justify-between text-xs text-muted">
        <span className="tabular-nums">{active ? `${formatDay(active.label)} · ${format(active.value)}` : ""}</span>
        <span className="tabular-nums">{max > 0 ? t("chart.max", { value: format(max) }) : ""}</span>
      </div>
      <div className="relative" style={{ height }}>
        {/* grid lines */}
        {[0, 0.5, 1].map((f) => (
          <div key={f} className="absolute inset-x-0 border-t border-dashed border-line" style={{ bottom: `${f * 100}%` }} />
        ))}
        {max === 0 && emptyText && (
          <div className="absolute inset-0 flex items-center justify-center text-xs text-muted">{emptyText}</div>
        )}
        <div className="absolute inset-0 flex items-end gap-[3px]" onMouseLeave={() => setHover(null)}>
          {data.map((d, i) => {
            const pct = nice > 0 ? (d.value / nice) * 100 : 0;
            return (
              <div
                key={d.label}
                className="flex h-full min-w-0 flex-1 items-end"
                onMouseEnter={() => setHover(i)}
                onTouchStart={() => setHover(i)}
                title={`${d.label}: ${format(d.value)}`}
              >
                <div
                  className={cx(
                    "w-full rounded-t-[3px] transition-[height,background-color] duration-300",
                    hover === i ? "bg-accent-hover" : "bg-accent/80",
                  )}
                  style={{ height: d.value > 0 ? `max(${pct}%, 2px)` : 0 }}
                />
              </div>
            );
          })}
        </div>
      </div>
      <div className="mt-1.5 flex gap-[3px] text-[10px] text-muted">
        {data.map((d, i) => (
          <div key={d.label} className="min-w-0 flex-1 overflow-visible text-center whitespace-nowrap">
            {i % labelEvery === 0 ? formatDay(d.label) : ""}
          </div>
        ))}
      </div>
    </div>
  );
}

function niceMax(v: number): number {
  if (v <= 0) return 0;
  const exp = Math.pow(10, Math.floor(Math.log10(v)));
  const n = v / exp;
  const step = n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10;
  return step * exp;
}
