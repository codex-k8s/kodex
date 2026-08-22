import { lstatSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";

export type E2EProfile = "web-only" | "mattermost";

export interface E2EEnvironment {
  readonly baseURL: string;
  readonly checkOnly: boolean;
  readonly profile: E2EProfile;
  readonly resourcePrefix: string;
  readonly runTimeoutMs: number;
  readonly storageState?: string;
}

const checkOnly = process.env.MATTERCODEX_E2E_CHECK_ONLY === "1";
const disposableConfirmation =
  "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION";

function exactHTTPSURL(raw: string): string {
  const parsed = new URL(raw);
  if (
    parsed.protocol !== "https:" ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash ||
    parsed.pathname !== "/"
  ) {
    throw new Error(
      "MATTERCODEX_E2E_BASE_URL must be an exact HTTPS origin without credentials, path, query, or fragment",
    );
  }
  return parsed.origin;
}

function boundedInteger(raw: string | undefined): number {
  const value = Number(raw ?? "900000");
  if (!Number.isSafeInteger(value) || value < 60_000 || value > 1_800_000) {
    throw new Error(
      "MATTERCODEX_E2E_RUN_TIMEOUT_MS must be between 60000 and 1800000",
    );
  }
  return value;
}

function storageStatePath(raw: string | undefined): string | undefined {
  if (checkOnly) return undefined;
  if (!raw) {
    throw new Error(
      "MATTERCODEX_E2E_STORAGE_STATE is required for a real authenticated E2E run",
    );
  }
  const path = isAbsolute(raw) ? raw : resolve(raw);
  const info = lstatSync(path);
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) {
    throw new Error(
      "MATTERCODEX_E2E_STORAGE_STATE must be a regular non-symlink file readable only by its owner",
    );
  }
  return path;
}

function requireDisposableConfirmation(): void {
  if (
    !checkOnly &&
    process.env.MATTERCODEX_E2E_CONFIRM_DISPOSABLE !== disposableConfirmation
  ) {
    throw new Error(
      `MATTERCODEX_E2E_CONFIRM_DISPOSABLE must equal ${disposableConfirmation}`,
    );
  }
}

function profile(raw: string | undefined): E2EProfile {
  if (raw === undefined || raw === "web-only") return "web-only";
  if (raw === "mattermost") return "mattermost";
  throw new Error(
    "MATTERCODEX_E2E_PROFILE must be either web-only or mattermost",
  );
}

function resourcePrefix(raw: string | undefined): string {
  const value = raw ?? (checkOnly ? "check-only" : "");
  if (!/^[a-z0-9](?:[a-z0-9-]{2,38}[a-z0-9])$/.test(value)) {
    throw new Error(
      "MATTERCODEX_E2E_RESOURCE_PREFIX must be a unique lowercase 4-40 character slug",
    );
  }
  return value;
}

export function loadE2EEnvironment(): E2EEnvironment {
  requireDisposableConfirmation();
  return {
    baseURL: exactHTTPSURL(
      process.env.MATTERCODEX_E2E_BASE_URL ?? "https://mattercodex.invalid",
    ),
    checkOnly,
    profile: profile(process.env.MATTERCODEX_E2E_PROFILE),
    resourcePrefix: resourcePrefix(process.env.MATTERCODEX_E2E_RESOURCE_PREFIX),
    runTimeoutMs: boundedInteger(process.env.MATTERCODEX_E2E_RUN_TIMEOUT_MS),
    storageState: storageStatePath(process.env.MATTERCODEX_E2E_STORAGE_STATE),
  };
}

export interface E2EAuthEnvironment {
  readonly baseURL: string;
  readonly checkOnly: boolean;
  readonly outputStorageState: string;
  readonly ownerPassword: string;
  readonly ownerUsername: string;
}

export function loadE2EAuthEnvironment(): E2EAuthEnvironment {
  requireDisposableConfirmation();
  const ownerUsername =
    process.env.MATTERCODEX_E2E_OWNER_USERNAME ?? (checkOnly ? "owner" : "");
  const ownerPassword =
    process.env.MATTERCODEX_E2E_OWNER_PASSWORD ??
    (checkOnly ? "check-only-password" : "");
  if (!ownerUsername || !ownerPassword) {
    throw new Error(
      "MATTERCODEX_E2E_OWNER_USERNAME and MATTERCODEX_E2E_OWNER_PASSWORD are required for OIDC sign-in",
    );
  }
  const output =
    process.env.MATTERCODEX_E2E_STORAGE_STATE ?? ".auth/owner.json";
  return {
    baseURL: exactHTTPSURL(
      process.env.MATTERCODEX_E2E_BASE_URL ?? "https://mattercodex.invalid",
    ),
    checkOnly,
    outputStorageState: isAbsolute(output) ? output : resolve(output),
    ownerPassword,
    ownerUsername,
  };
}
