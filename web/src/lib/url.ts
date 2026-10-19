/** "https://x.example/" → "https://x.example" */
export function normalizeUrl(u: string): string {
  return u.trim().replace(/\/+$/, "");
}

export function isHttpUrl(u: string): boolean {
  return /^https?:\/\/[^\s/?#]+[^\s]*$/i.test(u.trim());
}

/** True when the URL points at this machine only (nobody else could reach its OAuth callback). */
export function isLocalhostUrl(u: string): boolean {
  try {
    const h = new URL(u).hostname.toLowerCase();
    return h === "localhost" || h.endsWith(".localhost") || h === "127.0.0.1" || h === "[::1]" || h === "::1";
  } catch {
    return false;
  }
}
