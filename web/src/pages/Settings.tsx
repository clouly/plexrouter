import { useState, type FormEvent } from "react";
import { api, errorMessage } from "../api";
import { useAuth } from "../auth";
import { useI18n, type Lang } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { formatDate } from "../lib/format";
import { Card, Notice, PageHeader } from "../components/Card";
import { Badge } from "../components/Badge";
import { Button } from "../components/Button";
import { Input, Select } from "../components/Input";
import { useToast } from "../components/Toast";

export function SettingsPage() {
  const { t, lang, setLang } = useI18n();
  const { user, config } = useAuth();
  useDocumentTitle(t("nav.settings"), config?.name);

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

        <PasswordCard />

        <Card title={t("settings.preferences")}>
          <Select label={t("header.language")} value={lang} onChange={(e) => setLang(e.target.value as Lang)} wrapperClassName="max-w-xs">
            <option value="en">English</option>
            <option value="zh-CN">简体中文</option>
          </Select>
        </Card>
      </div>
    </>
  );
}

function PasswordCard() {
  const { t } = useI18n();
  const toast = useToast();
  const [oldPw, setOldPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!oldPw || !newPw) return setError(t("auth.fillAll"));
    if (newPw.length < 8) return setError(t("auth.passwordTooShort"));
    if (newPw !== confirm) return setError(t("auth.passwordMismatch"));
    setBusy(true);
    setError("");
    try {
      await api.changePassword({ old_password: oldPw, new_password: newPw });
      toast.success(t("settings.passwordChanged"));
      setOldPw("");
      setNewPw("");
      setConfirm("");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card title={t("settings.changePassword")} description={t("settings.changePasswordDesc")}>
      <form onSubmit={submit} className="flex max-w-sm flex-col gap-4" noValidate>
        {error && <Notice tone="danger">{error}</Notice>}
        <Input label={t("settings.currentPassword")} type="password" autoComplete="current-password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} disabled={busy} />
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
            {t("settings.updatePassword")}
          </Button>
        </div>
      </form>
    </Card>
  );
}
