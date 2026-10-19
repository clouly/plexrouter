import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { LOCALES, localeName, useI18n, type Lang } from "../i18n";
import { cx } from "../lib/cx";
import { Icon } from "./Icon";
import { Spinner } from "./Spinner";

/** Header language picker: globe + current native name (icon only on small screens). */
export function LanguageMenu() {
  const { lang, pendingLang, setLang, t } = useI18n();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([]);
  const menuId = useId();
  const selected = pendingLang ?? lang;

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent | TouchEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("touchstart", onDown);
    // Focus the current language when the menu opens.
    const idx = Math.max(0, LOCALES.findIndex((l) => l.code === selected));
    const id = window.setTimeout(() => itemRefs.current[idx]?.focus(), 0);
    return () => {
      window.clearTimeout(id);
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("touchstart", onDown);
    };
    // Only on open/close; `selected` changing while open shouldn't move focus.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const close = (focusButton: boolean) => {
    setOpen(false);
    if (focusButton) buttonRef.current?.focus();
  };

  const choose = (l: Lang) => {
    setLang(l);
    close(true);
  };

  const onMenuKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const items = itemRefs.current.filter((x): x is HTMLButtonElement => !!x);
    const cur = items.indexOf(document.activeElement as HTMLButtonElement);
    const focusAt = (i: number) => items[(i + items.length) % items.length]?.focus();
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        focusAt(cur + 1);
        break;
      case "ArrowUp":
        e.preventDefault();
        focusAt(cur - 1);
        break;
      case "Home":
        e.preventDefault();
        focusAt(0);
        break;
      case "End":
        e.preventDefault();
        focusAt(items.length - 1);
        break;
      case "Escape":
        e.preventDefault();
        e.stopPropagation();
        close(true);
        break;
      case "Tab":
        close(false);
        break;
    }
  };

  const onButtonKey = (e: KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setOpen(true);
    } else if (e.key === "Escape" && open) {
      e.preventDefault();
      close(true);
    }
  };

  return (
    <div className="relative" ref={rootRef}>
      <button
        ref={buttonRef}
        type="button"
        onClick={() => setOpen((o) => !o)}
        onKeyDown={onButtonKey}
        title={t("header.language")}
        aria-label={`${t("header.language")}: ${localeName(lang)}`}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        className="inline-flex h-8 items-center gap-1.5 rounded-md px-2 text-xs font-medium text-muted hover:bg-subtle hover:text-fg focus-visible:ring-2 focus-visible:ring-accent/30 focus-visible:outline-none"
      >
        {pendingLang ? <Spinner className="size-4" /> : <Icon name="globe" />}
        <span className="hidden max-w-[9rem] truncate sm:inline">{localeName(lang)}</span>
        <Icon name="chevronDown" className="hidden size-3 sm:block" />
      </button>
      {open && (
        <div
          id={menuId}
          role="menu"
          aria-label={t("header.language")}
          onKeyDown={onMenuKey}
          className="animate-pop absolute right-0 z-40 mt-1.5 max-h-[min(24rem,calc(100dvh-5rem))] w-52 overflow-y-auto rounded-lg border border-line bg-surface py-1 shadow-lg"
        >
          {LOCALES.map((l, i) => {
            const checked = l.code === selected;
            return (
              <button
                key={l.code}
                ref={(el) => {
                  itemRefs.current[i] = el;
                }}
                type="button"
                role="menuitemradio"
                aria-checked={checked}
                lang={l.code}
                tabIndex={-1}
                onClick={() => choose(l.code)}
                className={cx(
                  "flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-[13px] hover:bg-subtle focus:bg-subtle focus:outline-none",
                  checked && "font-medium text-accent",
                )}
              >
                <span className="truncate">{l.name}</span>
                {checked && <Icon name="check" className="size-3.5" />}
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}
