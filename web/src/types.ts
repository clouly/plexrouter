// Types mirroring docs/console-api.md. Keep in sync with the backend contract.

export type Role = "admin" | "user";

export interface User {
  id: number;
  email: string;
  name: string;
  role: Role;
  balance: number; // USD
  disabled: boolean;
  /** Requests/minute across all the user's keys; null = server default. */
  rate_limit_rpm: number | null;
  created_at: string;
}

export interface AdminUser extends User {
  total_cost: number;
  requests: number;
}

export interface ApiKey {
  id: number;
  name: string;
  prefix: string;
  credit_limit: number | null; // USD, null = unlimited
  usage: number; // USD
  /** Requests/minute for this key; null = no key-level limit. */
  rate_limit_rpm: number | null;
  /** Model slugs this key may call; [] = all models. */
  allowed_models: string[];
  disabled: boolean;
  created_at: string;
  last_used_at: string | null;
}

export interface PublicModel {
  id: string; // slug
  name: string;
  description: string;
  context_length: number;
  input_price: number; // USD / 1M prompt tokens
  output_price: number; // USD / 1M completion tokens
  /** USD / 1M cached prompt tokens read; null = charged as input. */
  cache_read_price: number | null;
  /** USD / 1M prompt tokens written to cache; null = charged as input. */
  cache_write_price: number | null;
  providers: number;
}

export interface UsageLog {
  id: number;
  model: string;
  provider: string;
  api_key_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  /** Included in prompt_tokens. */
  cache_read_tokens: number;
  cache_write_tokens: number;
  /** Which gateway endpoint was called. */
  api: GatewayApi;
  cost: number;
  latency_ms: number;
  status: "success" | "error";
  error: string;
  stream: boolean;
  created_at: string;
  user_email?: string;
  /** Admin only: USD paid to the provider; null = endpoint has no cost configured. */
  upstream_cost?: number | null;
  /** Admin only: name of the provider key used ("" if none). */
  provider_key?: string;
}

export type GatewayApi = "chat" | "messages" | "embeddings";

export type TransactionType = "signup" | "redeem" | "adjust";

export interface Transaction {
  id: number;
  amount: number;
  type: TransactionType;
  note: string;
  created_at: string;
}

export type ProviderType = "openai" | "anthropic" | "gemini" | "openai_compatible";

export const PROVIDER_TYPES: ProviderType[] = ["openai", "anthropic", "gemini", "openai_compatible"];

export interface Provider {
  id: number;
  name: string;
  type: ProviderType;
  base_url: string;
  enabled: boolean;
  keys_total: number;
  /** Keys currently usable (enabled, not failed, not cooling down). */
  keys_active: number;
  health: ProviderHealth;
  created_at: string;
}

export type ProviderHealthStatus = "healthy" | "degraded" | "down" | "idle" | "no_keys";

/** Computed by the server from the last 24 h of requests. */
export interface ProviderHealth {
  requests_24h: number;
  success_rate_24h: number | null; // 0..1, null when no requests
  avg_latency_ms_24h: number | null;
  status: ProviderHealthStatus;
}

export type ProviderKeyStatus = "active" | "auth_failed" | "quota_exceeded" | "cooling_down" | "disabled";

/** One upstream API key of a provider; requests rotate across usable keys. */
export interface ProviderKey {
  id: number;
  provider_id: number;
  name: string;
  key_hint: string;
  enabled: boolean;
  status: ProviderKeyStatus;
  last_error: string;
  last_error_at: string | null;
  cooldown_until: string | null;
  last_used_at: string | null;
  requests_24h: number;
  errors_24h: number;
  created_at: string;
}

export interface Endpoint {
  id: number;
  provider_id: number;
  provider_name: string;
  upstream_model: string;
  priority: number;
  enabled: boolean;
  /** USD / 1M tokens the operator pays this provider; null = unknown. */
  input_cost: number | null;
  output_cost: number | null;
  /** null = same as input_cost. */
  cache_read_cost: number | null;
  cache_write_cost: number | null;
  /** Set while the endpoint is skipped after repeated failures. */
  circuit_open_until: string | null;
}

export interface AdminModel {
  id: number;
  slug: string;
  name: string;
  description: string;
  context_length: number;
  input_price: number;
  output_price: number;
  /** null = charged as input. */
  cache_read_price: number | null;
  cache_write_price: number | null;
  enabled: boolean;
  created_at: string;
  endpoints: Endpoint[];
}

export interface RedeemCode {
  id: number;
  code: string;
  amount: number;
  used_by_email: string | null;
  used_at: string | null;
  created_at: string;
}

export interface DailyStat {
  date: string; // YYYY-MM-DD
  requests: number;
  tokens: number;
  cost: number;
}

export interface AppConfig {
  name: string;
  version: string;
  allow_register: boolean;
  /** Only the third-party sign-in providers configured on the server. */
  oauth_providers: OAuthProvider[];
  /** True until an admin account exists: the console shows the setup wizard instead. */
  setup_required: boolean;
}

export type OAuthProvider = "google" | "github";

export const OAUTH_PROVIDERS: OAuthProvider[] = ["google", "github"];

/** A third-party account linked to the current user. */
export interface Identity {
  provider: OAuthProvider;
  email: string;
  created_at: string;
}

export interface Paginated<T> {
  data: T[];
  total: number;
}

export interface ModelStat {
  model: string;
  requests: number;
  tokens: number;
  cost: number;
}

