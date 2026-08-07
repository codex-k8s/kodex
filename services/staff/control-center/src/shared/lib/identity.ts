export function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

export function etag(version: number): string {
  if (!Number.isSafeInteger(version) || version < 1)
    throw new Error("Resource version is invalid");
  return `"${String(version)}"`;
}

export function readCookie(name: string): string | undefined {
  const prefix = `${encodeURIComponent(name)}=`;
  for (const part of document.cookie.split(";")) {
    const value = part.trim();
    if (value.startsWith(prefix))
      return decodeURIComponent(value.slice(prefix.length));
  }
  return undefined;
}

export function csrfToken(): string {
  const value = readCookie("__Host-mattercodex-csrf");
  if (!value || value.length < 43 || value.length > 64) {
    throw new Error("CSRF token is unavailable");
  }
  return value;
}

export function mutationHeaders(version?: number): Record<string, string> {
  const headers: Record<string, string> = {
    "X-CSRF-Token": csrfToken(),
    "Idempotency-Key": newIdempotencyKey(),
  };
  if (version !== undefined) headers["If-Match"] = etag(version);
  return headers;
}
