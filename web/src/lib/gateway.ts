/** Base URL of the OpenAI-compatible gateway API served by this instance. */
export function gatewayBaseUrl(): string {
  return `${window.location.origin}/v1`;
}
