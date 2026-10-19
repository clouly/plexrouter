import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, ApiError, errorMessage, oauthCallbackUrl, startOAuth } from "../api";
import { useAuth } from "../auth";
import { useT } from "../i18n";
import { useAsync, useDocumentTitle } from "../lib/hooks";
import { cx } from "../lib/cx";
import { isHttpUrl, isLocalhostUrl, normalizeUrl } from "../lib/url";
import type { OAuthCredentials, OAuthProvider, SetupInput } from "../types";
import { AuthShell } from "./Auth";
import { Button } from "../components/Button";
import { Notice } from "../components/Card";
import { CopyRow } from "../components/CopyButton";
import { ErrorState, LoadingState } from "../components/EmptyState";
import { Icon } from "../components/Icon";
import { Input } from "../components/Input";
import { Segmented } from "../components/Segmented";
import { useToast } from "../components/Toast";
import { OAUTH_LABELS, OAuthLogo } from "../components/OAuth";

const GITHUB_NEW_APP = "https://github.com/settings/applications/new";
const GOOGLE_CREDENTIALS = "https://console.cloud.google.com/apis/credentials";
const DEFAULT_NAME = "PlexRouter";

function ExternalLink({ href, children }: { href: string; children: ReactNode }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      className="inline-flex items-center gap-1 text-[13px] font-medium text-accent hover:underline"
    >
      {children}
      <Icon name="arrowRight" className="size-3.5 -rotate-45" />
    </a>
  );
}

function Step({ n, title, children }: { n: number; title: string; children: ReactNode }) {
  return (
    <section className="flex gap-3">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent tabular-nums">{n}</span>
      <div className="min-w-0 flex-1">
        <h2 className="text-sm leading-6 font-semibold">{title}</h2>
        <div className="mt-2 flex flex-col gap-3">{children}</div>
      </div>
    </section>
  );
}

type CredErrors = Partial<Record<"client_id" | "client_secret" | "public_url", string>>;

const emptyCreds: Record<OAuthProvider, OAuthCredentials> = {
  github: { client_id: "", client_secret: "" },
  google: { client_id: "", client_secret: "" },
};

