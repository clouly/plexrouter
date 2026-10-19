import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { en, type MessageKey } from "./en";
import { zhCN } from "./zh-CN";
import { readStorage, writeStorage } from "../lib/storage";
import { setFormatLocale } from "../lib/format";

export type Lang = "en" | "zh-CN";
export type { MessageKey };

const dictionaries: Record<Lang, Record<MessageKey, string>> = { en, "zh-CN": zhCN };
const STORAGE_KEY = "plexrouter.lang";

function detectLang(): Lang {
  const saved = readStorage(STORAGE_KEY);
  if (saved === "en" || saved === "zh-CN") return saved;
  const nav = typeof navigator !== "undefined" ? navigator.language || "" : "";
  return nav.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

export type TFunc = (key: MessageKey, vars?: Record<string, string | number>) => string;

interface I18nValue {
  lang: Lang;
  setLang: (l: Lang) => void;
  t: TFunc;
}

const I18nContext = createContext<I18nValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detectLang);
  setFormatLocale(lang);

  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);

  const setLang = useCallback((l: Lang) => {
    setLangState(l);
    writeStorage(STORAGE_KEY, l);
  }, []);

  const t = useCallback<TFunc>(
    (key, vars) => {
      let s = dictionaries[lang][key] ?? en[key] ?? key;
      if (vars) {
        for (const [k, v] of Object.entries(vars)) s = s.split(`{${k}}`).join(String(v));
      }
      return s;
    },
    [lang],
  );

  const value = useMemo(() => ({ lang, setLang, t }), [lang, setLang, t]);
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
