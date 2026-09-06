import type { OwnerSessionMetadata } from "../../src/shared/api/generated/openapi/types.gen";

export function syntheticBrowserSession(
  overrides: Partial<OwnerSessionMetadata> = {},
): OwnerSessionMetadata {
  const now = Date.now();
  return {
    generation: "11111111-1111-4111-8111-111111111111",
    version: 1,
    sessionRevision: 1,
    serverTime: new Date(now).toISOString(),
    expiresAt: new Date(now + 20 * 60_000).toISOString(),
    accessExpiresAt: new Date(now + 10 * 60_000).toISOString(),
    absoluteExpiresAt: new Date(now + 60 * 60_000).toISOString(),
    renewAfter: new Date(now + 5 * 60_000).toISOString(),
    renewalMode: "BACKEND_REFRESH",
    ...overrides,
  };
}
