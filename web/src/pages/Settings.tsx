import { useEffect, useRef, useState, type FormEvent } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { api, errorMessage, startOAuth } from "../api";
import { useAuth } from "../auth";
import { LOCALES, isLang, translateErrorCode, useI18n } from "../i18n";
import { useAsync, useDocumentTitle, type AsyncState } from "../lib/hooks";
import { formatDate } from "../lib/format";
import { OAUTH_PROVIDERS, type Identity, type OAuthProvider } from "../types";
import { Card, Notice, PageHeader } from "../components/Card";
import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { Input, Select } from "../components/Input";
import { useToast } from "../components/Toast";
import { useConfirm } from "../components/Dialog";
import { Spinner } from "../components/Spinner";
import { OAUTH_LABELS, OAuthLogo } from "../components/OAuth";

export function SettingsPage() {
  const { t, lang, pendingLang, setLang } = useI18n();
  const { user, config } = useAuth();
  useDocumentTitle(t("nav.settings"), config?.name);
  const providers = config?.oauth_providers ?? [];

  const identities = useAsync(() => (providers.length ? api.identities() : Promise.resolve({ data: [] as Identity[] })), [providers.length]);
  useOAuthResult();

  return (
    <>
      <PageHeader title={t("settings.title")} />
      <div className="grid max-w-3xl gap-4">
        <Card title={t("settings.profile")}>
          <dl className="grid gap-x-6 gap-y-3 text-[13px] sm:grid-cols-[10rem_1fr]">
            <dt className="text-muted">{t("auth.email")}</dt>
            <dd className="min-w-0 break-all">{user?.email}</dd>
            <dt className="text-muted">{t("settings.name")}</dt>
            <dd>{user?.name || "—"}</dd>
            <dt className="text-muted">{t("settings.role")}</dt>
            <dd>{user && <Badge tone={user.role === "admin" ? "accent" : "neutral"}>{user.role === "admin" ? t("role.admin") : t("role.user")}</Badge>}</dd>
            <dt className="text-muted">{t("settings.memberSince")}</dt>
            <dd>{formatDate(user?.created_at)}</dd>
          </dl>
        </Card>

        {providers.length > 0 && <ConnectedAccountsCard providers={providers} identities={identities} />}

        <PasswordCard />

        <Card title={t("settings.preferences")}>
          <Select
            label={t("header.language")}
            value={pendingLang ?? lang}
            onChange={(e) => isLang(e.target.value) && setLang(e.target.value)}
            wrapperClassName="max-w-xs"
          >
            {LOCALES.map((l) => (
              <option key={l.code} value={l.code} lang={l.code}>
                {l.name}
              </option>
            ))}
          </Select>
        </Card>
      </div>
    </>
  );
}

/** Handles `?linked=<provider>` / `?oauth_error=<code>` after an account-link round trip, then cleans the URL. */
function useOAuthResult() {
  const { t } = useI18n();
  const toast = useToast();
  const location = useLocation();
  const navigate = useNavigate();
  const handled = useRef(false);

  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const linked = params.get("linked");
    const err = params.get("oauth_error");
    if (linked === null && err === null) return;
    if (!handled.current) {
      handled.current = true;
      if (err) toast.error(translateErrorCode(err) ?? t("auth.oauthFailed", { code: err }));
      else if (linked) {
        const name = OAUTH_LABELS[linked as OAuthProvider] ?? linked;
        // The identities list is fetched fresh on mount, so it already includes the new link.
        toast.success(t("settings.linked", { provider: name }));
      }
    }
    params.delete("linked");
    params.delete("oauth_error");
    const search = params.toString();
    navigate({ pathname: location.pathname, search: search ? `?${search}` : "", hash: location.hash }, { replace: true, state: location.state });
  }, [location, navigate, toast, t]);
}

