import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { readStorage, writeStorage } from "./lib/storage";

export type ThemePref = "light" | "dark" | "system";
const STORAGE_KEY = "plexrouter.theme";

interface ThemeValue {
  pref: ThemePref;
  dark: boolean;
  toggle: () => void;
}

const ThemeContext = createContext<ThemeValue | null>(null);

function systemDark(): boolean {
  return typeof window !== "undefined" && window.matchMedia?.("(prefers-color-scheme: dark)").matches;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [pref, setPref] = useState<ThemePref>(() => {
    const s = readStorage(STORAGE_KEY);
    return s === "light" || s === "dark" ? s : "system";
  });
  const [sysDark, setSysDark] = useState(systemDark);

  useEffect(() => {
    const mq = window.matchMedia?.("(prefers-color-scheme: dark)");
    if (!mq) return;
    const onChange = () => setSysDark(mq.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  const dark = pref === "system" ? sysDark : pref === "dark";

  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
  }, [dark]);

  const toggle = useCallback(() => {
    const next: ThemePref = dark ? "light" : "dark";
    setPref(next);
    writeStorage(STORAGE_KEY, next);
  }, [dark]);

  return <ThemeContext.Provider value={{ pref, dark, toggle }}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useTheme must be used inside <ThemeProvider>");
  return ctx;
}
