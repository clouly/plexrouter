// Formatting helpers. Locale-aware where it matters; currency is always USD.

let currentLocale = "en";
export function setFormatLocale(locale: string) {
  currentLocale = locale;
}

/**
 * USD with adaptive precision:
 *  - |n| >= 1   → 2 decimals ($1,234.50)
 *  - |n| < 1    → enough decimals for ~3 significant digits, max 8 ($0.000123)
 *  - 0          → $0.00
 */
export function formatUSD(n: number | null | undefined, opts: { sign?: boolean } = {}): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const abs = Math.abs(n);
  let maxDigits = 2;
  if (abs > 0 && abs < 1) {
    maxDigits = Math.min(8, Math.max(2, -Math.floor(Math.log10(abs)) + 2));
  }
  const body = abs.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: maxDigits });
  if (abs > 0 && Number(body.replace(/,/g, "")) === 0) {
    return `${n < 0 ? "-" : ""}<$0.00000001`;
  }
  const sign = n < 0 ? "-" : opts.sign && n > 0 ? "+" : "";
  return `${sign}$${body}`;
}

/** Price per 1M tokens. */
export function formatPrice(n: number): string {
  return formatUSD(n);
}

/** 1234567 → "1,234,567" */
export function formatInt(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  return Math.round(n).toLocaleString("en-US");
}

/** Compact token counts: 950 → "950", 12_300 → "12.3k", 4_500_000 → "4.5M". */
export function formatTokens(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const abs = Math.abs(n);
  const fmt = (v: number, suffix: string) => `${v.toFixed(v >= 100 ? 0 : 1).replace(/\.0$/, "")}${suffix}`;
  if (abs >= 1e9) return fmt(n / 1e9, "B");
  if (abs >= 1e6) return fmt(n / 1e6, "M");
  if (abs >= 1e4) return fmt(n / 1e3, "k");
  return formatInt(n);
}

/** Context window: 128000 → "128K", 1048576 → "1M". */
export function formatContext(n: number): string {
  if (!n) return "—";
  if (n >= 1_000_000) return `${+(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1000) return `${+(n / 1000).toFixed(1)}K`;
  return String(n);
}

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms)) return "—";
  if (ms >= 10_000) return `${(ms / 1000).toFixed(1)}s`;
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`;
  return `${Math.round(ms)}ms`;
}

export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(currentLocale, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function formatDate(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString(currentLocale, { year: "numeric", month: "short", day: "numeric" });
}

/** "YYYY-MM-DD" → short label like "Oct 19" (no timezone shift). */
export function formatDay(day: string): string {
  const [y, m, d] = day.split("-").map(Number);
  if (!y || !m || !d) return day;
  return new Date(y, m - 1, d).toLocaleDateString(currentLocale, { month: "short", day: "numeric" });
}

export function formatRelative(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const diff = (d.getTime() - Date.now()) / 1000;
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(currentLocale, { numeric: "auto" });
  if (abs < 45) return rtf.format(Math.round(diff), "second");
  if (abs < 2700) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 79200) return rtf.format(Math.round(diff / 3600), "hour");
  if (abs < 86400 * 26) return rtf.format(Math.round(diff / 86400), "day");
  return formatDate(iso);
}

/** Parses a user-typed number; returns null for empty, NaN for invalid. */
export function parseNumber(s: string): number | null {
  const v = s.trim();
  if (v === "") return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : NaN;
}
