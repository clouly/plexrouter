import { Fragment, useEffect, useId, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "../api";
import { useT } from "../i18n";
import { useAsync, useDebounced } from "../lib/hooks";
import { formatDateTime, formatInt, formatLatency, formatUSD } from "../lib/format";
import { cx } from "../lib/cx";
import type { UsageLog } from "../types";
import { Card } from "./Card";
import { Badge } from "./Badge";
import { Button } from "./Button";
import { Icon } from "./Icon";
import { Pagination } from "./Pagination";
import { controlClass } from "./Input";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "./Table";

/**
 * Usage log table with model filter + pagination, shared by the user "Usage" page and the
 * admin "Logs" page (which adds a user column and a user_id filter).
 * Filters are mirrored into the URL (?model=&user_id=) so they can be linked to.
 */
export function LogsView({ admin = false }: { admin?: boolean }) {
  const t = useT();
  const listId = useId();
  const [params, setParams] = useSearchParams();
  const [model, setModel] = useState(params.get("model") ?? "");
  const [userId, setUserId] = useState(params.get("user_id") ?? "");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [expanded, setExpanded] = useState<number | null>(null);

  const dModel = useDebounced(model.trim());
  const dUser = useDebounced(userId.trim());
  const uid = /^\d+$/.test(dUser) && Number(dUser) > 0 ? Number(dUser) : undefined;
  const userInvalid = admin && userId.trim() !== "" && !/^\d+$/.test(userId.trim());

  // Keep the URL in sync with the (debounced) filters.
  useEffect(() => {
    const next = new URLSearchParams(params);
    dModel ? next.set("model", dModel) : next.delete("model");
    if (admin) uid ? next.set("user_id", String(uid)) : next.delete("user_id");
    if (next.toString() !== params.toString()) setParams(next, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dModel, uid, admin]);

  const logs = useAsync(
    () =>
      admin
        ? api.admin.logs({ page, page_size: pageSize, model: dModel || undefined, user_id: uid })
        : api.usageLogs({ page, page_size: pageSize, model: dModel || undefined }),
    [admin, page, pageSize, dModel, uid],
  );

  // Model suggestions for the filter (best effort; failures just mean no suggestions).
  const suggestions = useAsync<string[]>(
    () =>
      (admin ? api.admin.models().then((r) => r.data.map((m) => m.slug)) : api.models().then((r) => r.data.map((m) => m.id))).catch(
        () => [],
      ),
    [admin],
  );

  const rows = logs.data?.data ?? [];
  const total = logs.data?.total ?? 0;
  const cols = admin ? 11 : 10;

  const filters = (
    <>
      <div className="relative w-full sm:w-56">
        <input
          className={cx(controlClass, "h-8 text-[13px]")}
          placeholder={t("logs.filterModel")}
          aria-label={t("logs.filterModel")}
          value={model}
          list={listId}
          onChange={(e) => {
            setModel(e.target.value);
            setPage(1);
          }}
        />
        <datalist id={listId}>
          {(suggestions.data ?? []).map((s) => (
            <option key={s} value={s} />
          ))}
        </datalist>
      </div>
      {admin && (
        <input
          className={cx(controlClass, "h-8 w-full text-[13px] sm:w-32", userInvalid && "border-red-500")}
          placeholder={t("logs.filterUser")}
          aria-label={t("logs.filterUser")}
          inputMode="numeric"
          value={userId}
          onChange={(e) => {
            setUserId(e.target.value);
            setPage(1);
          }}
        />
      )}
      {(model || userId) && (
        <Button
          size="sm"
          variant="ghost"
          icon="x"
          onClick={() => {
            setModel("");
            setUserId("");
            setPage(1);
          }}
        >
          {t("common.clear")}
        </Button>
      )}
      <Button size="sm" icon="refresh" onClick={logs.reload} loading={logs.loading && !!logs.data}>
        {t("common.refresh")}
      </Button>
    </>
  );

  return (
    <Card flush title={t("logs.title")} description={t("logs.hint")} actions={filters}>
      <Table minWidth={admin ? 1080 : 940}>
        <THead>
          <Th>{t("logs.time")}</Th>
          {admin && <Th>{t("logs.user")}</Th>}
          <Th>{t("logs.model")}</Th>
          <Th>{t("logs.provider")}</Th>
          <Th>{t("logs.key")}</Th>
          <Th align="right">{t("logs.tokensIn")}</Th>
          <Th align="right">{t("logs.tokensOut")}</Th>
          <Th align="right">{t("logs.cost")}</Th>
          <Th align="right">{t("logs.latency")}</Th>
          <Th>{t("logs.status")}</Th>
          <Th className="w-8" />
        </THead>
        <TBody>
          <TableState
            colSpan={cols}
            loading={logs.loading}
            error={logs.error}
            empty={rows.length === 0}
            onRetry={logs.reload}
            emptyText={dModel || uid ? t("logs.emptyFiltered") : t("logs.empty")}
          />
          {!logs.error &&
            rows.map((l) => (
              <LogRow
                key={l.id}
                log={l}
                admin={admin}
                cols={cols}
                open={expanded === l.id}
                onToggle={() => setExpanded((x) => (x === l.id ? null : l.id))}
              />
            ))}
        </TBody>
      </Table>
      {total > 0 && !logs.error && (
        <Pagination
          page={page}
          pageSize={pageSize}
          total={total}
          onChange={setPage}
          onPageSizeChange={(n) => {
            setPageSize(n);
            setPage(1);
          }}
        />
      )}
    </Card>
  );
}

function LogRow({ log: l, admin, cols, open, onToggle }: { log: UsageLog; admin: boolean; cols: number; open: boolean; onToggle: () => void }) {
  const t = useT();
  const failed = l.status === "error";
  return (
    <Fragment>
      <Tr onClick={failed ? onToggle : undefined} className={cx(open && "bg-subtle/50")}>
        <Td className="whitespace-nowrap text-muted tabular-nums">{formatDateTime(l.created_at)}</Td>
        {admin && <Td className="max-w-48 truncate" title={l.user_email}>{l.user_email || "—"}</Td>}
        <Td>
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-xs whitespace-nowrap">{l.model}</span>
            {l.stream && <Badge>{t("logs.stream")}</Badge>}
          </div>
        </Td>
        <Td className="whitespace-nowrap">{l.provider || <span className="text-muted">—</span>}</Td>
        <Td className="max-w-40 truncate text-muted" title={l.api_key_name}>
          {l.api_key_name || "—"}
        </Td>
        <Td align="right">{formatInt(l.prompt_tokens)}</Td>
        <Td align="right">{formatInt(l.completion_tokens)}</Td>
        <Td align="right" className="font-medium whitespace-nowrap">
          {formatUSD(l.cost)}
        </Td>
        <Td align="right" className="whitespace-nowrap text-muted">
          {formatLatency(l.latency_ms)}
        </Td>
        <Td>
          {failed ? (
            <Badge tone="danger" dot title={l.error}>
              {t("logs.error")}
            </Badge>
          ) : (
            <Badge tone="success" dot>
              {t("logs.success")}
            </Badge>
          )}
        </Td>
        <Td className="text-muted">
          {failed && <Icon name="chevronDown" className={cx("size-3.5 transition-transform", open && "rotate-180")} />}
        </Td>
      </Tr>
      {failed && open && (
        <tr className="bg-subtle/50">
          <td colSpan={cols} className="px-4 pt-0 pb-3">
            <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 font-mono text-xs break-all whitespace-pre-wrap text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300">
              {l.error || t("logs.noErrorMessage")}
            </div>
          </td>
        </tr>
      )}
    </Fragment>
  );
}
