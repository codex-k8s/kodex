import type { OwnerSessionMetadata } from "@/shared/api/generated/openapi/types.gen";

export interface BrowserSessionTiming {
  deadline: number;
  renewAt: number;
}

export function browserSessionTiming(
  value: OwnerSessionMetadata,
  receivedAt = Date.now(),
  elapsedMs = 0,
): BrowserSessionTiming {
  const serverTime = Date.parse(value.serverTime);
  const expiresAt = Date.parse(value.expiresAt);
  const accessExpiresAt = Date.parse(value.accessExpiresAt);
  const absoluteExpiresAt = Date.parse(value.absoluteExpiresAt);
  const renewAfter = Date.parse(value.renewAfter);
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      value.generation,
    ) ||
    !Number.isSafeInteger(value.version) ||
    value.version < 1 ||
    !Number.isSafeInteger(value.sessionRevision) ||
    value.sessionRevision < 1 ||
    ![
      serverTime,
      expiresAt,
      accessExpiresAt,
      absoluteExpiresAt,
      renewAfter,
      receivedAt,
      elapsedMs,
    ].every(Number.isFinite) ||
    elapsedMs < 0 ||
    expiresAt > absoluteExpiresAt ||
    !["BACKEND_REFRESH", "REAUTHENTICATION"].includes(value.renewalMode)
  )
    throw new Error("Browser session metadata is invalid");
  const offset = receivedAt - serverTime - elapsedMs;
  const deadline =
    Math.min(
      expiresAt,
      absoluteExpiresAt,
      value.renewalMode === "REAUTHENTICATION" ? accessExpiresAt : Infinity,
    ) + offset;
  if (deadline <= receivedAt) throw new Error("Browser session has expired");
  return {
    deadline,
    renewAt:
      value.renewalMode === "REAUTHENTICATION"
        ? deadline
        : Math.min(deadline, Math.max(receivedAt, renewAfter + offset)),
  };
}

export function browserSessionIdentity(value: OwnerSessionMetadata): string {
  return `${value.generation}:${String(value.version)}`;
}

export function authorizationRedirect(
  value: string,
  authority: string,
): string {
  const url = new URL(value);
  if (
    url.protocol !== "https:" ||
    url.origin !== new URL(authority).origin ||
    url.username ||
    url.password ||
    url.hash
  )
    throw new Error("Owner authorization redirect is invalid");
  return url.toString();
}

export function authorizationCallback(url: URL): {
  code: string;
  state: string;
} {
  const codes = url.searchParams.getAll("code");
  const states = url.searchParams.getAll("state");
  if (
    url.hash ||
    url.searchParams.has("error") ||
    codes.length !== 1 ||
    states.length !== 1 ||
    !codes[0] ||
    codes[0].length > 4096 ||
    !states[0] ||
    !/^[A-Za-z0-9_-]{43}$/.test(states[0])
  )
    throw new Error("Owner authorization callback is invalid");
  return { code: codes[0], state: states[0] };
}
