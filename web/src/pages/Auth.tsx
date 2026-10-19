import { useEffect, useState, type ReactNode } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { useAuth, useAppName } from "../auth";
import { translateErrorCode, useT } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { Icon } from "../components/Icon";
import { Notice } from "../components/Card";
import { ThemeToggle } from "../layout/Layout";
import { LanguageMenu } from "../components/LanguageMenu";
import { OAuthButtons } from "../components/OAuth";

/** Centered page with the app name, language and theme toggles; used by the login page and the setup wizard. */
export function AuthShell({
  title,
  subtitle,
  children,
  footer,
  wide,
}: {
  title: string;
  subtitle?: string;
  children: ReactNode;
  footer?: ReactNode;
  /** Wider column for longer forms (setup wizard). */
  wide?: boolean;
}) {
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
      <main className={`flex flex-1 items-start justify-center px-4 pb-12 ${wide ? "pt-[4vh]" : "pt-[8vh]"}`}>
        <div className={`w-full ${wide ? "max-w-xl" : "max-w-sm"}`}>
          <h1 className="text-center text-xl font-semibold tracking-tight">{title}</h1>
          {subtitle && <p className="mt-1.5 text-center text-sm text-muted">{subtitle}</p>}
          <div className={`mt-6 rounded-xl border border-line bg-surface shadow-xs ${wide ? "p-4 sm:p-6" : "p-5"}`}>{children}</div>
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
function useOAuthError(): { code: string | null; message: string } {
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

  if (!code) return { code: null, message: "" };
  return { code, message: translateErrorCode(code) ?? t("auth.oauthFailed", { code }) };
}

/** Sign-in is only through Google / GitHub; the first sign-in creates the account. */
export function LoginPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("auth.login"), config?.name);
  const { code: oauthCode, message: oauthError } = useOAuthError();
  const providers = config?.oauth_providers ?? [];

  // A fresh instance goes to the setup wizard — unless the first sign-in just failed, in which case
  // the error stays here with a way back to setup to fix the credentials.
  if (config?.setup_required && !oauthCode) return <Navigate to="/setup" replace />;

  return (
    <AuthShell
      title={t("auth.loginTitle", { name: config?.name || "PlexRouter" })}
      subtitle={t("auth.loginSubtitle")}
    >
      {oauthError && (
        <div role="alert" className="mb-4">
          <Notice tone="danger">
            {oauthError}
            {config?.setup_required && (
              <span className="mt-1.5 block">
                {t("auth.setupRetryHint")}{" "}
                <Link to="/setup" className="font-medium underline underline-offset-2 hover:no-underline">
                  {t("auth.backToSetup")}
                </Link>
              </span>
            )}
          </Notice>
        </div>
      )}
      {config && providers.length === 0 ? (
        <Notice tone="warning">{t("auth.notConfigured")}</Notice>
      ) : (
        <>
          <OAuthButtons providers={providers} />
          <p className="mt-4 text-center text-xs leading-relaxed whitespace-pre-line text-balance text-muted">
            {config?.allow_register === false ? t("auth.registrationClosedHint") : t("auth.newAccountHint")}
          </p>
        </>
      )}
    </AuthShell>
  );
}
