import { useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, useLocation, useNavigate } from "react-router-dom";
import { api, errorMessage } from "../api";
import { useAuth, useAppName } from "../auth";
import { useT } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { Button } from "../components/Button";
import { Input } from "../components/Input";
import { Icon } from "../components/Icon";
import { Notice } from "../components/Card";
import { LangToggle, ThemeToggle } from "../layout/Layout";

function AuthShell({ title, subtitle, children, footer }: { title: string; subtitle?: string; children: ReactNode; footer?: ReactNode }) {
  const name = useAppName();
  const t = useT();
  return (
    <div className="flex min-h-dvh flex-col">
      <header className="flex h-14 items-center justify-between px-4 sm:px-6">
        <Link to="/models" className="flex items-center gap-2 font-semibold tracking-tight">
          <span className="flex size-7 items-center justify-center rounded-lg bg-accent text-accent-fg">
            <Icon name="zap" className="size-4" strokeWidth={2.2} />
          </span>
          {name}
        </Link>
        <div className="flex items-center gap-1">
          <LangToggle />
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
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
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
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        {error && <Notice tone="danger">{error}</Notice>}
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
      <form onSubmit={submit} className="flex flex-col gap-4" noValidate>
        {error && <Notice tone="danger">{error}</Notice>}
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
