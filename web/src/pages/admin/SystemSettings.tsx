import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { useT, type MessageKey } from "../../i18n";
import { useAsync, useDocumentTitle } from "../../lib/hooks";
import { parseNumber } from "../../lib/format";
import { isHttpUrl, normalizeUrl } from "../../lib/url";
import type { OAuthProvider, SettingSource, SystemSettings, SystemSettingsInput, SystemSettingsResponse } from "../../types";
import { Card, Notice, PageHeader } from "../../components/Card";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { CopyRow } from "../../components/CopyButton";
import { useConfirm } from "../../components/Dialog";
import { ErrorState, LoadingState } from "../../components/EmptyState";
import { Icon } from "../../components/Icon";
import { Input } from "../../components/Input";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../components/Toast";
import { OAUTH_LABELS, OAuthLogo } from "../../components/OAuth";

type ScalarKey = Exclude<keyof SystemSettings, "github" | "google">;

/** Saves a PATCH and returns true on success; shared by every section. */
type SaveFn = (body: SystemSettingsInput, success: string) => Promise<boolean>;

export function AdminSystemSettingsPage() {
  const t = useT();
  const toast = useToast();
  const { config, refreshConfig } = useAuth();
  useDocumentTitle(t("nav.adminSettings"), config?.name);
  const res = useAsync(() => api.admin.settings(), []);

  const save: SaveFn = async (body, success) => {
    try {
      res.setData(await api.admin.updateSettings(body));
      toast.success(success);
      // Site name, allow_register and the sign-in providers are part of /api/config.
      void refreshConfig();
      return true;
    } catch (e) {
      toast.error(errorMessage(e));
      return false;
    }
  };

  return (
    <>
      <PageHeader title={t("sysSettings.title")} description={t("sysSettings.desc")} />
      {res.error && !res.data ? (
        <Card>
          <ErrorState error={res.error} onRetry={res.reload} />
        </Card>
      ) : !res.data ? (
        <Card>
          <LoadingState />
        </Card>
      ) : (
        <div className="grid max-w-3xl gap-4">
          <ScalarSection
            data={res.data}
            save={save}
            title={t("sysSettings.general")}
            description={t("sysSettings.generalDesc")}
            fields={[
              { key: "site_name", kind: "text", label: "sysSettings.siteName", hint: t("sysSettings.siteNameHint"), required: true },
              {
                key: "public_url",
                kind: "url",
                label: "sysSettings.publicUrl",
                hint: t("sysSettings.publicUrlHint", { origin: window.location.origin }),
                placeholder: window.location.origin,
              },
            ]}
          />
          <SignInSection data={res.data} save={save} />
          <ScalarSection
            data={res.data}
            save={save}
            title={t("sysSettings.registration")}
            description={t("sysSettings.registrationDesc")}
            fields={[
              { key: "allow_register", kind: "toggle", label: "sysSettings.allowRegister", hint: t("sysSettings.allowRegisterDesc") },
              { key: "signup_credit", kind: "number", label: "sysSettings.signupCredit", hint: t("sysSettings.signupCreditHint"), min: 0 },
            ]}
          />
          <ScalarSection
            data={res.data}
            save={save}
            title={t("sysSettings.limits")}
            description={t("sysSettings.limitsDesc")}
            fields={[
              { key: "default_user_rpm", kind: "number", label: "sysSettings.defaultRpm", hint: t("sysSettings.defaultRpmHint"), min: 0, integer: true },
              { key: "request_timeout_seconds", kind: "number", label: "sysSettings.timeout", hint: t("sysSettings.timeoutHint"), min: 5, integer: true },
            ]}
          />
          <ScalarSection
            data={res.data}
            save={save}
            title={t("sysSettings.data")}
            description={t("sysSettings.dataDesc")}
            fields={[{ key: "log_retention_days", kind: "number", label: "sysSettings.retention", hint: t("sysSettings.retentionHint"), min: 0, integer: true }]}
          />
          <ScalarSection
            data={res.data}
            save={save}
            title={t("sysSettings.network")}
            description={t("sysSettings.networkDesc")}
            fields={[{ key: "trust_proxy", kind: "toggle", label: "sysSettings.trustProxy", hint: t("sysSettings.trustProxyDesc") }]}
          />
        </div>
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Where a value comes from

function SourceNote({ source, onReset, busy }: { source: SettingSource | undefined; onReset?: () => void; busy?: boolean }) {
  const t = useT();
  if (source === "env") {
    return (
      <p className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-400">
        <Icon name="info" className="mt-px size-3.5" />
        <span>{t("sysSettings.fromEnv")}</span>
      </p>
    );
  }
  if (source === "console") {
    return (
      <p className="text-xs text-muted">
        {t("sysSettings.fromConsole")}
        {onReset && (
          <>
            {" · "}
            <button
              type="button"
              onClick={onReset}
              disabled={busy}
              title={t("sysSettings.resetHint")}
              className="font-medium text-accent hover:underline disabled:opacity-50"
            >
              {t("sysSettings.reset")}
            </button>
          </>
        )}
      </p>
    );
  }
  return null;
}

/** A card whose body is a form with one Save button at the bottom. */
function SectionCard({
  title,
  description,
  children,
  onSubmit,
  footer,
}: {
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  onSubmit: () => void;
  footer: ReactNode;
}) {
  const handle = (e: FormEvent) => {
    e.preventDefault();
    onSubmit();
  };
  return (
    <Card title={title} description={description} flush>
      <form onSubmit={handle} noValidate>
        <div className="flex flex-col gap-4 p-4">{children}</div>
        <div className="flex flex-wrap items-center justify-end gap-2 border-t border-line bg-subtle/50 px-4 py-3">{footer}</div>
      </form>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Plain fields (text, URL, number, toggle)

type FieldSpec = {
  key: ScalarKey;
  label: MessageKey;
  hint?: string;
  placeholder?: string;
} & ({ kind: "text"; required?: boolean } | { kind: "url" } | { kind: "number"; min: number; integer?: boolean } | { kind: "toggle" });

type Draft = Record<string, string | boolean>;

function toDraft(s: SystemSettings, fields: FieldSpec[]): Draft {
  const d: Draft = {};
  for (const f of fields) {
    const v = s[f.key];
    d[f.key] = f.kind === "toggle" ? Boolean(v) : String(v ?? "");
  }
  return d;
}

function ScalarSection({
  data,
  save,
  title,
  description,
  fields,
}: {
  data: SystemSettingsResponse;
  save: SaveFn;
  title: string;
  description?: string;
  fields: FieldSpec[];
}) {
  const t = useT();
  const s = data.settings;
  const [draft, setDraft] = useState<Draft>(() => toDraft(s, fields));
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  // Re-sync only when this section's values change (saving another section keeps edits here).
  const snapshot = JSON.stringify(fields.map((f) => s[f.key]));
  useEffect(() => {
    setDraft(toDraft(s, fields));
    setErrors({});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [snapshot]);

  /** Parsed value of a field, or an error message. */
  const parse = (f: FieldSpec): { value: string | number | boolean } | { error: string } => {
    const raw = draft[f.key];
    switch (f.kind) {
      case "toggle":
        return { value: Boolean(raw) };
      case "text": {
        const v = String(raw).trim();
        if (f.required && !v) return { error: t("sysSettings.required") };
        return { value: v };
      }
      case "url": {
        const v = normalizeUrl(String(raw));
        if (v && !isHttpUrl(v)) return { error: t("setup.publicUrlInvalid") };
        return { value: v };
      }
      case "number": {
        const n = parseNumber(String(raw));
        const bad = n === null || Number.isNaN(n) || n < f.min || (f.integer && !Number.isInteger(n));
        if (bad) return { error: t(f.integer ? "sysSettings.integerInvalid" : "sysSettings.numberInvalid", { min: f.min }) };
        return { value: n };
      }
    }
  };

  const changed = (f: FieldSpec) => {
    const p = parse(f);
    return "error" in p || p.value !== s[f.key];
  };
  const dirty = fields.some(changed);

  const submit = async () => {
    const errs: Record<string, string> = {};
    const body: Record<string, string | number | boolean> = {};
    for (const f of fields) {
      const p = parse(f);
      if ("error" in p) errs[f.key] = p.error;
      else if (p.value !== s[f.key]) body[f.key] = p.value; // only what changed, so env-provided values stay env-provided
    }
    setErrors(errs);
    if (Object.keys(errs).length || !Object.keys(body).length) return;
    setBusy(true);
    await save(body as SystemSettingsInput, t("sysSettings.saved"));
    setBusy(false);
  };

  const reset = async (key: ScalarKey) => {
    setBusy(true);
    await save({ [key]: null } as SystemSettingsInput, t("sysSettings.resetDone"));
    setBusy(false);
  };

  return (
    <SectionCard
      title={title}
      description={description}
      onSubmit={() => void submit()}
      footer={
        <Button type="submit" variant="primary" size="sm" loading={busy} disabled={!dirty}>
          {t("common.save")}
        </Button>
      }
    >
      {fields.map((f) => {
        const note = <SourceNote source={data.sources[f.key]} onReset={() => void reset(f.key)} busy={busy} />;
        if (f.kind === "toggle") {
          return (
            <div key={f.key} className="flex items-start justify-between gap-4">
              <div className="min-w-0">
                <div className="text-[13px] font-medium">{t(f.label)}</div>
                {f.hint && <p className="mt-0.5 text-xs leading-relaxed text-muted">{f.hint}</p>}
                <div className="mt-1">{note}</div>
              </div>
              <Toggle
                checked={Boolean(draft[f.key])}
                onChange={(v) => setDraft((d) => ({ ...d, [f.key]: v }))}
                label={t(f.label)}
                disabled={busy}
              />
            </div>
          );
        }
        return (
          <div key={f.key} className="flex flex-col gap-1.5">
            <Input
              label={t(f.label)}
              hint={f.hint}
              placeholder={f.placeholder}
              error={errors[f.key]}
              value={String(draft[f.key])}
              onChange={(e) => {
                setDraft((d) => ({ ...d, [f.key]: e.target.value }));
                setErrors((x) => ({ ...x, [f.key]: "" }));
              }}
              disabled={busy}
              required={f.kind === "text" && f.required}
              spellCheck={f.kind === "text"}
              {...(f.kind === "number"
                ? { type: "number", min: f.min, step: f.integer ? 1 : "any", inputMode: f.integer ? ("numeric" as const) : ("decimal" as const) }
                : f.kind === "url"
                  ? { inputMode: "url" as const }
                  : { maxLength: 100 })}
            />
            {note}
          </div>
        );
      })}
    </SectionCard>
  );
}

// ---------------------------------------------------------------------------
// Sign-in providers

const PROVIDERS: OAuthProvider[] = ["github", "google"];

function isConfigured(s: SystemSettings, p: OAuthProvider): boolean {
  return s[p].client_id !== "" && s[p].client_secret_set;
}

function SignInSection({ data, save }: { data: SystemSettingsResponse; save: SaveFn }) {
  const t = useT();
  return (
    <Card title={t("sysSettings.signIn")} description={t("sysSettings.signInDesc")}>
      <div className="grid gap-4 lg:grid-cols-2">
        {PROVIDERS.map((p) => (
          <ProviderPanel key={p} provider={p} data={data} save={save} />
        ))}
      </div>
      <p className="mt-4 flex items-start gap-1.5 text-xs text-muted">
        <Icon name="info" className="mt-px size-3.5" />
        <span>{t("sysSettings.testHint")}</span>
      </p>
    </Card>
  );
}

function ProviderPanel({ provider: p, data, save }: { provider: OAuthProvider; data: SystemSettingsResponse; save: SaveFn }) {
  const t = useT();
  const confirm = useConfirm();
  const s = data.settings;
  const st = s[p];
  const name = OAUTH_LABELS[p];
  const other: OAuthProvider = p === "github" ? "google" : "github";
  const configured = isConfigured(s, p);
  // Turning this one off (or resetting it) with no other provider would lock everybody out.
  const isLast = configured && !isConfigured(s, other);

  const [clientId, setClientId] = useState(st.client_id);
  const [secret, setSecret] = useState("");
  const [errors, setErrors] = useState<{ client_id?: string; client_secret?: string }>({});
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setClientId(st.client_id);
    setErrors({});
  }, [st.client_id, st.client_secret_set]);

  const dirty = clientId.trim() !== st.client_id || secret.trim() !== "";

  const run = async (body: SystemSettingsInput, success: string) => {
    setBusy(true);
    const ok = await save(body, success);
    if (ok) setSecret("");
    setBusy(false);
  };

  const submit = () => {
    const id = clientId.trim();
    const errs: typeof errors = {};
    if (!id) errs.client_id = t("sysSettings.clientIdRequired");
    if (!st.client_secret_set && !secret.trim()) errs.client_secret = t("setup.clientSecretRequired");
    setErrors(errs);
    if (Object.keys(errs).length || !dirty) return;
    const body: { client_id?: string; client_secret?: string } = {};
    if (id !== st.client_id) body.client_id = id;
    if (secret.trim()) body.client_secret = secret.trim();
    void run({ [p]: body }, t("sysSettings.providerSaved", { provider: name }));
  };

  const disable = async () => {
    if (isLast) return;
    const ok = await confirm({
      title: t("sysSettings.disableTitle", { provider: name }),
      message: t("sysSettings.disableMessage", { provider: name }),
      confirmLabel: t("common.disable"),
      danger: true,
    });
    if (ok) await run({ [p]: { client_id: "" } }, t("sysSettings.providerDisabled", { provider: name }));
  };

  const reset = async () => {
    const ok = await confirm({
      title: t("sysSettings.resetProviderTitle", { provider: name }),
      message: t("sysSettings.resetProviderMessage", { provider: name }),
      confirmLabel: t("sysSettings.reset"),
      danger: true,
    });
    if (ok) await run({ [p]: { client_id: null } }, t("sysSettings.resetDone"));
  };

  const handle = (e: FormEvent) => {
    e.preventDefault();
    submit();
  };

  return (
    <form onSubmit={handle} noValidate className="flex min-w-0 flex-col rounded-lg border border-line">
      <div className="flex items-center gap-3 border-b border-line px-3 py-2.5">
        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-line bg-surface">
          <OAuthLogo provider={p} />
        </span>
        <div className="min-w-0 flex-1 text-sm font-medium">{name}</div>
        {configured ? (
          <Badge tone="success" dot>
            {t("common.enabled")}
          </Badge>
        ) : (
          <Badge>{t("sysSettings.providerNotSet")}</Badge>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-3 p-3">
        <SourceNote source={data.sources[p]} onReset={isLast ? undefined : () => void reset()} busy={busy} />
        <Input
          label={t("setup.clientId")}
          value={clientId}
          onChange={(e) => {
            setClientId(e.target.value);
            setErrors((x) => ({ ...x, client_id: undefined }));
          }}
          error={errors.client_id}
          autoComplete="off"
          spellCheck={false}
          disabled={busy}
        />
        <Input
          label={t("setup.clientSecret")}
          type="password"
          value={secret}
          onChange={(e) => {
            setSecret(e.target.value);
            setErrors((x) => ({ ...x, client_secret: undefined }));
          }}
          placeholder={st.client_secret_set ? t("sysSettings.secretSavedPlaceholder") : undefined}
          error={errors.client_secret}
          autoComplete="new-password"
          spellCheck={false}
          disabled={busy}
        />
        <CopyRow label={t("sysSettings.callbackUrl")} value={data.callback_urls?.[p] ?? ""} />
        {isLast && <Notice tone="warning">{t("sysSettings.lastProvider", { other: OAUTH_LABELS[other] })}</Notice>}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2 border-t border-line bg-subtle/50 px-3 py-2.5">
        {configured && (
          <Button
            variant="danger-ghost"
            size="sm"
            disabled={busy || isLast}
            title={isLast ? t("sysSettings.lastProvider", { other: OAUTH_LABELS[other] }) : undefined}
            onClick={() => void disable()}
          >
            {t("common.disable")}
          </Button>
        )}
        <Button type="submit" variant="primary" size="sm" loading={busy} disabled={!dirty}>
          {t("common.save")}
        </Button>
      </div>
    </form>
  );
}
