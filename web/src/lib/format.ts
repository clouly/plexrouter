// Formatting helpers. Locale-aware where it matters; currency is always USD.

let currentLocale = "en";
export function setFormatLocale(locale: string) {
  currentLocale = locale;
}

const nfCache = new Map<string, Intl.NumberFormat>();
function nf(opts: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = currentLocale + JSON.stringify(opts);
  let f = nfCache.get(key);
  if (!f) {
    try {
      f = new Intl.NumberFormat(currentLocale, opts);
    } catch {
      f = new Intl.NumberFormat("en-US", opts);
    }
    nfCache.set(key, f);
  }
  return f;
}

function usd(n: number, maxDigits: number, sign: boolean): string {
  return nf({
    style: "currency",
    currency: "USD",
    currencyDisplay: "narrowSymbol", // "$" everywhere, not "US$"
    minimumFractionDigits: 2,
    maximumFractionDigits: maxDigits,
    signDisplay: sign ? "exceptZero" : "auto",
  }).format(n);
}

/**
 * USD with adaptive precision, formatted for the active locale:
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
  if (abs > 0 && Number(abs.toFixed(maxDigits)) === 0) {
    return `${n < 0 ? "-" : ""}<${usd(0.00000001, 8, false)}`;
  }
  return usd(n === 0 ? 0 : n, maxDigits, !!opts.sign); // normalize -0
}

/** Price per 1M tokens. */
export function formatPrice(n: number): string {
  return formatUSD(n);
}

/** Cache price/cost per 1M tokens; null (= charged as the input price) shows as "—". */
export function formatCachePrice(n: number | null | undefined): string {
  return n === null || n === undefined ? "—" : formatUSD(n);
}

/** 1234567 → "1,234,567" (locale grouping) */
export function formatInt(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  return nf({ maximumFractionDigits: 0 }).format(Math.round(n));
}

/** Locale decimal with up to `digits` fraction digits and no grouping (for compact units). */
function dec(n: number, digits: number, minDigits = 0): string {
  return nf({ minimumFractionDigits: minDigits, maximumFractionDigits: digits, useGrouping: false }).format(n);
}

/** Compact token counts: 950 → "950", 12_300 → "12.3k", 4_500_000 → "4.5M". */
export function formatTokens(n: number | null | undefined): string {
  if (n === null || n === undefined || !Number.isFinite(n)) return "—";
  const abs = Math.abs(n);
  const fmt = (v: number, suffix: string) => `${dec(Number(v.toFixed(v >= 100 ? 0 : 1)), 1)}${suffix}`;
  if (abs >= 1e9) return fmt(n / 1e9, "B");
  if (abs >= 1e6) return fmt(n / 1e6, "M");
  if (abs >= 1e4) return fmt(n / 1e3, "k");
  return formatInt(n);
}

/** Context window: 128000 → "128K", 1048576 → "1M". */
export function formatContext(n: number): string {
  if (!n) return "—";
  if (n >= 1_000_000) return `${dec(n / 1_000_000, 2)}M`;
  if (n >= 1000) return `${dec(n / 1000, 1)}K`;
  return String(n);
}

export function formatLatency(ms: number): string {
  if (!Number.isFinite(ms)) return "—";
  if (ms >= 10_000) return `${dec(ms / 1000, 1, 1)}s`;
  if (ms >= 1000) return `${dec(ms / 1000, 2, 2)}s`;
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

/** 0.982 → "98.2%" (locale-aware). */
export function formatPercent(ratio: number | null | undefined, opts: { digits?: number; sign?: boolean } = {}): string {
  if (ratio === null || ratio === undefined || Number.isNaN(ratio)) return "—";
  if (!Number.isFinite(ratio)) return ratio > 0 ? (opts.sign ? "+∞" : "∞") : "-∞";
  return nf({
    style: "percent",
    maximumFractionDigits: opts.digits ?? 1,
    signDisplay: opts.sign ? "exceptZero" : "auto",
  }).format(ratio);
}

/** Time of day, e.g. "14:05". */
export function formatTime(iso: string | null | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString(currentLocale, { hour: "2-digit", minute: "2-digit" });
}

/** Short duration from seconds, e.g. "2 min 13 s" / "2m 13s" depending on the locale. */
export function formatDuration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const unit = (n: number, u: "hour" | "minute" | "second") => nf({ style: "unit", unit: u, unitDisplay: "narrow" }).format(n);
  if (s >= 3600) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return m ? `${unit(h, "hour")} ${unit(m, "minute")}` : unit(h, "hour");
  }
  if (s >= 60) {
    const m = Math.floor(s / 60);
    const r = s % 60;
    return r ? `${unit(m, "minute")} ${unit(r, "second")}` : unit(m, "minute");
  }
  return unit(s, "second");
}

/** True when an RFC 3339 timestamp is in the future (relative to `now`, default Date.now()). */
export function isFuture(iso: string | null | undefined, now = Date.now()): boolean {
  if (!iso) return false;
  const t = new Date(iso).getTime();
  return Number.isFinite(t) && t > now;
}
