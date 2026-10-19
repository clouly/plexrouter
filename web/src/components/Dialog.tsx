import { createContext, useCallback, useContext, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cx } from "../lib/cx";
import { useT } from "../i18n";
import { Button } from "./Button";
import { IconButton } from "./Button";

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: "sm" | "md" | "lg" | "xl";
  /** When set, the body is wrapped in a <form> and Enter submits. */
  onSubmit?: () => void;
  /** Prevent closing by backdrop/Escape (e.g. while submitting or for one-time secrets). */
  dismissible?: boolean;
}

const widths = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-2xl", xl: "max-w-4xl" };

export function Dialog({ open, onClose, title, description, children, footer, size = "md", onSubmit, dismissible = true }: DialogProps) {
  const t = useT();
  const panelRef = useRef<HTMLDivElement>(null);
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  // Read through a ref so toggling `dismissible` (e.g. while submitting) doesn't re-run the
  // open effect, which would steal focus and re-lock scrolling.
  const dismissibleRef = useRef(dismissible);
  dismissibleRef.current = dismissible;

  useEffect(() => {
    if (!open) return;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const prevFocus = document.activeElement as HTMLElement | null;
    // Focus the first input, or the panel itself.
    const id = window.setTimeout(() => {
      const el = panelRef.current?.querySelector<HTMLElement>("input:not([type=hidden]):not([disabled]), textarea, select");
      (el ?? panelRef.current)?.focus();
    }, 0);
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || !dismissibleRef.current) return;
      // Only the topmost dialog reacts (e.g. a confirm opened over an edit dialog).
      const dialogs = document.querySelectorAll('[role="dialog"]');
      if (dialogs[dialogs.length - 1] !== panelRef.current) return;
      closeRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      window.clearTimeout(id);
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      prevFocus?.focus?.();
    };
  }, [open]);

  if (!open) return null;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit?.();
  };

  const inner = (
    <>
      <div className="flex items-start justify-between gap-4 border-b border-line px-5 py-4">
        <div className="min-w-0">
          <h2 className="text-base font-semibold">{title}</h2>
          {description && <p className="mt-1 text-[13px] text-muted">{description}</p>}
        </div>
        {dismissible && <IconButton icon="x" label={t("common.close")} onClick={onClose} className="-mt-1 -mr-2" />}
      </div>
      <div className="max-h-[calc(100dvh-12rem)] overflow-y-auto px-5 py-4">{children}</div>
      {footer && <div className="flex flex-wrap justify-end gap-2 border-t border-line bg-subtle/50 px-5 py-3">{footer}</div>}
    </>
  );

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center p-0 sm:items-center sm:p-4" role="presentation">
      <div className="animate-fade absolute inset-0 bg-black/50 backdrop-blur-[1px]" onClick={dismissible ? onClose : undefined} />
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
        className={cx(
          "animate-pop relative w-full overflow-hidden rounded-t-2xl border border-line bg-surface shadow-2xl outline-none sm:rounded-xl",
          widths[size],
        )}
      >
        {onSubmit ? (
          <form onSubmit={handleSubmit} noValidate>
            {inner}
          </form>
        ) : (
          inner
        )}
      </div>
    </div>,
    document.body,
  );
}

// ---------------------------------------------------------------------------
// Confirm dialogs: `const confirm = useConfirm(); if (await confirm({...})) ...`

interface ConfirmOptions {
  title: ReactNode;
  message?: ReactNode;
  confirmLabel?: string;
  danger?: boolean;
}

type ConfirmFn = (opts: ConfirmOptions) => Promise<boolean>;
const ConfirmContext = createContext<ConfirmFn | null>(null);

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const t = useT();
  const [state, setState] = useState<(ConfirmOptions & { resolve: (v: boolean) => void }) | null>(null);

  const confirm = useCallback<ConfirmFn>((opts) => new Promise<boolean>((resolve) => setState({ ...opts, resolve })), []);

  const close = (v: boolean) => {
    state?.resolve(v);
    setState(null);
  };

  return (
    <ConfirmContext.Provider value={confirm}>
      {children}
      <Dialog
        open={!!state}
        onClose={() => close(false)}
        title={state?.title}
        size="sm"
        onSubmit={() => close(true)}
        footer={
          <>
            <Button onClick={() => close(false)}>{t("common.cancel")}</Button>
            <Button type="submit" variant={state?.danger ? "danger" : "primary"}>
              {state?.confirmLabel ?? t("common.confirm")}
            </Button>
          </>
        }
      >
        <div className="text-sm text-muted">{state?.message ?? t("common.areYouSure")}</div>
      </Dialog>
    </ConfirmContext.Provider>
  );
}

export function useConfirm(): ConfirmFn {
  const ctx = useContext(ConfirmContext);
  if (!ctx) throw new Error("useConfirm must be used inside <ConfirmProvider>");
  return ctx;
}
