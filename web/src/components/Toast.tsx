import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { cx } from "../lib/cx";
import { Icon } from "./Icon";

type ToastKind = "success" | "error" | "info";
interface ToastItem {
  id: number;
  kind: ToastKind;
  message: string;
}

interface ToastApi {
  success: (msg: string) => void;
  error: (msg: string) => void;
  info: (msg: string) => void;
}

const ToastContext = createContext<ToastApi | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const dismiss = useCallback((id: number) => setItems((xs) => xs.filter((x) => x.id !== id)), []);

  const push = useCallback(
    (kind: ToastKind, message: string) => {
      const id = nextId.current++;
      setItems((xs) => [...xs.slice(-4), { id, kind, message }]);
      window.setTimeout(() => dismiss(id), kind === "error" ? 6000 : 3500);
    },
    [dismiss],
  );

  const api = useMemo<ToastApi>(
    () => ({ success: (m) => push("success", m), error: (m) => push("error", m), info: (m) => push("info", m) }),
    [push],
  );

  return (
    <ToastContext.Provider value={api}>
      {children}
      {createPortal(
        <div className="pointer-events-none fixed inset-x-0 bottom-0 z-[60] flex flex-col items-center gap-2 p-4 sm:items-end">
          {items.map((it) => (
            <div
              key={it.id}
              role={it.kind === "error" ? "alert" : "status"}
              className="animate-toast pointer-events-auto flex w-full max-w-sm items-start gap-2.5 rounded-lg border border-line bg-surface px-3.5 py-3 text-sm shadow-lg"
            >
              <Icon
                name={it.kind === "success" ? "check" : it.kind === "error" ? "alert" : "info"}
                className={cx(
                  "mt-0.5 size-4",
                  it.kind === "success" && "text-emerald-600 dark:text-emerald-400",
                  it.kind === "error" && "text-red-600 dark:text-red-400",
                  it.kind === "info" && "text-accent",
                )}
              />
              <div className="min-w-0 flex-1 break-words">{it.message}</div>
              <button type="button" className="text-muted hover:text-fg" onClick={() => dismiss(it.id)} aria-label="×">
                <Icon name="x" className="size-3.5" />
              </button>
            </div>
          ))}
        </div>,
        document.body,
      )}
    </ToastContext.Provider>
  );
}

export function useToast(): ToastApi {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside <ToastProvider>");
  return ctx;
}