/** First-run wizard: site name + one OAuth app, then the first sign-in creates the admin. */
export function SetupPage() {
  const t = useT();
  const toast = useToast();
  const { config, refreshConfig } = useAuth();
  const info = useAsync(() => api.setup(), []);

  const [siteName, setSiteName] = useState(config?.name || DEFAULT_NAME);
  const [provider, setProvider] = useState<OAuthProvider>("github");
  const [creds, setCreds] = useState(emptyCreds);
  /** null = use the suggested URL as-is. */
  const [publicUrl, setPublicUrl] = useState<string | null>(null);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [errors, setErrors] = useState<CredErrors>({});
  const [serverError, setServerError] = useState("");
  const [busy, setBusy] = useState(false);

  const name = siteName.trim() || DEFAULT_NAME;
  useDocumentTitle(t("setup.docTitle"), name);

  // Someone else finished setup meanwhile: the config refresh sends this page to /login.
  useEffect(() => {
    if (info.data && !info.data.setup_required) void refreshConfig();
  }, [info.data, refreshConfig]);

  const suggested = normalizeUrl(info.data?.suggested_public_url || window.location.origin);
  const effectiveUrl = publicUrl === null ? suggested : normalizeUrl(publicUrl);
  const urlChanged = publicUrl !== null && effectiveUrl !== suggested;
  const callback = (p: OAuthProvider) =>
    !urlChanged && info.data?.callback_urls?.[p] ? info.data.callback_urls[p] : oauthCallbackUrl(effectiveUrl, p);
  const localhost = isLocalhostUrl(effectiveUrl);

  const setCred = (field: keyof OAuthCredentials, value: string) => {
    setCreds((c) => ({ ...c, [provider]: { ...c[provider], [field]: value } }));
    setErrors((e) => ({ ...e, [field]: undefined }));
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const cur = creds[provider];
    const errs: CredErrors = {};
    if (!cur.client_id.trim()) errs.client_id = t("setup.clientIdRequired");
    if (!cur.client_secret.trim()) errs.client_secret = t("setup.clientSecretRequired");
    if (urlChanged && !isHttpUrl(effectiveUrl)) {
      errs.public_url = t("setup.publicUrlInvalid");
      setShowAdvanced(true);
    }
    setErrors(errs);
    if (Object.keys(errs).length) return;

    const body: SetupInput = {};
    if (siteName.trim() && siteName.trim() !== config?.name) body.site_name = siteName.trim();
    if (urlChanged) body.public_url = effectiveUrl;
    // The selected provider, plus the other one if it was filled in completely too.
    for (const p of ["github", "google"] as const) {
      const c = creds[p];
      if (p === provider || (c.client_id.trim() && c.client_secret.trim())) {
        body[p] = { client_id: c.client_id.trim(), client_secret: c.client_secret.trim() };
      }
    }

    setBusy(true);
    setServerError("");
    try {
      await api.completeSetup(body);
      await refreshConfig();
      startOAuth(provider, "login"); // the page navigates away; keep the button busy
    } catch (err) {
      setBusy(false);
      if (err instanceof ApiError && err.code === "setup_done") {
        toast.error(errorMessage(err));
        void refreshConfig();
        return;
      }
      setServerError(errorMessage(err));
    }
  };

  const label = OAUTH_LABELS[provider];
  const cur = creds[provider];

  return (
    <AuthShell title={t("setup.title", { name })} subtitle={t("setup.subtitle")} wide>
      {info.error && !info.data ? (
        <ErrorState error={info.error} onRetry={info.reload} />
      ) : !info.data ? (
        <LoadingState />
      ) : (
        <form onSubmit={submit} noValidate className="flex flex-col gap-6">
          <Step n={1} title={t("setup.stepWelcome")}>
            <p className="text-[13px] leading-relaxed text-muted">{t("setup.welcomeText")}</p>
            <Input
              label={t("setup.siteName")}
              hint={t("setup.siteNameHint")}
              placeholder={DEFAULT_NAME}
              value={siteName}
              onChange={(e) => setSiteName(e.target.value)}
              maxLength={100}
              disabled={busy}
            />
          </Step>

          <div className="border-t border-line" />

          <Step n={2} title={t("setup.stepSignIn")}>
            <p className="text-[13px] leading-relaxed text-muted">{t("setup.signInText")}</p>
            <Segmented<OAuthProvider>
              label={t("setup.stepSignIn")}
              value={provider}
              onChange={(p) => {
                setProvider(p);
                setErrors((e) => ({ public_url: e.public_url }));
              }}
              options={[
                {
                  value: "github",
                  label: (
                    <span className="inline-flex items-center gap-1.5">
                      <OAuthLogo provider="github" className="size-3.5" /> GitHub
                      <span className="rounded bg-accent-soft px-1 text-[10px] leading-4 font-semibold text-accent">{t("setup.quickest")}</span>
                    </span>
                  ),
                },
                {
                  value: "google",
                  label: (
                    <span className="inline-flex items-center gap-1.5">
                      <OAuthLogo provider="google" className="size-3.5" /> Google
                    </span>
                  ),
                },
              ]}
            />

            {provider === "github" ? (
              <>
                <div className="flex flex-col gap-1">
                  <p className="text-[13px] leading-relaxed">{t("setup.githubStep1")}</p>
                  <ExternalLink href={GITHUB_NEW_APP}>{t("setup.githubLink")}</ExternalLink>
                </div>
                <div className="flex flex-col gap-2">
                  <CopyRow label={t("setup.githubAppName")} value={name} />
                  <CopyRow label={t("setup.githubHomepage")} value={effectiveUrl} />
                  <CopyRow label={t("setup.githubCallback")} value={callback("github")} />
                </div>
              </>
            ) : (
              <>
                <div className="flex flex-col gap-1">
                  <p className="text-[13px] leading-relaxed">{t("setup.googleStep1")}</p>
                  <ExternalLink href={GOOGLE_CREDENTIALS}>{t("setup.googleLink")}</ExternalLink>
                </div>
                <CopyRow label={t("setup.googleRedirect")} value={callback("google")} />
              </>
            )}

            <p className="flex items-start gap-1.5 text-xs text-muted">
              <Icon name="info" className="mt-px size-3.5" />
              <span>{t("setup.exactMatch")}</span>
            </p>
            {localhost && <Notice tone="warning">{t("setup.localhostWarning")}</Notice>}

            <p className="text-[13px] leading-relaxed">{provider === "github" ? t("setup.githubStep2") : t("setup.googleStep2")}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <Input
                label={t("setup.clientId")}
                value={cur.client_id}
                onChange={(e) => setCred("client_id", e.target.value)}
                error={errors.client_id}
                autoComplete="off"
                spellCheck={false}
                required
                disabled={busy}
              />
              <Input
                label={t("setup.clientSecret")}
                type="password"
                value={cur.client_secret}
                onChange={(e) => setCred("client_secret", e.target.value)}
                error={errors.client_secret}
                hint={provider === "github" ? t("setup.githubSecretHint") : undefined}
                autoComplete="new-password"
                spellCheck={false}
                required
                disabled={busy}
              />
            </div>

            <div>
              <button
                type="button"
                onClick={() => setShowAdvanced((v) => !v)}
                aria-expanded={showAdvanced}
                className="inline-flex items-center gap-1 text-xs font-medium text-muted hover:text-fg"
              >
                <Icon name="chevronRight" className={cx("size-3.5 transition-transform", showAdvanced && "rotate-90")} />
                {t("setup.publicUrlToggle")}
              </button>
              {showAdvanced && (
                <div className="mt-2">
                  <Input
                    label={t("setup.publicUrl")}
                    value={publicUrl ?? suggested}
                    onChange={(e) => {
                      setPublicUrl(e.target.value);
                      setErrors((x) => ({ ...x, public_url: undefined }));
                    }}
                    error={errors.public_url}
                    hint={t("setup.publicUrlHint")}
                    placeholder={suggested}
                    inputMode="url"
                    spellCheck={false}
                    disabled={busy}
                  />
                </div>
              )}
            </div>
          </Step>

          <div className="border-t border-line" />

          <Step n={3} title={t("setup.stepFinish")}>
            <p className="text-[13px] leading-relaxed text-muted">{t("setup.finishText", { provider: label })}</p>
            {serverError && (
              <div role="alert">
                <Notice tone="danger">{serverError}</Notice>
              </div>
            )}
            <Button type="submit" variant="primary" loading={busy} className="w-full">
              {!busy && <OAuthLogo provider={provider} className="size-4" />}
              {t("setup.finish", { provider: label })}
            </Button>
          </Step>
        </form>
      )}
    </AuthShell>
  );
}
