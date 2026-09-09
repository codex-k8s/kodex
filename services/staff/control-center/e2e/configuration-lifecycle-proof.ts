import { createHash } from "node:crypto";
import { constants } from "node:fs";
import { lstat, mkdir, open, readFile, realpath } from "node:fs/promises";
import { dirname, isAbsolute, resolve } from "node:path";

import type { Versions } from "./ui-acceptance-proof";

export const lifecycleOperations = [
  "PROMPT_CREATE",
  "PROMPT_VALIDATE",
  "PROMPT_PUBLISH",
  "INTEGRATION_CREATE",
  "INTEGRATION_VALIDATE",
  "INTEGRATION_PUBLISH",
  "INTEGRATION_RESTORE",
  "INTEGRATION_COPY",
  "INTEGRATION_ARCHIVE",
] as const;
export type LifecycleOperation = (typeof lifecycleOperations)[number];
export type LifecycleOutcome = "PASS" | "REJECTED" | "UNKNOWN";

interface Header {
  schemaVersion: 1;
  type: "metadata";
  profile: "CONFIGURATION_UI_LIFECYCLE";
  prefix: string;
  versions: Versions;
  browser: "chromium" | "firefox" | "webkit";
  syntheticSourceSHA256: string;
  timestampUTC: string;
}
interface Intent {
  type: "intent";
  sequence: number;
  operation: LifecycleOperation;
  inputSHA256: string;
  timestampUTC: string;
}
interface Stop {
  type: "stop";
  sequence: number;
  operation: LifecycleOperation;
  outcome: LifecycleOutcome;
  requestSHA256?: string;
  status?: number;
  refSHA256?: string;
  version?: number;
  revision?: number;
  publishedRefSHA256?: string;
  timestampUTC: string;
}
type Entry = Header | Intent | Stop;

export interface LifecycleIdentity {
  ref: string;
  version: number;
  revision?: number;
  publishedRef?: string;
}

export interface LifecycleConfiguration {
  baseURL: string;
  browser: "chromium" | "firefox" | "webkit";
  journalPath: string;
  prefix: string;
  resume: boolean;
  runTimeoutMs: number;
  syntheticSource: string;
  syntheticSourceSHA256: string;
  versions: Versions;
}

const hash = (value: string | Buffer) =>
  createHash("sha256").update(value).digest("hex");

export function safeRefDigest(ref: string): string {
  if (!/^[a-zA-Z0-9_-]{1,100}$/.test(ref))
    throw new Error("Invalid configuration reference");
  return hash(ref);
}

export function operationOutcome(status: number | undefined): LifecycleOutcome {
  if (status === undefined || status >= 500) return "UNKNOWN";
  if (status >= 200 && status < 300) return "PASS";
  return "REJECTED";
}

export function permittedLifecycleRequest(
  method: string,
  pathname: string,
): boolean {
  if (["GET", "HEAD", "OPTIONS"].includes(method)) return true;
  if (method === "PUT" && pathname === "/api/v1/session") return true;
  if (method !== "POST") return false;
  if (pathname === "/api/v1/session/ticket") return true;
  return [
    /^\/api\/v1\/(?:prompt-template|integration-definition)-configurations\/drafts$/,
    /^\/api\/v1\/(?:prompt-template|integration-definition)-configurations\/[a-zA-Z0-9_-]+\/revisions\/[a-zA-Z0-9_-]+\/(?:saves|validation|publication|impact-plans)$/,
    /^\/api\/v1\/integration-definition-configurations\/copies$/,
    /^\/api\/v1\/integration-definition-configurations\/[a-zA-Z0-9_-]+\/archive$/,
  ].some((pattern) => pattern.test(pathname));
}

function exactOrigin(raw: string): string {
  const value = new URL(raw);
  if (
    value.protocol !== "https:" ||
    value.origin !== raw ||
    value.username ||
    value.password
  )
    throw new Error("KODEX_E2E_BASE_URL must be an exact HTTPS origin");
  return value.origin;
}

function positiveInteger(raw: string | undefined): number {
  const value = Number(raw ?? "900000");
  if (!Number.isSafeInteger(value) || value < 60_000 || value > 1_800_000)
    throw new Error("Invalid configuration lifecycle timeout");
  return value;
}

async function privateRegularFile(path: string, maximumBytes: number) {
  if (!isAbsolute(path) || resolve(path) !== path)
    throw new Error("Private canonical file path required");
  const info = await lstat(path);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    info.size < 1 ||
    info.size > maximumBytes ||
    (info.mode & 0o077) !== 0 ||
    (await realpath(path)) !== path
  )
    throw new Error("Private regular file required");
  return readFile(path);
}