function ConnectedAccountsCard({
  providers,
  identities,
}: {
  providers: OAuthProvider[];
  identities: AsyncState<{ data: Identity[] }>;
}) {
  const { t } = useI18n();
  const toast = useToast();
  const confirm = useConfirm();
  const [busy, setBusy] = useState<OAuthProvider | null>(null);
  const list = identities.data?.data ?? [];
  const ordered = OAUTH_PROVIDERS.filter((p) => providers.includes(p));

  const disconnect = async (id: Identity) => {
    const name = OAUTH_LABELS[id.provider];
    const ok = await confirm({
      title: t("settings.disconnectTitle", { provider: name }),
      message: t("settings.disconnectMessage", { provider: name, email: id.email }),
      confirmLabel: t("settings.disconnect"),
      danger: true,
    });
    if (!ok) return;
    setBusy(id.provider);
    try {
      await api.unlinkIdentity(id.provider);
      identities.setData((prev) => (prev ? { data: prev.data.filter((x) => x.provider !== id.provider) } : prev));
      toast.success(t("settings.disconnected", { provider: name }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <Card title={t("settings.connectedAccounts")} description={t("settings.connectedAccountsDesc")} flush>
      {identities.error && !identities.data ? (
        <div className="flex flex-wrap items-center justify-between gap-2 p-4">
          <span className="text-[13px] text-red-600 dark:text-red-400">{errorMessage(identities.error)}</span>
          <Button size="sm" icon="refresh" onClick={identities.reload}>
            {t("common.retry")}
          </Button>
        </div>
      ) : (
        <ul className="divide-y divide-line">
          {ordered.map((p) => {
            const id = list.find((x) => x.provider === p);
            return (
              <li key={p} className="flex items-center gap-3 px-4 py-3">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-line bg-surface">
                  <OAuthLogo provider={p} />
                </span>
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{OAUTH_LABELS[p]}</div>
                  {identities.loading && !identities.data ? (
                    <Spinner className="mt-1 size-3 text-muted" />
                  ) : id ? (
                    <div className="text-xs text-muted sm:flex sm:gap-1">
                      <div className="truncate" title={id.email}>
                        {id.email}
                      </div>
                      <div className="shrink-0">
                        <span className="hidden sm:inline">· </span>
                        {t("settings.connectedOn", { date: formatDate(id.created_at) })}
                      </div>
                    </div>
                  ) : (
                    <div className="text-xs text-muted">{t("settings.notConnected")}</div>
                  )}
                </div>
                {identities.data &&
                  (id ? (
                    <Button size="sm" variant="danger-ghost" loading={busy === p} disabled={busy !== null} onClick={() => void disconnect(id)}>
                      {t("settings.disconnect")}
                    </Button>
                  ) : (
                    <Button size="sm" disabled={busy !== null} onClick={() => startOAuth(p, "link")}>
                      {t("settings.connect")}
                    </Button>
                  ))}
              </li>
            );
          })}
        </ul>
      )}
    </Card>
  );
}

function PasswordCard() {
  const { t } = useI18n();
  const toast = useToast();
  const { user, refreshUser } = useAuth();
  // Treat a missing field (older server) as "has a password".
  const hasPassword = user?.has_password !== false;
  const [oldPw, setOldPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if ((hasPassword && !oldPw) || !newPw) return setError(t("auth.fillAll"));
    if (newPw.length < 8) return setError(t("auth.passwordTooShort"));
    if (newPw !== confirm) return setError(t("auth.passwordMismatch"));
    setBusy(true);
    setError("");
    try {
      await api.changePassword(hasPassword ? { old_password: oldPw, new_password: newPw } : { new_password: newPw });
      toast.success(hasPassword ? t("settings.passwordChanged") : t("settings.passwordSet"));
      setOldPw("");
      setNewPw("");
      setConfirm("");
      if (!hasPassword) await refreshUser();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card
      title={hasPassword ? t("settings.changePassword") : t("settings.setPassword")}
      description={hasPassword ? t("settings.changePasswordDesc") : t("settings.setPasswordDesc")}
    >
      <form onSubmit={submit} className="flex max-w-sm flex-col gap-4" noValidate>
        {error && (
          <div role="alert">
            <Notice tone="danger">{error}</Notice>
          </div>
        )}
        {hasPassword && (
          <Input label={t("settings.currentPassword")} type="password" autoComplete="current-password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} disabled={busy} />
        )}
        <Input
          label={t("settings.newPassword")}
          type="password"
          autoComplete="new-password"
          value={newPw}
          onChange={(e) => setNewPw(e.target.value)}
          disabled={busy}
          hint={t("auth.passwordHint")}
        />
        <Input label={t("auth.confirmPassword")} type="password" autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} disabled={busy} />
        <div>
          <Button type="submit" variant="primary" loading={busy}>
            {hasPassword ? t("settings.updatePassword") : t("settings.setPasswordButton")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
