import type { ReactNode, TdHTMLAttributes, ThHTMLAttributes } from "react";
import { cx } from "../lib/cx";
import { useT } from "../i18n";
import { Spinner } from "./Spinner";
import { Button } from "./Button";

// Pins the last column (row actions) to the right edge while the table scrolls.
const stickyLastColumn = cx(
  "[&_tr>*:last-child:not([colspan])]:sticky [&_tr>*:last-child:not([colspan])]:right-0",
  "[&_tr>*:last-child:not([colspan])]:shadow-[inset_1px_0_0_var(--c-line)]",
  "[&_thead_th:last-child]:bg-subtle [&_tbody_td:last-child:not([colspan])]:bg-surface",
);

/**
 * Table that scrolls horizontally inside its container — never the whole page.
 * With stickyLast, the last column (row actions) stays visible while scrolling.
 */
export function Table({
  children,
  className,
  minWidth,
  stickyLast,
}: {
  children: ReactNode;
  className?: string;
  minWidth?: number;
  stickyLast?: boolean;
}) {
  return (
    <div className={cx("w-full overflow-x-auto", stickyLast && stickyLastColumn, className)}>
      <table className="w-full border-collapse text-left text-[13px]" style={minWidth ? { minWidth } : undefined}>
        {children}
      </table>
    </div>
  );
}

export function THead({ children }: { children: ReactNode }) {
  return (
    <thead className="border-b border-line bg-subtle/60 text-xs text-muted">
      <tr>{children}</tr>
    </thead>
  );
}

export function Th({ className, align, ...rest }: ThHTMLAttributes<HTMLTableCellElement> & { align?: "left" | "right" | "center" }) {
  return (
    <th
      className={cx(
        "px-3 py-2 font-medium whitespace-nowrap first:pl-4 last:pr-4",
        align === "right" && "text-right",
        align === "center" && "text-center",
        className,
      )}
      {...rest}
    />
  );
}

export function TBody({ children }: { children: ReactNode }) {
  return <tbody className="divide-y divide-line">{children}</tbody>;
}

export function Tr({ className, children, onClick }: { className?: string; children: ReactNode; onClick?: () => void }) {
  return (
    <tr className={cx("transition-colors hover:bg-subtle/50", onClick && "cursor-pointer", className)} onClick={onClick}>
      {children}
    </tr>
  );
}

export function Td({ className, align, ...rest }: TdHTMLAttributes<HTMLTableCellElement> & { align?: "left" | "right" | "center" }) {
  return (
    <td
      className={cx(
        "px-3 py-2.5 align-middle first:pl-4 last:pr-4",
        align === "right" && "text-right tabular-nums",
        align === "center" && "text-center",
        className,
      )}
      {...rest}
    />
  );
}

/**
 * Renders a single full-width row for loading / error / empty states.
 * Returns null when there is data to show.
 */
export function TableState({
  colSpan,
  loading,
  error,
  empty,
  onRetry,
  emptyText,
}: {
  colSpan: number;
  loading: boolean;
  error?: Error;
  empty: boolean;
  onRetry?: () => void;
  emptyText?: ReactNode;
}) {
  const t = useT();
  let content: ReactNode = null;
  if (error) {
    content = (
      <div className="flex flex-col items-center gap-2">
        <span className="text-red-600 dark:text-red-400">{error.message}</span>
        {onRetry && (
          <Button size="sm" icon="refresh" onClick={onRetry}>
            {t("common.retry")}
          </Button>
        )}
      </div>
    );
  } else if (loading && empty) {
    content = (
      <span className="inline-flex items-center gap-2">
        <Spinner /> {t("common.loading")}
      </span>
    );
  } else if (empty) {
    content = emptyText ?? t("common.noData");
  }
  if (!content) return null;
  return (
    <tr>
      <td colSpan={colSpan} className="px-4 py-12 text-center text-sm text-muted">
        {content}
      </td>
    </tr>
  );
}