export async function loadLifecycleConfiguration(
  env: NodeJS.ProcessEnv,
  versions: Versions,
): Promise<LifecycleConfiguration> {
  const checkOnly = env.KODEX_E2E_CHECK_ONLY === "1";
  const browser = env.KODEX_E2E_BROWSER ?? "chromium";
  if (!["chromium", "firefox", "webkit"].includes(browser))
    throw new Error("Unsupported configuration lifecycle browser");
  const prefix =
    env.KODEX_E2E_RESOURCE_PREFIX ?? (checkOnly ? "check-only" : "");
  if (!/^[a-z][a-z0-9-]{3,39}$/.test(prefix))
    throw new Error("Invalid configuration lifecycle prefix");
  if (
    !checkOnly &&
    env.KODEX_E2E_CONFIGURATION_LIFECYCLE_CONFIRM !==
      "RUN_CONFIGURATION_UI_LIFECYCLE"
  )
    throw new Error("Configuration lifecycle confirmation is required");
  const journalPath =
    env.KODEX_E2E_CONFIGURATION_LIFECYCLE_STATE ??
    (checkOnly ? "/tmp/kodex-check-only-configuration-lifecycle.jsonl" : "");
  if (!isAbsolute(journalPath) || resolve(journalPath) !== journalPath)
    throw new Error("Configuration lifecycle state path must be absolute");
  const sourcePath = resolve(
    env.KODEX_E2E_CONFIGURATION_SYNTHETIC_SOURCE ??
      "../../../contracts/integrations/v1/definitions/synthetic.yaml",
  );
  const source = checkOnly
    ? Buffer.from("check-only")
    : await readFile(sourcePath);
  if (source.length < 1 || source.length > 262_144)
    throw new Error("Invalid Synthetic definition source");
  if (!checkOnly) {
    const manifestPath = env.KODEX_E2E_SERVING_MANIFEST ?? "";
    const manifest = await privateRegularFile(manifestPath, 1 << 20);
    if (hash(manifest) !== versions.servingManifestSHA256)
      throw new Error("Serving manifest digest mismatch");
  }
  return {
    baseURL: exactOrigin(env.KODEX_E2E_BASE_URL ?? "https://kodex.invalid"),
    browser: browser as LifecycleConfiguration["browser"],
    journalPath,
    prefix,
    resume: env.KODEX_E2E_CONFIGURATION_LIFECYCLE_RESUME === "1",
    runTimeoutMs: positiveInteger(env.KODEX_E2E_RUN_TIMEOUT_MS),
    syntheticSource: source.toString("utf8"),
    syntheticSourceSHA256: hash(source),
    versions,
  };
}

function validateEntries(
  raw: string,
  header: Omit<Header, "timestampUTC">,
): Entry[] {
  const lines = raw.trim().split("\n");
  if (!lines.length || lines.length > 128)
    throw new Error("Invalid configuration lifecycle journal");
  const entries = lines.map((line) => {
    if (Buffer.byteLength(line) > 16_384)
      throw new Error("Configuration lifecycle journal record is too large");
    return JSON.parse(line) as Entry;
  });
  const first = entries[0];
  const firstRecord = first as unknown as Record<string, unknown>;
  if (
    !first ||
    first.type !== "metadata" ||
    firstRecord.schemaVersion !== header.schemaVersion ||
    firstRecord.profile !== header.profile ||
    first.prefix !== header.prefix ||
    first.browser !== header.browser ||
    first.syntheticSourceSHA256 !== header.syntheticSourceSHA256 ||
    JSON.stringify(first.versions) !== JSON.stringify(header.versions)
  )
    throw new Error("Configuration lifecycle journal scope mismatch");
  const intents = new Map<number, LifecycleOperation>();
  for (const entry of entries.slice(1)) {
    if (entry.type === "metadata")
      throw new Error("Invalid configuration lifecycle journal record");
    if (
      !Number.isSafeInteger(entry.sequence) ||
      entry.sequence < 1 ||
      !lifecycleOperations.includes(entry.operation)
    )
      throw new Error("Invalid configuration lifecycle journal record");
    if (entry.type === "intent") {
      if (
        intents.has(entry.sequence) ||
        !/^[a-f0-9]{64}$/.test(entry.inputSHA256)
      )
        throw new Error("Invalid configuration lifecycle intent");
      intents.set(entry.sequence, entry.operation);
      continue;
    }
    if (
      intents.get(entry.sequence) !== entry.operation ||
      !["PASS", "REJECTED", "UNKNOWN"].includes(entry.outcome) ||
      (entry.status !== undefined &&
        (!Number.isSafeInteger(entry.status) ||
          entry.status < 100 ||
          entry.status > 599)) ||
      [entry.requestSHA256, entry.refSHA256, entry.publishedRefSHA256].some(
        (digest) => digest !== undefined && !/^[a-f0-9]{64}$/.test(digest),
      ) ||
      (entry.version !== undefined &&
        (!Number.isSafeInteger(entry.version) || entry.version < 1)) ||
      (entry.revision !== undefined &&
        (!Number.isSafeInteger(entry.revision) || entry.revision < 1))
    )
      throw new Error("Invalid configuration lifecycle outcome");
  }
  return entries;
}

