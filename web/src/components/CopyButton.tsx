import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { copyText } from "../lib/clipboard";
import { cx } from "../lib/cx";
import { useT } from "../i18n";
import { Icon } from "./Icon";
import { useToast } from "./Toast";

export function CopyButton({ text, label, className, size = "icon" }: { text: string; label?: string; className?: string; size?: "icon" | "sm" }) {
  const t = useT();
  const toast = useToast();
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const id = window.setTimeout(() => setCopied(false), 1500);
    return () => window.clearTimeout(id);
  }, [copied]);

  const onClick = async (e: MouseEvent) => {
    e.stopPropagation();
    if (await copyText(text)) setCopied(true);
    else toast.error(t("common.copyFailed"));
  };

  const title = copied ? t("common.copied") : (label ?? t("common.copy"));

  if (size === "sm") {
    return (
      <button
        type="button"
        onClick={onClick}
        className={cx(
          "inline-flex h-7 items-center gap-1.5 rounded-md border border-line bg-surface px-2 text-xs font-medium text-fg shadow-xs hover:bg-subtle",
          className,
        )}
      >
        <Icon name={copied ? "check" : "copy"} className={cx("size-3.5", copied && "text-emerald-600 dark:text-emerald-400")} />
        {title}
      </button>
    );
  }
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      aria-label={title}
      className={cx("inline-flex size-6 items-center justify-center rounded text-muted hover:bg-subtle hover:text-fg", className)}
    >
      <Icon name={copied ? "check" : "copy"} className={cx("size-3.5", copied && "text-emerald-600 dark:text-emerald-400")} />
    </button>
  );
}

/** A labelled value with a copy button, for the fields to paste into the provider's form. */
export function CopyRow({ label, value }: { label: ReactNode; value: string }) {
  return (
    <div className="rounded-lg border border-line bg-subtle/60 px-3 py-2">
      <div className="text-[11px] font-medium text-muted">{label}</div>
      <div className="mt-0.5 flex items-center gap-2">
        <code className="min-w-0 flex-1 font-mono text-[13px] break-all">{value || "—"}</code>
        {value && <CopyButton text={value} className="shrink-0" />}
      </div>
    </div>
  );
}
