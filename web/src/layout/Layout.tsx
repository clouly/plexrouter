import { useEffect, useRef, useState } from "react";
import { Link, NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { useAuth, useAppName } from "../auth";
import { useI18n, type MessageKey } from "../i18n";
import { useTheme } from "../theme";
import { cx } from "../lib/cx";
import { formatUSD } from "../lib/format";
import { Icon, type IconName } from "../components/Icon";
import { IconButton } from "../components/Button";
import { useToast } from "../components/Toast";
import { LanguageMenu } from "../components/LanguageMenu";

interface NavItem {
  to: string;
  label: MessageKey;
  icon: IconName;
  end?: boolean;
  auth?: boolean; // requires login
}

const userNav: NavItem[] = [
  { to: "/", label: "nav.dashboard", icon: "dashboard", end: true, auth: true },
  { to: "/keys", label: "nav.keys", icon: "key", auth: true },
  { to: "/models", label: "nav.models", icon: "cube" },
  { to: "/usage", label: "nav.usage", icon: "chart", auth: true },
  { to: "/credits", label: "nav.credits", icon: "wallet", auth: true },
  { to: "/docs", label: "nav.docs", icon: "book" },
  { to: "/settings", label: "nav.settings", icon: "settings", auth: true },
];

const adminNav: NavItem[] = [
  { to: "/admin", label: "nav.adminOverview", icon: "overview", end: true },
  { to: "/admin/reports", label: "nav.adminReports", icon: "trending" },
  { to: "/admin/providers", label: "nav.adminProviders", icon: "server" },
  { to: "/admin/models", label: "nav.adminModels", icon: "cube" },
  { to: "/admin/users", label: "nav.adminUsers", icon: "users" },
  { to: "/admin/logs", label: "nav.adminLogs", icon: "list" },
  { to: "/admin/redeem-codes", label: "nav.adminRedeem", icon: "ticket" },
];

function Logo({ name }: { name: string }) {
  return (
    <Link to="/" className="flex min-w-0 items-center gap-2 font-semibold tracking-tight">
      <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-accent text-accent-fg">
        <Icon name="zap" className="size-4" strokeWidth={2.2} />
      </span>
      <span className="truncate">{name}</span>
    </Link>
  );
}

function NavSection({ title, items, onNavigate }: { title?: string; items: NavItem[]; onNavigate?: () => void }) {
  const { t } = useI18n();
  return (
    <div className="flex flex-col gap-0.5">
      {title && <div className="px-2.5 pt-4 pb-1.5 text-[11px] font-semibold tracking-wider text-muted uppercase">{title}</div>}
      {items.map((it) => (
        <NavLink
          key={it.to}
          to={it.to}
          end={it.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cx(
              "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-[13px] font-medium transition-colors",
              isActive ? "bg-accent-soft text-accent" : "text-muted hover:bg-subtle hover:text-fg",
            )
          }
        >
          <Icon name={it.icon} />
          {t(it.label)}
        </NavLink>
      ))}
    </div>
  );
}

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const { t } = useI18n();
  const { user, config } = useAuth();
  const items = user ? userNav : userNav.filter((i) => !i.auth);
  return (
    <div className="flex h-full flex-col">
      <nav className="flex-1 overflow-y-auto px-3 py-3">
        <NavSection items={items} onNavigate={onNavigate} />
        {user?.role === "admin" && <NavSection title={t("nav.admin")} items={adminNav} onNavigate={onNavigate} />}
        {!user && (
          <div className="mt-4 flex flex-col gap-2 px-1">
            <Link to="/login" onClick={onNavigate} className="inline-flex h-9 items-center justify-center rounded-lg bg-accent text-sm font-medium text-accent-fg hover:bg-accent-hover">
              {t("auth.login")}
            </Link>
          </div>
        )}
      </nav>
      <div className="border-t border-line px-4 py-3 text-[11px] text-muted">
        <span>{t("footer.poweredBy")}</span>
        {config?.version && <span> · v{config.version.replace(/^v/, "")}</span>}
      </div>
    </div>
  );
}

export function ThemeToggle() {
  const { dark, toggle } = useTheme();
  const { t } = useI18n();
  return <IconButton icon={dark ? "sun" : "moon"} label={dark ? t("header.lightMode") : t("header.darkMode")} onClick={toggle} />;
}

