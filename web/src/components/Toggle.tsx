import { cx } from "../lib/cx";

export function Toggle({
  checked,
  onChange,
  disabled,
  label,
  size = "md",
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  label?: string;
  size?: "sm" | "md";
}) {
  const sm = size === "sm";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className={cx(
        "relative inline-flex shrink-0 items-center rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50",
        sm ? "h-4.5 w-8" : "h-5 w-9",
        checked ? "bg-accent" : "bg-line-strong",
      )}
    >
      <span
        className={cx(
          "inline-block rounded-full bg-white shadow transition-transform",
          sm ? "size-3.5" : "size-4",
          checked ? (sm ? "translate-x-4" : "translate-x-[18px]") : "translate-x-0.5",
        )}
      />
    </button>
  );
}
