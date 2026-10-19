import { errorMessage } from "../api";
import type { ReactNode } from "react";
import { useT } from "../i18n";
import { Button } from "./Button";
import { Icon, type IconName } from "./Icon";
import { Spinner } from "./Spinner";

export function EmptyState({ icon = "info", title, description, action }: { icon?: IconName; title: ReactNode; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-4 py-12 text-center">
      <div className="flex size-10 items-center justify-center rounded-full bg-subtle text-muted">
        <Icon name={icon} className="size-5" />
      </div>
      <div className="text-sm font-medium">{title}</div>
      {description && <div className="max-w-sm text-[13px] text-muted">{description}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}

export function LoadingState({ label }: { label?: string }) {
  const t = useT();
  return (
    <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted">
      <Spinner /> {label ?? t("common.loading")}
    </div>
  );
}

export function ErrorState({ error, onRetry }: { error: Error; onRetry?: () => void }) {
  const t = useT();
  return (
    <EmptyState
      icon="alert"
      title={t("common.errorTitle")}
      description={<span className="text-red-600 dark:text-red-400">{errorMessage(error)}</span>}
      action={
        onRetry && (
          <Button size="sm" icon="refresh" onClick={onRetry}>
            {t("common.retry")}
          </Button>
        )
      }
    />
  );
}
