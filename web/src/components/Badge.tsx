import type { ReactNode } from "react";
import { cx } from "../lib/cx";

export type BadgeTone = "neutral" | "accent" | "success" | "warning" | "danger";

const tones: Record<BadgeTone, string> = {
  neutral: "bg-subtle text-muted border-line",
  accent: "bg-accent-soft text-accent border-accent/20",
  success: "bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-400 dark:border-emerald-900",
  warning: "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-950/40 dark:text-amber-400 dark:border-amber-900",
  danger: "bg-red-50 text-red-700 border-red-200 dark:bg-red-950/40 dark:text-red-400 dark:border-red-900",
};

export function Badge({ tone = "neutral", children, className, title, dot }: { tone?: BadgeTone; children: ReactNode; className?: string; title?: string; dot?: boolean }) {
  return (
    <span
      title={title}
      className={cx("inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px] leading-4 font-medium whitespace-nowrap", tones[tone], className)}
    >
      {dot && <span className="size-1.5 rounded-full bg-current" />}
      {children}
    </span>
  );
}
