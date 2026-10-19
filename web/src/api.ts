import type {
  AdminModel,
  AdminStats,
  AdminUser,
  ApiKey,
  AppConfig,
  ModelInput,
  Paginated,
  Provider,
  ProviderInput,
  ProviderTestResult,
  PublicModel,
  RedeemCode,
  Role,
  Transaction,
  UsageLog,
  UsageStats,
  User,
} from "./types";

/** Error thrown for any non-2xx response. `message` comes from the body's `error.message`. */
export class ApiError extends Error {
  status: number;
  code: string;
  constructor(message: string, status: number, code = "") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

type Query = Record<string, string | number | undefined | null>;

function qs(query?: Query): string {
  if (!query) return "";
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === "") continue;
    p.set(k, String(v));
  }
  const s = p.toString();
  return s ? `?${s}` : "";
}

async function request<T>(method: string, path: string, body?: unknown, query?: Query): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path + qs(query), {
      method,
      credentials: "include",
      headers: body !== undefined ? { "Content-Type": "application/json", Accept: "application/json" } : { Accept: "application/json" },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (e) {
    throw new ApiError(e instanceof Error ? e.message : "Network error", 0, "network_error");
  }
  const text = await res.text();
  let data: unknown = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    const err = (data as { error?: { message?: string; code?: string } } | null)?.error;
    throw new ApiError(err?.message || `${res.status} ${res.statusText}`.trim(), res.status, err?.code ?? "");
  }
  return data as T;
}

const get = <T>(path: string, query?: Query) => request<T>("GET", path, undefined, query);
const post = <T>(path: string, body?: unknown) => request<T>("POST", path, body ?? {});
const patch = <T>(path: string, body: unknown) => request<T>("PATCH", path, body);
const del = <T>(path: string) => request<T>("DELETE", path);

type Ok = { ok: true };
export type PageQuery = { page: number; page_size: number };

export const api = {
  // Public
  config: () => get<AppConfig>("/api/config"),
  models: () => get<{ data: PublicModel[] }>("/api/models"),

  // Auth
  register: (body: { email: string; password: string; name?: string }) => post<{ user: User }>("/api/auth/register", body),
  login: (body: { email: string; password: string }) => post<{ user: User }>("/api/auth/login", body),
  logout: () => post<Ok>("/api/auth/logout"),
  me: () => get<{ user: User }>("/api/me"),
  changePassword: (body: { old_password: string; new_password: string }) => post<Ok>("/api/me/password", body),

  // Keys
  keys: () => get<{ data: ApiKey[] }>("/api/keys"),
  createKey: (body: { name: string; credit_limit?: number | null }) => post<{ key: ApiKey; secret: string }>("/api/keys", body),
  updateKey: (id: number, body: { name?: string; credit_limit?: number | null; disabled?: boolean }) =>
    patch<{ key: ApiKey }>(`/api/keys/${id}`, body),
  deleteKey: (id: number) => del<Ok>(`/api/keys/${id}`),

  // Usage & credits
  usageLogs: (q: PageQuery & { model?: string }) => get<Paginated<UsageLog>>("/api/usage/logs", q),
  usageStats: (days: number) => get<UsageStats>("/api/usage/stats", { days }),
  transactions: (q: PageQuery) => get<Paginated<Transaction>>("/api/credits/transactions", q),
  redeem: (code: string) => post<{ amount: number; balance: number }>("/api/credits/redeem", { code }),

  // Admin
  admin: {
    stats: () => get<AdminStats>("/api/admin/stats"),
    providers: () => get<{ data: Provider[] }>("/api/admin/providers"),
    createProvider: (body: ProviderInput) => post<{ provider: Provider }>("/api/admin/providers", body),
    updateProvider: (id: number, body: Partial<ProviderInput>) => patch<{ provider: Provider }>(`/api/admin/providers/${id}`, body),
    deleteProvider: (id: number) => del<Ok>(`/api/admin/providers/${id}`),
    testProvider: (id: number) => post<ProviderTestResult>(`/api/admin/providers/${id}/test`),
    upstreamModels: (id: number) => get<{ data: { id: string }[] }>(`/api/admin/providers/${id}/upstream-models`),
    models: () => get<{ data: AdminModel[] }>("/api/admin/models"),
    createModel: (body: ModelInput) => post<{ model: AdminModel }>("/api/admin/models", body),
    updateModel: (id: number, body: Partial<ModelInput>) => patch<{ model: AdminModel }>(`/api/admin/models/${id}`, body),
    deleteModel: (id: number) => del<Ok>(`/api/admin/models/${id}`),
    importModels: (body: { provider_id: number; upstream_models: string[]; slug_prefix: string }) =>
      post<{ created: number; skipped: number }>("/api/admin/models/import", body),
    users: (q: PageQuery & { q?: string }) => get<Paginated<AdminUser>>("/api/admin/users", q),
    updateUser: (id: number, body: { name?: string; role?: Role; disabled?: boolean }) => patch<{ user: User }>(`/api/admin/users/${id}`, body),
    adjustCredits: (id: number, body: { amount: number; note?: string }) => post<{ user: User }>(`/api/admin/users/${id}/credits`, body),
    logs: (q: PageQuery & { user_id?: number; model?: string }) => get<Paginated<UsageLog>>("/api/admin/logs", q),
    redeemCodes: (q: PageQuery) => get<Paginated<RedeemCode>>("/api/admin/redeem-codes", q),
    createRedeemCodes: (body: { amount: number; count: number }) => post<{ codes: string[] }>("/api/admin/redeem-codes", body),
    deleteRedeemCode: (id: number) => del<Ok>(`/api/admin/redeem-codes/${id}`),
  },
};

export function errorMessage(e: unknown): string {
  if (e instanceof Error) return e.message;
  return String(e);
}
