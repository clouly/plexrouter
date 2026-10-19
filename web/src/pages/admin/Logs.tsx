import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useDocumentTitle } from "../../lib/hooks";
import { PageHeader } from "../../components/Card";
import { LogsView } from "../../components/LogsView";

export function AdminLogsPage() {
  const t = useT();
  const { config } = useAuth();
  useDocumentTitle(t("nav.adminLogs"), config?.name);
  return (
    <>
      <PageHeader title={t("adminLogs.title")} description={t("adminLogs.desc")} />
      <LogsView admin />
    </>
  );
}
