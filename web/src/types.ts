// Types mirroring docs/console-api.md. Keep in sync with the backend contract.

export type Role = "admin" | "user";

export interface User {
  id: number;
  email: string;
  name: string;
  role: Role;
  balance: number; // USD
  disabled: boolean;
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
  providers: number;
}

export interface UsageLog {
  id: number;
  model: string;
  provider: string;
  api_key_name: string;
  prompt_tokens: number;
  completion_tokens: number;
  cost: number;
  latency_ms: number;
  status: "success" | "error";
  error: string;
  stream: boolean;
  created_at: string;
  user_email?: string;
}

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
  api_key_hint: string;
  enabled: boolean;
  created_at: string;
}

export interface Endpoint {
  id: number;
  provider_id: number;
  provider_name: string;
  upstream_model: string;
  priority: number;
  enabled: boolean;
}

export interface AdminModel {
  id: number;
  slug: string;
  name: string;
  description: string;
  context_length: number;
  input_price: number;
  output_price: number;
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
  cost_today: number;
  daily: DailyStat[];
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
}

export interface ModelInput {
  slug: string;
  name: string;
  description?: string;
  context_length?: number;
  input_price: number;
  output_price: number;
  enabled?: boolean;
  endpoints: EndpointInput[];
}

export interface ProviderInput {
  name: string;
  type: ProviderType;
  base_url?: string;
  api_key: string;
  enabled?: boolean;
}