export async function openLifecycleJournal(
  configuration: LifecycleConfiguration,
) {
  const path = configuration.journalPath;
  const parent = dirname(path);
  const parentInfo = await lstat(parent);
  if (
    !parentInfo.isDirectory() ||
    parentInfo.isSymbolicLink() ||
    (parentInfo.mode & 0o077) !== 0 ||
    (await realpath(parent)) !== parent
  )
    throw new Error("Configuration lifecycle state parent must be private");
  const header = {
    schemaVersion: 1 as const,
    type: "metadata" as const,
    profile: "CONFIGURATION_UI_LIFECYCLE" as const,
    prefix: configuration.prefix,
    versions: configuration.versions,
    browser: configuration.browser,
    syntheticSourceSHA256: configuration.syntheticSourceSHA256,
  };
  let entries: Entry[] = [];
  let file;
  if (configuration.resume) {
    const input = await privateRegularFile(path, 1 << 20);
    entries = validateEntries(input.toString("utf8"), header);
    file = await open(
      path,
      constants.O_WRONLY | constants.O_APPEND | constants.O_NOFOLLOW,
    );
  } else {
    await mkdir(parent, { recursive: true, mode: 0o700 });
    file = await open(
      path,
      constants.O_WRONLY |
        constants.O_CREAT |
        constants.O_EXCL |
        constants.O_NOFOLLOW,
      0o600,
    );
    const metadata: Header = {
      ...header,
      timestampUTC: new Date().toISOString(),
    };
    const line = `${JSON.stringify(metadata)}\n`;
    await file.writeFile(line);
    await file.sync();
    entries = [metadata];
  }
  const append = async (entry: Entry) => {
    const line = `${JSON.stringify(entry)}\n`;
    if (Buffer.byteLength(line) > 16_384)
      throw new Error("Configuration lifecycle journal record is too large");
    await file.writeFile(line);
    await file.sync();
    entries.push(entry);
  };
  const latestOutcome = (sequence: number) =>
    entries
      .filter(
        (entry): entry is Stop =>
          entry.type === "stop" && entry.sequence === sequence,
      )
      .at(-1)?.outcome;
  const pending = () => {
    return entries.filter(
      (entry): entry is Intent =>
        entry.type === "intent" &&
        !["PASS", "REJECTED"].includes(latestOutcome(entry.sequence) ?? ""),
    );
  };
  return {
    entries: () => [...entries],
    latestOutcome,
    pending,
    intent: async (operation: LifecycleOperation, input: string) => {
      if (pending().length) throw new Error("Unresolved lifecycle intent");
      const sequence =
        1 +
        Math.max(
          0,
          ...entries
            .filter((entry) => entry.type === "intent")
            .map((entry) => entry.sequence),
        );
      await append({
        type: "intent",
        sequence,
        operation,
        inputSHA256: hash(input),
        timestampUTC: new Date().toISOString(),
      });
      return sequence;
    },
    stop: async (
      sequence: number,
      operation: LifecycleOperation,
      outcome: LifecycleOutcome,
      identity?: LifecycleIdentity,
      status?: number,
      requestBody?: string,
    ) => {
      if (
        !pending().some(
          (entry) =>
            entry.sequence === sequence && entry.operation === operation,
        )
      )
        throw new Error("Lifecycle intent is not pending");
      await append({
        type: "stop",
        sequence,
        operation,
        outcome,
        ...(requestBody === undefined
          ? {}
          : { requestSHA256: hash(requestBody) }),
        ...(status === undefined ? {} : { status }),
        ...(identity === undefined
          ? {}
          : {
              refSHA256: safeRefDigest(identity.ref),
              version: identity.version,
              ...(identity.revision === undefined
                ? {}
                : { revision: identity.revision }),
              ...(identity.publishedRef === undefined
                ? {}
                : { publishedRefSHA256: safeRefDigest(identity.publishedRef) }),
            }),
        timestampUTC: new Date().toISOString(),
      });
    },
    close: () => file.close(),
  };
}
