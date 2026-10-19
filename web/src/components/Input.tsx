import { forwardRef, useId, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from "react";
import { cx } from "../lib/cx";

export const controlClass =
  "w-full rounded-lg border border-line bg-surface px-3 text-sm text-fg placeholder:text-muted/70 shadow-xs transition-colors " +
  "hover:border-line-strong focus:border-accent focus:outline-none focus:ring-2 focus:ring-accent/20 " +
  "disabled:cursor-not-allowed disabled:opacity-60";

export function Field({
  label,
  hint,
  error,
  htmlFor,
  children,
  className,
  required,
}: {
  label?: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  children: ReactNode;
  className?: string;
  required?: boolean;
}) {
  return (
    <div className={cx("flex flex-col gap-1.5", className)}>
      {label && (
        <label htmlFor={htmlFor} className="text-[13px] font-medium text-fg">
          {label}
          {required && <span className="ml-0.5 text-red-500">*</span>}
        </label>
      )}
      {children}
      {error ? <p className="text-xs text-red-600 dark:text-red-400">{error}</p> : hint ? <p className="text-xs text-muted">{hint}</p> : null}
    </div>
  );
}

type InputProps = InputHTMLAttributes<HTMLInputElement> & { label?: ReactNode; hint?: ReactNode; error?: ReactNode; wrapperClassName?: string };

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { label, hint, error, className, wrapperClassName, id, required, ...rest },
  ref,
) {
  const autoId = useId();
  const inputId = id ?? autoId;
  const input = (
    <input
      ref={ref}
      id={inputId}
      required={required}
      aria-invalid={error ? true : undefined}
      className={cx(controlClass, "h-9", error ? "border-red-500" : undefined, className)}
      {...rest}
    />
  );
  if (!label && !hint && !error) return wrapperClassName ? <div className={wrapperClassName}>{input}</div> : input;
  return (
    <Field label={label} hint={hint} error={error} htmlFor={inputId} className={wrapperClassName} required={required}>
      {input}
    </Field>
  );
});

type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { label?: ReactNode; hint?: ReactNode; wrapperClassName?: string };

export function Textarea({ label, hint, className, wrapperClassName, id, ...rest }: TextareaProps) {
  const autoId = useId();
  const inputId = id ?? autoId;
  return (
    <Field label={label} hint={hint} htmlFor={inputId} className={wrapperClassName}>
      <textarea id={inputId} className={cx(controlClass, "min-h-20 py-2", className)} {...rest} />
    </Field>
  );
}

type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & { label?: ReactNode; hint?: ReactNode; wrapperClassName?: string };

export function Select({ label, hint, className, wrapperClassName, id, children, ...rest }: SelectProps) {
  const autoId = useId();
  const inputId = id ?? autoId;
  const select = (
    <div className="relative">
      <select id={inputId} className={cx(controlClass, "h-9 appearance-none pr-8", className)} {...rest}>
        {children}
      </select>
      <svg className="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
        <path d="m6 9 6 6 6-6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </div>
  );
  if (!label && !hint) return wrapperClassName ? <div className={wrapperClassName}>{select}</div> : select;
  return (
    <Field label={label} hint={hint} htmlFor={inputId} className={wrapperClassName}>
      {select}
    </Field>
  );
}

export function Checkbox({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label?: ReactNode; disabled?: boolean }) {
  return (
    <label className={cx("inline-flex min-w-0 items-center gap-2 text-sm", disabled ? "opacity-60" : "cursor-pointer")}>
      <input
        type="checkbox"
        className="size-4 shrink-0 rounded border-line accent-[var(--c-accent)]"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
      {label && <span className="min-w-0">{label}</span>}
    </label>
  );
}

export function SearchInput({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <div className={cx("relative min-w-0", className)}>
      <svg className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
        <path d="M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16zm10 2-4.35-4.35" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className={cx(controlClass, "h-9 pl-8")}
      />
    </div>
  );
}