function UserMenu() {
  const { user, logout } = useAuth();
  const { t } = useI18n();
  const toast = useToast();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  if (!user) {
    return (
      <Link to="/login" className="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent px-3 text-xs font-medium text-accent-fg hover:bg-accent-hover">
        <Icon name="login" className="size-3.5" />
        {t("auth.login")}
      </Link>
    );
  }

  const initial = (user.name || user.email).trim().charAt(0).toUpperCase() || "?";

  const doLogout = async () => {
    setOpen(false);
    try {
      await logout();
    } catch {
      toast.error(t("auth.logoutFailed"));
    }
    navigate("/login");
  };

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex h-8 items-center gap-2 rounded-md pr-1.5 pl-1 hover:bg-subtle"
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <span className="flex size-6 items-center justify-center rounded-full bg-accent-soft text-xs font-semibold text-accent">{initial}</span>
        <span className="hidden text-xs font-medium tabular-nums sm:inline">{formatUSD(user.balance)}</span>
        <Icon name="chevronDown" className="size-3.5 text-muted" />
      </button>
      {open && (
        <div role="menu" className="animate-pop absolute right-0 z-40 mt-1.5 w-64 overflow-hidden rounded-lg border border-line bg-surface shadow-lg">
          <div className="border-b border-line px-3 py-2.5">
            <div className="truncate text-sm font-medium">{user.name || user.email}</div>
            {user.name && <div className="truncate text-xs text-muted">{user.email}</div>}
          </div>
          <Link to="/credits" onClick={() => setOpen(false)} className="flex items-center justify-between px-3 py-2.5 hover:bg-subtle" role="menuitem">
            <span className="text-xs text-muted">{t("header.balance")}</span>
            <span className="font-semibold tabular-nums">{formatUSD(user.balance)}</span>
          </Link>
          <div className="border-t border-line py-1">
            <Link to="/settings" onClick={() => setOpen(false)} className="flex items-center gap-2 px-3 py-1.5 text-[13px] hover:bg-subtle" role="menuitem">
              <Icon name="settings" className="size-3.5 text-muted" /> {t("nav.settings")}
            </Link>
            <button type="button" onClick={doLogout} className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-[13px] hover:bg-subtle" role="menuitem">
              <Icon name="logout" className="size-3.5 text-muted" /> {t("auth.logout")}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export function Layout() {
  const name = useAppName();
  const { t } = useI18n();
  const [drawer, setDrawer] = useState(false);
  const location = useLocation();

  useEffect(() => setDrawer(false), [location.pathname]);
  useEffect(() => {
    if (!drawer) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setDrawer(false);
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawer]);

  return (
    <div className="min-h-dvh">
      {/* Desktop sidebar */}
      <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 flex-col border-r border-line bg-surface lg:flex">
        <div className="flex h-14 shrink-0 items-center border-b border-line px-4">
          <Logo name={name} />
        </div>
        <SidebarContent />
      </aside>

      {/* Mobile drawer */}
      {drawer && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div className="animate-fade absolute inset-0 bg-black/50" onClick={() => setDrawer(false)} />
          <aside className="animate-pop absolute inset-y-0 left-0 flex w-64 max-w-[85vw] flex-col border-r border-line bg-surface shadow-xl">
            <div className="flex h-14 shrink-0 items-center justify-between border-b border-line px-4">
              <Logo name={name} />
              <IconButton icon="x" label={t("common.close")} onClick={() => setDrawer(false)} />
            </div>
            <SidebarContent onNavigate={() => setDrawer(false)} />
          </aside>
        </div>
      )}

      <div className="lg:pl-60">
        <header className="sticky top-0 z-20 flex h-14 items-center gap-2 border-b border-line bg-page/85 px-3 backdrop-blur sm:px-6">
          <IconButton icon="menu" label={t("header.menu")} onClick={() => setDrawer(true)} className="lg:hidden" />
          <div className="min-w-0 lg:hidden">
            <Logo name={name} />
          </div>
          <div className="flex-1" />
          <LanguageMenu />
          <ThemeToggle />
          <UserMenu />
        </header>
        <main className="mx-auto w-full max-w-7xl px-4 py-6 sm:px-6 lg:py-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
