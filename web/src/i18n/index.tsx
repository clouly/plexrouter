import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { en, type MessageKey } from "./locales/en";
import { readStorage, writeStorage } from "../lib/storage";
import { setFormatLocale } from "../lib/format";

export type { MessageKey };

/** Supported locales, in menu order, with their native names. */
export const LOCALES = [
  { code: "en", name: "English" },
  { code: "zh-CN", name: "简体中文" },
  { code: "zh-TW", name: "繁體中文" },
  { code: "ja", name: "日本語" },
  { code: "ko", name: "한국어" },
  { code: "es", name: "Español" },
  { code: "fr", name: "Français" },
  { code: "de", name: "Deutsch" },
  { code: "pt-BR", name: "Português (Brasil)" },
  { code: "ru", name: "Русский" },
] as const;

export type Lang = (typeof LOCALES)[number]["code"];
export type Messages = Record<MessageKey, string>;

const STORAGE_KEY = "plexrouter.lang";
const SUPPORTED = new Set<string>(LOCALES.map((l) => l.code));

export function isLang(v: unknown): v is Lang {
  return typeof v === "string" && SUPPORTED.has(v);
}

export function localeName(lang: Lang): string {
  return LOCALES.find((l) => l.code === lang)?.name ?? lang;
}

// Each non-English dictionary is its own chunk; English is bundled as the fallback.
const loaders: Record<Exclude<Lang, "en">, () => Promise<{ default: Messages }>> = {
  "zh-CN": () => import("./locales/zh-CN"),
  "zh-TW": () => import("./locales/zh-TW"),
  ja: () => import("./locales/ja"),
  ko: () => import("./locales/ko"),
  es: () => import("./locales/es"),
  fr: () => import("./locales/fr"),
  de: () => import("./locales/de"),
  "pt-BR": () => import("./locales/pt-BR"),
  ru: () => import("./locales/ru"),
};

const cache = new Map<Lang, Messages>([["en", en]]);

/** Loads (and caches) a dictionary. Rejects if the chunk can't be fetched. */
export async function loadMessages(lang: Lang): Promise<Messages> {
  const hit = cache.get(lang);
  if (hit) return hit;
  const mod = await loaders[lang as Exclude<Lang, "en">]();
  cache.set(lang, mod.default);
  return mod.default;
}

/** Maps a BCP 47 tag (e.g. from navigator.languages) to a supported locale, or null. */
export function matchLang(tag: string): Lang | null {
  const t = tag.trim().toLowerCase().replace(/_/g, "-");
  if (!t) return null;
  const parts = t.split("-");
  const base = parts[0];
  if (base === "zh") {
    return parts.some((p) => p === "hant" || p === "tw" || p === "hk" || p === "mo") ? "zh-TW" : "zh-CN";
  }
  if (base === "pt") return "pt-BR";
  return isLang(base) ? base : null;
}

export function detectLang(): Lang {
  const saved = readStorage(STORAGE_KEY);
  if (isLang(saved)) return saved;
  if (typeof navigator !== "undefined") {
    const prefs = navigator.languages?.length ? navigator.languages : [navigator.language || ""];
    for (const tag of prefs) {
      const m = matchLang(tag);
      if (m) return m;
    }
  }
  return "en";
}

/**
 * Call (and await) before the first render so the initial language is ready and the UI doesn't
 * flash English. Never rejects; a failed chunk just means English is used.
 */
export async function preloadInitialLang(): Promise<void> {
  try {
    await loadMessages(detectLang());
  } catch {
    /* fall back to English */
  }
}

// ---------------------------------------------------------------------------
// Formatting

export type Vars = Record<string, string | number>;

const pluralRulesCache = new Map<string, Intl.PluralRules>();
const numberFormatCache = new Map<string, Intl.NumberFormat>();

function pluralRules(lang: string): Intl.PluralRules {
  let r = pluralRulesCache.get(lang);
  if (!r) pluralRulesCache.set(lang, (r = new Intl.PluralRules(lang)));
  return r;
}

function formatNumber(lang: string, n: number): string {
  let f = numberFormatCache.get(lang);
  if (!f) numberFormatCache.set(lang, (f = new Intl.NumberFormat(lang, { maximumFractionDigits: 20 })));
  return f.format(n);
}

function varText(lang: string, v: string | number): string {
  return typeof v === "number" ? formatNumber(lang, v) : v;
}

/** Index of the "}" matching the "{" at `open`, or -1. */
function matchBrace(s: string, open: number): number {
  let depth = 0;
  for (let i = open; i < s.length; i++) {
    if (s[i] === "{") depth++;
    else if (s[i] === "}" && --depth === 0) return i;
  }
  return -1;
}

