import { useAuth } from "../auth";
import { useT } from "../i18n";
import { useDocumentTitle } from "../lib/hooks";
import { PageHeader } from "../components/Card";
import { LogsView } from "../components/LogsView";

export function UsagePage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("nav.usage"), config?.name);
  return (
    <>
      <PageHeader title={t("usage.title")} description={t("usage.desc")} />
      <LogsView />
    </>
  );
}
