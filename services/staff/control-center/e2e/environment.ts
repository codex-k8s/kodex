import { lstatSync } from "node:fs";
import { isAbsolute, resolve } from "node:path";

import { type BrowserStorageState, readStorageState } from "./storage-state";

export type E2EProfile = "web-only" | "mattermost";

export interface E2EEnvironment {
  readonly baseURL: string;
  readonly checkOnly: boolean;
  readonly profile: E2EProfile;
  readonly resourcePrefix: string;
  readonly runTimeoutMs: number;
  readonly storageState?: BrowserStorageState;
  readonly mattermost?: MattermostE2EEnvironment;
}

export interface MattermostE2EEnvironment {
  readonly origin: string;
  readonly tokenFile?: string;
  readonly teamName: string;
  readonly channelName: string;
  readonly healthyConnectionName: string;
  readonly outageConnectionName: string;
}

const checkOnly = process.env.KODEX_E2E_CHECK_ONLY === "1";
const disposableConfirmation =
  "I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION";

function exactHTTPSURL(raw: string, variable = "KODEX_E2E_BASE_URL"): string {
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
      `${variable} must be an exact HTTPS origin without credentials, path, query, or fragment`,
    );
  }
  return parsed.origin;
}

function boundedInteger(raw: string | undefined): number {
  const value = Number(raw ?? "900000");
  if (!Number.isSafeInteger(value) || value < 60_000 || value > 1_800_000) {
    throw new Error(
      "KODEX_E2E_RUN_TIMEOUT_MS must be between 60000 and 1800000",
    );
  }
  return value;
}

function storageState(
  raw: string | undefined,
): BrowserStorageState | undefined {
  if (checkOnly) return undefined;
  if (!raw) {
    throw new Error(
      "KODEX_E2E_STORAGE_STATE is required for a real authenticated E2E run",
    );
  }
  return readStorageState(isAbsolute(raw) ? raw : resolve(raw));
}

function protectedFilePath(
  variable: string,
  raw: string | undefined,
): string | undefined {
  if (checkOnly) return undefined;
  if (!raw) throw new Error(`${variable} is required`);
  const path = isAbsolute(raw) ? raw : resolve(raw);
  const info = lstatSync(path);
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) {
    throw new Error(
      `${variable} must be a regular non-symlink file readable only by its owner`,
    );
  }
  return path;
}

function boundedName(variable: string, raw: string | undefined): string {
  const value = raw ?? (checkOnly ? "check-only" : "");
  if (!value || value.length > 160 || /[\r\n]/.test(value)) {
    throw new Error(`${variable} must be a non-empty single-line name`);
  }
  return value;
}

function mattermostEnvironment(
  selectedProfile: E2EProfile,
): MattermostE2EEnvironment | undefined {
  if (selectedProfile !== "mattermost") return undefined;
  const teamName = boundedName(
    "KODEX_E2E_MATTERMOST_TEAM_NAME",
    process.env.KODEX_E2E_MATTERMOST_TEAM_NAME,
  );
  const channelName = boundedName(
    "KODEX_E2E_MATTERMOST_CHANNEL_NAME",
    process.env.KODEX_E2E_MATTERMOST_CHANNEL_NAME,
  );
  if (!/^[a-z0-9][a-z0-9_-]{1,62}$/.test(teamName)) {
    throw new Error(
      "KODEX_E2E_MATTERMOST_TEAM_NAME must be a lowercase Mattermost name",
    );
  }
  if (!/^[a-z0-9][a-z0-9_-]{1,62}$/.test(channelName)) {
    throw new Error(
      "KODEX_E2E_MATTERMOST_CHANNEL_NAME must be a lowercase Mattermost name",
    );
  }
  return {
    origin: exactHTTPSURL(
      process.env.KODEX_E2E_MATTERMOST_ORIGIN ?? "https://mattermost.invalid",
      "KODEX_E2E_MATTERMOST_ORIGIN",
    ),
    tokenFile: protectedFilePath(
      "KODEX_E2E_MATTERMOST_TOKEN_FILE",
      process.env.KODEX_E2E_MATTERMOST_TOKEN_FILE,
    ),
    teamName,
    channelName,
    healthyConnectionName: boundedName(
      "KODEX_E2E_MATTERMOST_HEALTHY_CONNECTION",
      process.env.KODEX_E2E_MATTERMOST_HEALTHY_CONNECTION,
    ),
    outageConnectionName: boundedName(
      "KODEX_E2E_MATTERMOST_OUTAGE_CONNECTION",
      process.env.KODEX_E2E_MATTERMOST_OUTAGE_CONNECTION,
    ),
  };
}

function requireDisposableConfirmation(): void {
  if (
    !checkOnly &&
    process.env.KODEX_E2E_CONFIRM_DISPOSABLE !== disposableConfirmation
  ) {
    throw new Error(
      `KODEX_E2E_CONFIRM_DISPOSABLE must equal ${disposableConfirmation}`,
    );
  }
}

function profile(raw: string | undefined): E2EProfile {
  if (raw === undefined || raw === "web-only") return "web-only";
  if (raw === "mattermost") return "mattermost";
  throw new Error("KODEX_E2E_PROFILE must be either web-only or mattermost");
}

function resourcePrefix(raw: string | undefined): string {
  const value = raw ?? (checkOnly ? "check-only" : "");
  if (!/^[a-z0-9](?:[a-z0-9-]{2,38}[a-z0-9])$/.test(value)) {
    throw new Error(
      "KODEX_E2E_RESOURCE_PREFIX must be a unique lowercase 4-40 character slug",
    );
  }
  return value;
}

export function loadE2EEnvironment(): E2EEnvironment {
  requireDisposableConfirmation();
  const selectedProfile = profile(process.env.KODEX_E2E_PROFILE);
  return {
    baseURL: exactHTTPSURL(
      process.env.KODEX_E2E_BASE_URL ?? "https://kodex.invalid",
    ),
    checkOnly,
    profile: selectedProfile,
    resourcePrefix: resourcePrefix(process.env.KODEX_E2E_RESOURCE_PREFIX),
    runTimeoutMs: boundedInteger(process.env.KODEX_E2E_RUN_TIMEOUT_MS),
    storageState: storageState(process.env.KODEX_E2E_STORAGE_STATE),
    mattermost: mattermostEnvironment(selectedProfile),
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
    process.env.KODEX_E2E_OWNER_USERNAME ?? (checkOnly ? "owner" : "");
  const ownerPassword =
    process.env.KODEX_E2E_OWNER_PASSWORD ??
    (checkOnly ? "check-only-password" : "");
  if (!ownerUsername || !ownerPassword) {
    throw new Error(
      "KODEX_E2E_OWNER_USERNAME and KODEX_E2E_OWNER_PASSWORD are required for OIDC sign-in",
    );
  }
  const output = process.env.KODEX_E2E_STORAGE_STATE ?? ".auth/owner.json";
  return {
    baseURL: exactHTTPSURL(
      process.env.KODEX_E2E_BASE_URL ?? "https://kodex.invalid",
    ),
    checkOnly,
    outputStorageState: isAbsolute(output) ? output : resolve(output),
    ownerPassword,
    ownerUsername,
  };
}
