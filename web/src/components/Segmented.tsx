import type { ReactNode } from "react";
import { cx } from "../lib/cx";

export interface SegmentedOption<T> {
  value: T;
  label: ReactNode;
}

/** Small pill-style switch, e.g. "7d / 30d". */
export function Segmented<T extends string | number>({
  value,
  options,
  onChange,
  label,
}: {
  value: T;
  options: SegmentedOption<T>[];
  onChange: (v: T) => void;
  label?: string;
}) {
  return (
    <div role="radiogroup" aria-label={label} className="inline-flex shrink-0 items-center gap-0.5 rounded-lg border border-line bg-subtle p-0.5">
      {options.map((o) => (
        <button
          key={String(o.value)}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          onClick={() => onChange(o.value)}
          className={cx(
            "h-7 rounded-md px-2.5 text-xs font-medium whitespace-nowrap transition-colors",
            o.value === value ? "bg-surface text-fg shadow-xs" : "text-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
