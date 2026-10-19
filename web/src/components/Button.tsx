import { forwardRef, type ButtonHTMLAttributes } from "react";
import { cx } from "../lib/cx";
import { Spinner } from "./Spinner";
import { Icon, type IconName } from "./Icon";

type Variant = "primary" | "secondary" | "ghost" | "danger" | "danger-ghost";
type Size = "xs" | "sm" | "md";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
  loading?: boolean;
  icon?: IconName;
}

const variants: Record<Variant, string> = {
  primary: "bg-accent text-accent-fg hover:bg-accent-hover border border-transparent shadow-sm",
  secondary: "bg-surface text-fg border border-line hover:bg-subtle hover:border-line-strong shadow-xs",
  ghost: "text-fg hover:bg-subtle border border-transparent",
  danger: "bg-red-600 text-white hover:bg-red-700 border border-transparent shadow-sm",
  "danger-ghost": "text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/40 border border-transparent",
};

const sizes: Record<Size, string> = {
  xs: "h-7 px-2 text-xs gap-1 rounded-md",
  sm: "h-8 px-2.5 text-[13px] gap-1.5 rounded-md",
  md: "h-9 px-3.5 text-sm gap-2 rounded-lg",
};

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "secondary", size = "md", loading, icon, disabled, className, children, type = "button", ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      disabled={disabled || loading}
      className={cx(
        "inline-flex shrink-0 items-center justify-center font-medium whitespace-nowrap transition-colors select-none",
        "disabled:cursor-not-allowed disabled:opacity-50",
        variants[variant],
        sizes[size],
        className,
      )}
      {...rest}
    >
      {loading ? <Spinner className={size === "md" ? "size-4" : "size-3.5"} /> : icon ? <Icon name={icon} className={size === "md" ? "size-4" : "size-3.5"} /> : null}
      {children}
    </button>
  );
});

export function IconButton({
  icon,
  label,
  className,
  ...rest
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> & { icon: IconName; label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      className={cx(
        "inline-flex size-8 items-center justify-center rounded-md text-muted transition-colors hover:bg-subtle hover:text-fg disabled:opacity-50",
        className,
      )}
      {...rest}
    >
      <Icon name={icon} />
    </button>
  );
}
