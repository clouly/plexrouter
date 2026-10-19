import type { ReactNode } from "react";
import { cx } from "../lib/cx";

export function Card({
  title,
  description,
  actions,
  children,
  className,
  bodyClassName,
  flush,
}: {
  title?: ReactNode;
  description?: ReactNode;
  actions?: ReactNode;
  children?: ReactNode;
  className?: string;
  bodyClassName?: string;
  /** No body padding — for tables that should touch the card edges. */
  flush?: boolean;
}) {
  return (
    <section className={cx("min-w-0 rounded-xl border border-line bg-surface shadow-xs", className)}>
      {(title || actions) && (
        <header className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b border-line px-4 py-3">
          <div className="min-w-0">
            {title && <h2 className="text-sm font-semibold">{title}</h2>}
            {description && <p className="mt-0.5 text-xs text-muted">{description}</p>}
          </div>
          {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={cx(flush ? "" : "p-4", bodyClassName)}>{children}</div>
    </section>
  );
}

export function StatCard({ label, value, hint, loading }: { label: ReactNode; value: ReactNode; hint?: ReactNode; loading?: boolean }) {
  return (
    <div className="min-w-0 rounded-xl border border-line bg-surface p-4 shadow-xs">
      <div className="text-xs font-medium text-muted">{label}</div>
      <div className="mt-1.5 truncate text-2xl font-semibold tracking-tight tabular-nums">
        {loading ? <span className="inline-block h-7 w-24 animate-pulse rounded bg-subtle align-middle" /> : value}
      </div>
      {hint && <div className="mt-1 truncate text-xs text-muted">{hint}</div>}
    </div>
  );
}

export function PageHeader({ title, description, actions }: { title: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="mt-1 text-sm text-muted">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  );
}

export function Notice({ tone = "info", children, className }: { tone?: "info" | "warning" | "danger"; children: ReactNode; className?: string }) {
  const tones = {
    info: "border-accent/25 bg-accent-soft text-fg",
    warning: "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200",
    danger: "border-red-300 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200",
  };
  return <div className={cx("rounded-lg border px-3 py-2.5 text-[13px] leading-relaxed", tones[tone], className)}>{children}</div>;
}
