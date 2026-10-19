import type { ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useAuth } from "./auth";
import { useT } from "./i18n";
import { Layout } from "./layout/Layout";
import { Spinner } from "./components/Spinner";
import { EmptyState } from "./components/EmptyState";
import { LoginPage } from "./pages/Auth";
import { SetupPage } from "./pages/Setup";
import { DashboardPage } from "./pages/Dashboard";
import { KeysPage } from "./pages/Keys";
import { ModelsPage } from "./pages/Models";
import { UsagePage } from "./pages/Usage";
import { CreditsPage } from "./pages/Credits";
import { DocsPage } from "./pages/Docs";
import { SettingsPage } from "./pages/Settings";
import { NotFoundPage } from "./pages/NotFound";
import { AdminOverviewPage } from "./pages/admin/Overview";
import { AdminProvidersPage } from "./pages/admin/Providers";
import { AdminReportsPage } from "./pages/admin/Reports";
import { AdminModelsPage } from "./pages/admin/Models";
import { AdminUsersPage } from "./pages/admin/Users";
import { AdminLogsPage } from "./pages/admin/Logs";
import { AdminRedeemCodesPage } from "./pages/admin/RedeemCodes";
import { AdminSystemSettingsPage } from "./pages/admin/SystemSettings";

function FullPageSpinner() {
  return (
    <div className="flex min-h-[50vh] items-center justify-center text-muted">
      <Spinner className="size-5" />
    </div>
  );
}

function RequireAuth({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const location = useLocation();
  if (user === undefined) return <FullPageSpinner />;
  if (user === null) return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />;
  return <>{children}</>;
}

function RequireAdmin({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const t = useT();
  if (user === undefined) return <FullPageSpinner />;
  if (user === null) return <Navigate to="/login" replace />;
  if (user.role !== "admin") return <EmptyState icon="alert" title={t("common.forbidden")} description={t("common.forbiddenDesc")} />;
  return <>{children}</>;
}

function GuestOnly({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const location = useLocation();
  if (user === undefined) return <FullPageSpinner />;
  if (user) {
    const from = (location.state as { from?: string } | null)?.from;
    return <Navigate to={from && from.startsWith("/") ? from : "/"} replace />;
  }
  return <>{children}</>;
}

/** Paths reachable while the instance still needs its first-run setup. */
function setupExempt(path: string): boolean {
  // /login handles setup_required itself (it keeps a failed first sign-in's error on screen).
  return ["/setup", "/login", "/models", "/docs"].some((p) => path === p || path.startsWith(p + "/"));
}

/** Until an admin exists (config.setup_required), everything but a few public pages goes to /setup. */
function SetupGate({ children }: { children: ReactNode }) {
  const { config } = useAuth();
  const location = useLocation();
  if (setupExempt(location.pathname)) return <>{children}</>;
  if (!config) return <FullPageSpinner />;
  if (config.setup_required) return <Navigate to="/setup" replace />;
  return <>{children}</>;
}

/** /setup is only for a fresh instance; afterwards it goes to the login page (or home when signed in). */
function SetupOnly({ children }: { children: ReactNode }) {
  const { config, user } = useAuth();
  if (!config) return <FullPageSpinner />;
  if (config.setup_required) return <>{children}</>;
  if (user === undefined) return <FullPageSpinner />;
  return <Navigate to={user ? "/" : "/login"} replace />;
}

const auth = (el: ReactNode) => <RequireAuth>{el}</RequireAuth>;
const admin = (el: ReactNode) => <RequireAdmin>{el}</RequireAdmin>;

export function App() {
  return (
    <SetupGate>
    <Routes>
      <Route path="/setup" element={<SetupOnly><SetupPage /></SetupOnly>} />
      <Route path="/login" element={<GuestOnly><LoginPage /></GuestOnly>} />
      <Route path="/register" element={<Navigate to="/login" replace />} />
      <Route element={<Layout />}>
        <Route index element={auth(<DashboardPage />)} />
        <Route path="keys" element={auth(<KeysPage />)} />
        <Route path="models" element={<ModelsPage />} />
        <Route path="usage" element={auth(<UsagePage />)} />
        <Route path="credits" element={auth(<CreditsPage />)} />
        <Route path="docs" element={<DocsPage />} />
        <Route path="settings" element={auth(<SettingsPage />)} />
        <Route path="admin">
          <Route index element={admin(<AdminOverviewPage />)} />
          <Route path="reports" element={admin(<AdminReportsPage />)} />
          <Route path="providers" element={admin(<AdminProvidersPage />)} />
          <Route path="models" element={admin(<AdminModelsPage />)} />
          <Route path="users" element={admin(<AdminUsersPage />)} />
          <Route path="logs" element={admin(<AdminLogsPage />)} />
          <Route path="redeem-codes" element={admin(<AdminRedeemCodesPage />)} />
          <Route path="settings" element={admin(<AdminSystemSettingsPage />)} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
    </SetupGate>
  );
}