const PLURAL_HEAD = /^\{\s*([\w.]+)\s*,\s*plural\s*,/;

/** Resolves `{n, plural, one {...} other {...}}` blocks, then replaces `{name}` placeholders. */
export function formatMessage(lang: string, msg: string, vars?: Vars): string {
  let out = "";
  let i = 0;
  while (i < msg.length) {
    const open = msg.indexOf("{", i);
    if (open < 0) {
      out += msg.slice(i);
      break;
    }
    out += msg.slice(i, open);
    const close = matchBrace(msg, open);
    if (close < 0) {
      out += msg.slice(open);
      break;
    }
    const block = msg.slice(open, close + 1);
    const head = PLURAL_HEAD.exec(block);
    if (head) {
      out += formatPlural(lang, block.slice(head[0].length, -1), vars?.[head[1]], vars);
    } else {
      const name = block.slice(1, -1).trim();
      out += vars && name in vars ? varText(lang, vars[name]) : block;
    }
    i = close + 1;
  }
  return out;
}

function formatPlural(lang: string, body: string, raw: string | number | undefined, vars?: Vars): string {
  const n = typeof raw === "number" ? raw : Number(raw);
  const branches = new Map<string, string>();
  let i = 0;
  while (i < body.length) {
    const open = body.indexOf("{", i);
    if (open < 0) break;
    const sel = body.slice(i, open).trim();
    const close = matchBrace(body, open);
    if (close < 0 || !sel) break;
    branches.set(sel, body.slice(open + 1, close));
    i = close + 1;
  }
  const finite = Number.isFinite(n);
  const chosen =
    (finite ? (branches.get(`=${n}`) ?? branches.get(pluralRules(lang).select(n))) : undefined) ?? branches.get("other") ?? "";
  // Pass raw numbers for plurals; a pre-formatted string is shown as-is.
  const num = typeof raw === "number" ? formatNumber(lang, raw) : String(raw ?? "");
  return formatMessage(lang, chosen.split("#").join(num), vars);
}

// ---------------------------------------------------------------------------
// Translating API error codes. The provider keeps these up to date with the active language so
// non-React code (e.g. errorMessage in api.ts) can translate without a hook.

let activeLang: Lang = "en";
let activeMessages: Messages = en;

/** Returns the localized text for an API error code, or undefined if there's no `errors.<code>` key. */
export function translateErrorCode(code: string | undefined | null): string | undefined {
  if (!code) return undefined;
  const key = `errors.${code}`;
  if (!(key in en)) return undefined;
  return formatMessage(activeLang, activeMessages[key as MessageKey] ?? en[key as MessageKey]);
}

// ---------------------------------------------------------------------------
// React

export type TFunc = (key: MessageKey, vars?: Vars) => string;

interface I18nValue {
  /** The language currently displayed. */
  lang: Lang;
  /** A language whose dictionary is still loading (the previous one stays on screen meanwhile). */
  pendingLang: Lang | null;
  setLang: (l: Lang) => void;
  t: TFunc;
}

const I18nContext = createContext<I18nValue | null>(null);

function initialState(): { lang: Lang; messages: Messages } {
  const lang = detectLang();
  const messages = cache.get(lang);
  // If the initial chunk wasn't preloaded (or failed), start in English; the effect below retries.
  return messages ? { lang, messages } : { lang: "en", messages: en };
}

export function I18nProvider({ children }: { children: ReactNode }) {
  const [active, setActive] = useState(initialState);
  const [pendingLang, setPendingLang] = useState<Lang | null>(() => {
    const want = detectLang();
    return want !== active.lang ? want : null;
  });
  const requestId = useRef(0);

  const { lang, messages } = active;
  activeLang = lang;
  activeMessages = messages;
  setFormatLocale(lang);

  useEffect(() => {
    document.documentElement.lang = lang;
    // Pages set "<page> · <app>" titles through useDocumentTitle, which re-runs when the
    // translated title changes; nothing else to do here.
  }, [lang]);

  const switchTo = useCallback((l: Lang) => {
    const id = ++requestId.current;
    const hit = cache.get(l);
    if (hit) {
      setPendingLang(null);
      setActive({ lang: l, messages: hit });
      return;
    }
    setPendingLang(l);
    loadMessages(l)
      .then((m) => {
        if (id === requestId.current) setActive({ lang: l, messages: m });
      })
      .catch(() => {
        /* keep showing the current language */
      })
      .finally(() => {
        if (id === requestId.current) setPendingLang(null);
      });
  }, []);

  // Initial chunk missing (preload failed): try once more in the background.
  useEffect(() => {
    if (pendingLang) switchTo(pendingLang);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const setLang = useCallback(
    (l: Lang) => {
      writeStorage(STORAGE_KEY, l);
      switchTo(l);
    },
    [switchTo],
  );

  const t = useCallback<TFunc>((key, vars) => formatMessage(lang, messages[key] ?? en[key] ?? key, vars), [lang, messages]);

  const value = useMemo(() => ({ lang, pendingLang, setLang, t }), [lang, pendingLang, setLang, t]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used inside <I18nProvider>");
  return ctx;
}

export function useT(): TFunc {
  return useI18n().t;
}
