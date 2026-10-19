import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, errorMessage } from "../../api";
import { useAuth } from "../../auth";
import { useT } from "../../i18n";
import { useAsync, useDebounced, useDocumentTitle } from "../../lib/hooks";
import { formatDate, formatInt, formatUSD, parseNumber } from "../../lib/format";
import { cx } from "../../lib/cx";
import type { AdminUser, User } from "../../types";
import { Card, PageHeader } from "../../components/Card";
import { Badge } from "../../components/Badge";
import { Button } from "../../components/Button";
import { Dialog, useConfirm } from "../../components/Dialog";
import { Input, SearchInput } from "../../components/Input";
import { Pagination } from "../../components/Pagination";
import { useToast } from "../../components/Toast";
import { Table, TBody, THead, Td, Th, TableState, Tr } from "../../components/Table";

export function AdminUsersPage() {
  const t = useT();
  const toast = useToast();
  const confirm = useConfirm();
  const { user: me, config, setUser: setMe } = useAuth();
  useDocumentTitle(t("nav.adminUsers"), config?.name);
  const [q, setQ] = useState("");
  const dq = useDebounced(q.trim());
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const users = useAsync(() => api.admin.users({ page, page_size: pageSize, q: dq || undefined }), [page, pageSize, dq]);
  const [busyId, setBusyId] = useState<number | null>(null);
  const [crediting, setCrediting] = useState<AdminUser | null>(null);

  const rows = users.data?.data ?? [];
  const total = users.data?.total ?? 0;

  // The PATCH/credits endpoints return a plain User; keep the aggregate columns we already have.
  const merge = (u: User) => {
    users.setData((d) => d && { ...d, data: d.data.map((x) => (x.id === u.id ? { ...x, ...u } : x)) });
    if (me && u.id === me.id) setMe({ ...me, ...u });
  };

  const update = async (u: AdminUser, body: { role?: "admin" | "user"; disabled?: boolean }, success: string) => {
    setBusyId(u.id);
    try {
      merge((await api.admin.updateUser(u.id, body)).user);
      toast.success(success);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusyId(null);
    }
  };

  const changeRole = async (u: AdminUser) => {
    const role = u.role === "admin" ? "user" : "admin";
    const ok = await confirm({
      title: role === "admin" ? t("users.makeAdminTitle") : t("users.makeUserTitle"),
      message: role === "admin" ? t("users.makeAdminMessage", { email: u.email }) : t("users.makeUserMessage", { email: u.email }),
      confirmLabel: role === "admin" ? t("users.makeAdmin") : t("users.makeUser"),
      danger: role === "admin",
    });
    if (ok) await update(u, { role }, t("users.roleChanged"));
  };

  const toggleDisabled = async (u: AdminUser) => {
    if (!u.disabled) {
      const ok = await confirm({
        title: t("users.disableTitle"),
        message: t("users.disableMessage", { email: u.email }),
        confirmLabel: t("common.disable"),
        danger: true,
      });
      if (!ok) return;
    }
    await update(u, { disabled: !u.disabled }, u.disabled ? t("users.enabledToast") : t("users.disabledToast"));
  };

  return (
    <>
      <PageHeader title={t("users.title")} description={t("users.desc")} />
      <Card
        flush
        title={t("users.count", { n: total })}
        actions={
          <>
            <SearchInput
              value={q}
              onChange={(v) => {
                setQ(v);
                setPage(1);
              }}
              placeholder={t("users.search")}
              className="w-full sm:w-64"
            />
            <Button size="sm" icon="refresh" onClick={users.reload} loading={users.loading && !!users.data}>
              {t("common.refresh")}
            </Button>
          </>
        }
      >
        <Table stickyLast minWidth={1040}>
          <THead>
            <Th>{t("users.user")}</Th>
            <Th>{t("settings.role")}</Th>
            <Th align="right">{t("users.balance")}</Th>
            <Th align="right">{t("users.totalCost")}</Th>
            <Th align="right">{t("dashboard.requests")}</Th>
            <Th>{t("common.status")}</Th>
            <Th>{t("users.joined")}</Th>
            <Th align="right">{t("common.actions")}</Th>
          </THead>
          <TBody>
            <TableState
              colSpan={8}
              loading={users.loading}
              error={users.error}
              empty={rows.length === 0}
              onRetry={users.reload}
              emptyText={dq ? t("common.noMatch") : undefined}
            />
            {!users.error &&
              rows.map((u) => {
                const self = me?.id === u.id;
                const busy = busyId === u.id;
                return (
                  <Tr key={u.id} className={cx(u.disabled && "opacity-70")}>
                    <Td>
                      <div className="flex items-center gap-1.5">
                        <span className="max-w-64 truncate font-medium">{u.name || u.email}</span>
                        {self && <Badge tone="accent">{t("users.you")}</Badge>}
                      </div>
                      <div className="max-w-72 truncate text-xs text-muted">
                        {u.name ? `${u.email} · ` : ""}#{u.id}
                      </div>
                    </Td>
                    <Td>
                      <Badge tone={u.role === "admin" ? "accent" : "neutral"}>{u.role === "admin" ? t("role.admin") : t("role.user")}</Badge>
                    </Td>
                    <Td align="right" className="font-medium">
                      {formatUSD(u.balance)}
                    </Td>
                    <Td align="right">{formatUSD(u.total_cost)}</Td>
                    <Td align="right">{formatInt(u.requests)}</Td>
                    <Td>
                      <Badge tone={u.disabled ? "danger" : "success"} dot>
                        {u.disabled ? t("common.disabled") : t("common.active")}
                      </Badge>
                    </Td>
                    <Td className="whitespace-nowrap text-muted">{formatDate(u.created_at)}</Td>
                    <Td align="right">
                      <div className="flex justify-end gap-1">
                        <Button size="xs" icon="wallet" onClick={() => setCrediting(u)} disabled={busy}>
                          {t("users.credits")}
                        </Button>
                        <Button size="xs" onClick={() => changeRole(u)} disabled={busy || self} title={self ? t("users.selfHint") : undefined}>
                          {u.role === "admin" ? t("users.makeUser") : t("users.makeAdmin")}
                        </Button>
                        <Button
                          size="xs"
                          variant={u.disabled ? "secondary" : "danger-ghost"}
                          onClick={() => toggleDisabled(u)}
                          disabled={busy || self}
                          title={self ? t("users.selfHint") : undefined}
                        >
                          {u.disabled ? t("common.enable") : t("common.disable")}
                        </Button>
                        <Link
                          to={`/admin/logs?user_id=${u.id}`}
                          className="inline-flex h-7 items-center rounded-md px-2 text-xs font-medium text-muted hover:bg-subtle hover:text-fg"
                        >
                          {t("users.logs")}
                        </Link>
                      </div>
                    </Td>
                  </Tr>
                );
              })}
          </TBody>
        </Table>
        {total > 0 && !users.error && (
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

      <CreditsDialog
        user={crediting}
        onClose={() => setCrediting(null)}
        onDone={(u) => {
          merge(u);
          setCrediting(null);
        }}
      />
    </>
  );
}

function CreditsDialog({ user, onClose, onDone }: { user: AdminUser | null; onClose: () => void; onDone: (u: User) => void }) {
  const t = useT();
  const toast = useToast();
  const [amount, setAmount] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!user) return;
    setAmount("");
    setNote("");
    setError("");
  }, [user]);

  const n = parseNumber(amount);
  const valid = n !== null && !Number.isNaN(n) && n !== 0;

  const submit = async () => {
    if (!user) return;
    if (!valid) {
      setError(t("users.amountInvalid"));
      return;
    }
    setError("");
    setBusy(true);
    try {
      const r = await api.admin.adjustCredits(user.id, { amount: n, note: note.trim() || undefined });
      toast.success(t("users.creditsAdjusted", { amount: formatUSD(n, { sign: true }), email: user.email, balance: formatUSD(r.user.balance) }));
      onDone(r.user);
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog
      open={!!user}
      onClose={onClose}
      dismissible={!busy}
      onSubmit={submit}
      title={t("users.adjustTitle")}
      description={user?.email}
      footer={
        <>
          <Button onClick={onClose} disabled={busy}>
            {t("common.cancel")}
          </Button>
          <Button type="submit" variant="primary" loading={busy} disabled={!valid}>
            {t("users.apply")}
          </Button>
        </>
      }
    >
      {user && (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3 text-[13px]">
            <div className="rounded-lg border border-line p-3">
              <div className="text-xs text-muted">{t("users.currentBalance")}</div>
              <div className="mt-1 text-lg font-semibold tabular-nums">{formatUSD(user.balance)}</div>
            </div>
            <div className="rounded-lg border border-line p-3">
              <div className="text-xs text-muted">{t("users.newBalance")}</div>
              <div className={cx("mt-1 text-lg font-semibold tabular-nums", valid && user.balance + n < 0 && "text-red-600 dark:text-red-400")}>
                {valid ? formatUSD(user.balance + n) : "—"}
              </div>
            </div>
          </div>
          <Input
            label={t("users.amount")}
            type="number"
            step="any"
            inputMode="decimal"
            placeholder="10.00"
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            error={error || undefined}
            hint={t("users.amountHint")}
            disabled={busy}
            required
          />
          <Input label={t("users.note")} placeholder={t("users.notePlaceholder")} value={note} onChange={(e) => setNote(e.target.value)} disabled={busy} maxLength={200} />
        </div>
      )}
    </Dialog>
  );
}
