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

export interface DualBarDatum {
  label: string; // YYYY-MM-DD (start of the bucket)
  end?: string; // YYYY-MM-DD, last day when a bar covers several days
  a: number;
  b: number;
}

/**
 * Two series per slot drawn as a "bullet" bar: series A as a wide light bar, series B as a narrow
 * solid bar in front of it (e.g. revenue vs. cost — the visible gap is the profit). Works at any
 * density, from 7 to ~100 slots.
 */
export function DualBarChart({
  data,
  format,
  labels,
  height = 200,
  emptyText,
}: {
  data: DualBarDatum[];
  format: (v: number) => string;
  labels: [string, string];
  height?: number;
  emptyText?: string;
}) {
  const t = useT();
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(0, ...data.map((d) => Math.max(d.a, d.b)));
  const nice = niceMax(max);
  const labelEvery = Math.max(1, Math.ceil(data.length / 8));
  const gap = data.length > 60 ? 1 : data.length > 30 ? 2 : 3;
  const active = hover !== null ? data[hover] : null;
  const day = (d: DualBarDatum) => (d.end && d.end !== d.label ? `${formatDay(d.label)} – ${formatDay(d.end)}` : formatDay(d.label));
  const pct = (v: number) => (nice > 0 && v > 0 ? `max(${(v / nice) * 100}%, 2px)` : 0);

  return (
    <div className="relative select-none">
      <div className="mb-2 flex min-h-5 flex-wrap items-center justify-between gap-x-3 text-xs text-muted">
        <span className="flex items-center gap-3">
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm bg-accent/35" />
            {labels[0]}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm bg-amber-500" />
            {labels[1]}
          </span>
        </span>
        <span className="tabular-nums">
          {active
            ? `${day(active)} · ${labels[0]} ${format(active.a)} · ${labels[1]} ${format(active.b)}`
            : max > 0
              ? t("chart.max", { value: format(max) })
              : ""}
        </span>
      </div>
      <div className="relative" style={{ height }}>
        {[0, 0.5, 1].map((f) => (
          <div key={f} className="absolute inset-x-0 border-t border-dashed border-line" style={{ bottom: `${f * 100}%` }} />
        ))}
        {max === 0 && emptyText && <div className="absolute inset-0 flex items-center justify-center text-xs text-muted">{emptyText}</div>}
        <div className="absolute inset-0 flex items-end" style={{ gap }} onMouseLeave={() => setHover(null)}>
          {data.map((d, i) => (
            <div
              key={d.label}
              className="relative flex h-full min-w-0 flex-1 items-end justify-center"
              onMouseEnter={() => setHover(i)}
              onTouchStart={() => setHover(i)}
              title={`${day(d)}: ${labels[0]} ${format(d.a)}, ${labels[1]} ${format(d.b)}`}
            >
              <div
                className={cx("absolute bottom-0 w-full rounded-t-[3px] transition-[height,background-color] duration-300", hover === i ? "bg-accent/50" : "bg-accent/30")}
                style={{ height: pct(d.a) }}
              />
              <div
                className={cx("relative w-1/2 rounded-t-[2px] transition-[height,background-color] duration-300", hover === i ? "bg-amber-600" : "bg-amber-500")}
                style={{ height: pct(d.b) }}
              />
            </div>
          ))}
        </div>
      </div>
      <div className="mt-1.5 flex text-[10px] text-muted" style={{ gap }}>
        {data.map((d, i) => (
          <div key={d.label} className="min-w-0 flex-1 overflow-visible text-center whitespace-nowrap">
            {i % labelEvery === 0 ? formatDay(d.label) : ""}
          </div>
        ))}
      </div>
    </div>
  );
}
