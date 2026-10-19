import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api, ApiError } from "./api";
import { OAUTH_PROVIDERS, type AppConfig, type User } from "./types";

interface AuthValue {
  /** undefined while the initial /api/me is in flight, null when logged out. */
  user: User | null | undefined;
  config: AppConfig | null;
  setUser: (u: User | null) => void;
  refreshUser: () => Promise<void>;
  logout: () => Promise<void>;
}

const AuthContext = createContext<AuthValue | null>(null);

const fallbackConfig: AppConfig = { name: "PlexRouter", version: "", allow_register: true, oauth_providers: [] };

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null | undefined>(undefined);
  const [config, setConfig] = useState<AppConfig | null>(null);

  const refreshUser = useCallback(async () => {
    try {
      const r = await api.me();
      setUser(r.user);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) setUser(null);
      else setUser((u) => (u === undefined ? null : u));
    }
  }, []);

  useEffect(() => {
    void refreshUser();
    api
      .config()
      // Older servers may not send oauth_providers; keep only the providers we know how to show.
      .then((c) => setConfig({ ...c, oauth_providers: (c.oauth_providers ?? []).filter((p) => OAUTH_PROVIDERS.includes(p)) }))
      .catch(() => setConfig(fallbackConfig));
  }, [refreshUser]);

  // Document titles ("<page> · <app name>") are set per page by useDocumentTitle, which re-runs when
  // the config name or the language changes.

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setUser(null);
    }
  }, []);

  return <AuthContext.Provider value={{ user, config, setUser, refreshUser, logout }}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}

export function useAppName(): string {
  return useAuth().config?.name || "PlexRouter";
}
