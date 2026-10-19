import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { api, errorMessage } from "../api";
import { translateErrorCode } from "../i18n";
import { useAuth, useAppName } from "../auth";
import { useT } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { Button } from "../components/Button";
import { Input } from "../components/Input";
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
          {footer && <div className="mt-4 text-center text-sm text-muted">{footer}</div>}
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

function useAfterAuth() {
  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from;
  return () => navigate(from && from.startsWith("/") && !from.startsWith("/login") ? from : "/", { replace: true });
}

export function LoginPage() {
  const t = useT();
  const { setUser, config } = useAuth();
  const done = useAfterAuth();
  const location = useLocation();
  useDocumentTitle(t("auth.login"), config?.name);
  const oauthError = useOAuthError();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  // The OAuth error stays until the user tries the email form.
  const [oauthDismissed, setOauthDismissed] = useState(false);
  const shownError = error || (oauthDismissed ? "" : oauthError);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setOauthDismissed(true);
    if (!email.trim() || !password) {
      setError(t("auth.fillAll"));
      return;
    }
    setBusy(true);
    setError("");
    try {
      const r = await api.login({ email: email.trim(), password });
      setUser(r.user);
      done();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell
      title={t("auth.loginTitle", { name: config?.name || "PlexRouter" })}
      subtitle={t("auth.loginSubtitle")}
      footer={
        config?.allow_register !== false && (
          <>
            {t("auth.noAccount")}{" "}
            <Link to="/register" state={location.state} className="font-medium text-accent hover:underline">
              {t("auth.register")}
            </Link>
          </>
        )
      }
    >
      {shownError && (
        <div role="alert" className="mb-4">
          <Notice tone="danger">{shownError}</Notice>
        </div>
      )}
      <OAuthButtons providers={config?.oauth_providers ?? []} disabled={busy} />
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Input label={t("auth.email")} type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} disabled={busy} autoFocus />
        <Input
          label={t("auth.password")}
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={busy}
        />
        <Button type="submit" variant="primary" loading={busy} className="w-full">
          {t("auth.login")}
        </Button>
      </form>
    </AuthShell>
  );
}

export function RegisterPage() {
  const t = useT();
  const { setUser, config } = useAuth();
  const done = useAfterAuth();
  const location = useLocation();
  useDocumentTitle(t("auth.register"), config?.name);
  const [email, setEmail] = useState("");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (config && !config.allow_register) return <Navigate to="/login" replace />;

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!email.trim() || !password) return setError(t("auth.fillAll"));
    if (password.length < 8) return setError(t("auth.passwordTooShort"));
    if (password !== confirm) return setError(t("auth.passwordMismatch"));
    setBusy(true);
    setError("");
    try {
      const r = await api.register({ email: email.trim(), password, name: name.trim() || undefined });
      setUser(r.user);
      done();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <AuthShell
      title={t("auth.registerTitle")}
      subtitle={t("auth.registerSubtitle")}
      footer={
        <>
          {t("auth.haveAccount")}{" "}
          <Link to="/login" state={location.state} className="font-medium text-accent hover:underline">
            {t("auth.login")}
          </Link>
        </>
      }
    >
      {error && (
        <div role="alert" className="mb-4">
          <Notice tone="danger">{error}</Notice>
        </div>
      )}
      <OAuthButtons providers={config?.oauth_providers ?? []} disabled={busy} />
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        <Input label={t("auth.email")} type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} disabled={busy} autoFocus required />
        <Input label={t("auth.nameOptional")} autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} disabled={busy} />
        <Input
          label={t("auth.password")}
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={busy}
          hint={t("auth.passwordHint")}
          required
        />
        <Input
          label={t("auth.confirmPassword")}
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          disabled={busy}
          required
        />
        <Button type="submit" variant="primary" loading={busy} className="w-full">
          {t("auth.createAccount")}
        </Button>
      </form>
    </AuthShell>
  );
}
