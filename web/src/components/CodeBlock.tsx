import { useState, type ReactNode } from "react";
import { cx } from "../lib/cx";
import { CopyButton } from "./CopyButton";

export function CodeBlock({ code, lang, className }: { code: string; lang?: string; className?: string }) {
  return (
    <div className={cx("group relative min-w-0 overflow-hidden rounded-lg border border-line bg-code", className)}>
      {lang && <div className="border-b border-line px-3 py-1.5 font-mono text-[11px] text-muted">{lang}</div>}
      <CopyButton text={code} className="absolute top-1 right-1.5 bg-code" />
      <pre className="overflow-x-auto p-3 pr-10 font-mono text-[12.5px] leading-relaxed">
        <code>{code}</code>
      </pre>
    </div>
  );
}

export interface CodeTab {
  label: string;
  code: string;
}

export function CodeTabs({ tabs, className }: { tabs: CodeTab[]; className?: string }) {
  const [active, setActive] = useState(0);
  const tab = tabs[Math.min(active, tabs.length - 1)];
  return (
    <div className={cx("relative min-w-0 overflow-hidden rounded-lg border border-line bg-code", className)}>
      <div className="flex items-center gap-1 overflow-x-auto border-b border-line px-1.5 pt-1.5" role="tablist">
        {tabs.map((tb, i) => (
          <button
            key={tb.label}
            type="button"
            role="tab"
            aria-selected={i === active}
            onClick={() => setActive(i)}
            className={cx(
              "-mb-px border-b-2 px-2.5 pb-1.5 font-mono text-xs whitespace-nowrap transition-colors",
              i === active ? "border-accent text-fg" : "border-transparent text-muted hover:text-fg",
            )}
          >
            {tb.label}
          </button>
        ))}
      </div>
      <CopyButton text={tab.code} className="absolute top-10 right-1.5 bg-code" />
      <pre className="overflow-x-auto p-3 pr-10 font-mono text-[12.5px] leading-relaxed">
        <code>{tab.code}</code>
      </pre>
    </div>
  );
}

export function InlineCode({ children }: { children: ReactNode }) {
  return <code className="rounded bg-subtle px-1 py-0.5 font-mono text-[12px]">{children}</code>;
}
