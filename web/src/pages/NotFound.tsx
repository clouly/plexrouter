import { Link } from "react-router-dom";
import { useT } from "../i18n";
import { EmptyState } from "../components/EmptyState";

export function NotFoundPage() {
  const t = useT();
  return (
    <EmptyState
      icon="alert"
      title={t("notFound.title")}
      description={t("notFound.desc")}
      action={
        <Link to="/" className="text-sm font-medium text-accent hover:underline">
          {t("notFound.back")}
        </Link>
      }
    />
  );
}