export interface UsageStats {
  total_cost: number;
  total_requests: number;
  total_tokens: number;
  daily: DailyStat[];
  by_model: ModelStat[];
}

export interface AdminStats {
  users: number;
  providers: number;
  models: number;
  total_balance: number;
  requests_today: number;
  cost_today: number; // = revenue today
  profit_today: number;
  daily: DailyStat[];
  alerts: Alert[];
}

export type AlertKind = "key_failed" | "provider_no_keys" | "provider_down" | "model_unroutable" | "missing_cost";

export interface Alert {
  level: "warning" | "error";
  kind: AlertKind;
  message: string; // English fallback
  /** Values for a localized message, by kind. */
  params?: { provider?: string; key?: string; reason?: "auth" | "quota"; model?: string; rate?: number; n?: number } | null;
  provider_id?: number;
  model_id?: number;
  key_id?: number;
}

/** Model from the public OpenRouter catalog, used as a reference for importing. */
export interface CatalogModel {
  id: string;
  name: string;
  description: string;
  context_length: number;
  input_price: number;
  output_price: number;
  cache_read_price: number | null;
  cache_write_price: number | null;
  upstream_match: string | null;
  exists: boolean;
}

export interface CatalogImportInput {
  provider_id: number;
  items: { id: string; upstream_model: string }[];
  markup_percent: number;
  enable: boolean;
  update_existing: boolean;
}

export interface CatalogImportResult {
  created: number;
  updated: number;
  endpoints_added: number;
}

export interface ReportRow {
  requests: number;
  tokens: number;
  revenue: number;
  upstream_cost: number;
  profit: number;
  unknown_cost_requests: number;
}

export interface AdminReport {
  totals: ReportRow;
  daily: (ReportRow & { date: string })[];
  by_model: (ReportRow & { model: string })[];
  by_provider: (ReportRow & { provider_id: number; provider: string; success_rate: number | null })[];
}

export interface ProviderTestResult {
  ok: boolean;
  latency_ms: number;
  message: string;
}

export interface EndpointInput {
  provider_id: number;
  upstream_model: string;
  priority?: number;
  enabled?: boolean;
  input_cost?: number | null;
  output_cost?: number | null;
  cache_read_cost?: number | null;
  cache_write_cost?: number | null;
}

export interface ModelInput {
  slug: string;
  name: string;
  description?: string;
  context_length?: number;
  input_price: number;
  output_price: number;
  cache_read_price?: number | null;
  cache_write_price?: number | null;
  enabled?: boolean;
  endpoints: EndpointInput[];
}

export interface ProviderInput {
  name: string;
  type: ProviderType;
  base_url?: string;
  /** Optional; becomes the provider's first key. */
  api_key?: string;
  enabled?: boolean;
}

/** PATCH body; keys are managed through the provider keys endpoints. */
export type ProviderUpdate = Partial<Omit<ProviderInput, "api_key">>;

export interface KeyInput {
  name?: string;
  credit_limit?: number | null;
  rate_limit_rpm?: number | null;
  allowed_models?: string[];
  disabled?: boolean;
}

export interface AdminUserUpdate {
  name?: string;
  role?: Role;
  disabled?: boolean;
  rate_limit_rpm?: number | null;
}

// ---------------------------------------------------------------------------
// First-run setup and system settings

export type CallbackUrls = Record<OAuthProvider, string>;

export interface SetupInfo {
  setup_required: boolean;
  /** Derived from the request. */
  suggested_public_url: string;
  /** For suggested_public_url. */
  callback_urls: CallbackUrls;
}

export interface OAuthCredentials {
  client_id: string;
  client_secret: string;
}

export interface SetupInput {
  site_name?: string;
  public_url?: string;
  github?: OAuthCredentials;
  google?: OAuthCredentials;
}

export interface SetupResult {
  ok: true;
  oauth_providers: OAuthProvider[];
}

export interface OAuthProviderSettings {
  client_id: string;
  client_secret_set: boolean;
}

export interface SystemSettings {
  site_name: string;
  /** "" = derive from each request. */
  public_url: string;
  allow_register: boolean;
  /** USD for new accounts. */
  signup_credit: number;
  /** Requests/minute per user unless set on the user; 0 = unlimited. */
  default_user_rpm: number;
  request_timeout_seconds: number;
  /** 0 = keep forever. */
  log_retention_days: number;
  /** Read client IPs from X-Forwarded-For. */
  trust_proxy: boolean;
  github: OAuthProviderSettings;
  google: OAuthProviderSettings;
}

/** Where a setting's current value comes from: console value > environment variable > built-in default. */
export type SettingSource = "console" | "env" | "default";

export type SystemSettingKey = keyof SystemSettings;

export interface SystemSettingsResponse {
  settings: SystemSettings;
  sources: Partial<Record<SystemSettingKey, SettingSource>>;
  callback_urls: CallbackUrls;
}

export interface OAuthProviderPatch {
  client_id?: string | null;
  client_secret?: string;
}

type Nullable<T> = { [K in keyof T]?: T[K] | null };

/** PATCH body: any subset; null resets a field to its env/default value. */
export type SystemSettingsInput = Nullable<Omit<SystemSettings, "github" | "google">> & {
  /** client_id "" disables the provider, null drops the console value (back to the env variable, if any);
   *  client_secret "" keeps the current one. */
  github?: OAuthProviderPatch;
  google?: OAuthProviderPatch;
};
