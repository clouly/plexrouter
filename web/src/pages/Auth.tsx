import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { translateErrorCode } from "../i18n";
import { useAuth, useAppName } from "../auth";
import { useT } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { Icon } from "../components/Icon";
import { Notice } from "../components/Card";
import { ThemeToggle } from "../layout/Layout";
import { LanguageMenu } from "../components/LanguageMenu";
import { OAuthButtons } from "../components/OAuth";

function AuthShell({ title, subtitle, children, footer }: { title: string; subtitle?: string; children: ReactNode; footer?: ReactNode }) {
  const name = useAppName();
  const t = useT();
  return (
    <div className="flex min-h-dvh flex-col">
      <header className="flex h-14 items-center justify-between px-4 sm:px-6">
        <Link to="/models" className="flex min-w-0 items-center gap-2 font-semibold tracking-tight">
          <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-fg">
            <Icon name="zap" className="size-4" strokeWidth={2.2} />
          </span>
          <span className="truncate">{name}</span>
        </Link>
        <div className="flex items-center gap-1">
          <LanguageMenu />
          <ThemeToggle />
        </div>
      </header>
      <main className="flex flex-1 items-start justify-center px-4 pt-[8vh] pb-12">
        <div className="w-full max-w-sm">
          <h1 className="text-center text-xl font-semibold tracking-tight">{title}</h1>
          {subtitle && <p className="mt-1.5 text-center text-sm text-muted">{subtitle}</p>}
          <div className="mt-6 rounded-xl border border-line bg-surface p-5 shadow-xs">{children}</div>
          {footer && <div className="mt-4 text-center text-xs text-muted">{footer}</div>}
          <div className="mt-6 flex justify-center gap-4 text-xs text-muted">
            <Link to="/models" className="hover:text-fg">
              {t("nav.models")}
            </Link>
            <Link to="/docs" className="hover:text-fg">
              {t("nav.docs")}
            </Link>
          </div>
        </div>
      </main>
    </div>
  );
}

/**
 * Reads `?oauth_error=<code>` once (set by the server after a failed Google/GitHub sign-in),
 * returns its localized text, and strips the param from the URL.
 */
function useOAuthError(): string {
  const t = useT();
  const location = useLocation();
  const navigate = useNavigate();
  const [code] = useState(() => new URLSearchParams(location.search).get("oauth_error"));

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    if (!params.has("oauth_error")) return;
    params.delete("oauth_error");
    const search = params.toString();
    navigate({ pathname: location.pathname, search: search ? `?${search}` : "", hash: location.hash }, { replace: true, state: location.state });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  if (!code) return "";
  return translateErrorCode(code) ?? t("auth.oauthFailed", { code });
}

/** Sign-in is only through Google / GitHub; the first sign-in creates the account. */
export function LoginPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("auth.login"), config?.name);
  const oauthError = useOAuthError();
  const providers = config?.oauth_providers ?? [];

  return (
    <AuthShell
      title={t("auth.loginTitle", { name: config?.name || "PlexRouter" })}
      subtitle={t("auth.loginSubtitle")}
      footer={providers.length > 0 && (config?.allow_register === false ? t("auth.registrationClosedHint") : t("auth.newAccountHint"))}
    >
      {oauthError && (
        <div role="alert" className="mb-4">
          <Notice tone="danger">{oauthError}</Notice>
        </div>
      )}
      {config && providers.length === 0 ? <Notice tone="warning">{t("auth.notConfigured")}</Notice> : <OAuthButtons providers={providers} />}
    </AuthShell>
  );
}
