import {
  chmodSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";

export interface DiscoveryRefs {
  readonly analystRef?: string;
  readonly continuationRunRef?: string;
  readonly coordinatorRef?: string;
  readonly firstRunRef?: string;
  readonly instructionRunRef?: string;
  readonly projectRef?: string;
  readonly publishedInstructionRef?: string;
  readonly scheduledRunRef?: string;
  readonly workflowRef?: string;
  readonly workflowRunRef?: string;
  readonly writerRef?: string;
}

interface DiscoveryState {
  readonly resourcePrefix: string;
  readonly refs: DiscoveryRefs;
  readonly updatedAt: string;
  readonly version: number;
}

export const discoveryMode = process.env.KODEX_E2E_DISCOVERY === "1";

export function loadDiscoveryRefs(resourcePrefix: string): DiscoveryRefs {
  if (!discoveryMode || process.env.KODEX_E2E_CHECK_ONLY === "1") return {};
  const path = statePath();
  try {
    const info = lstatSync(path);
    if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) {
      throw new Error(
        "KODEX_E2E_RUN_STATE must be a regular non-symlink file readable only by its owner",
      );
    }
    const state = JSON.parse(readFileSync(path, "utf8")) as DiscoveryState;
    if (state.version !== 1 || state.resourcePrefix !== resourcePrefix) {
      throw new Error(
        "KODEX_E2E_RUN_STATE belongs to another E2E run or has an unsupported version",
      );
    }
    return state.refs;
  } catch (error) {
    if (isMissingFile(error)) return {};
    throw error;
  }
}

export function saveDiscoveryRefs(
  resourcePrefix: string,
  refs: DiscoveryRefs,
): void {
  if (!discoveryMode || process.env.KODEX_E2E_CHECK_ONLY === "1") return;
  const path = statePath();
  const temporaryPath = `${path}.${String(process.pid)}.tmp`;
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  const state: DiscoveryState = {
    version: 1,
    resourcePrefix,
    refs,
    updatedAt: new Date().toISOString(),
  };
  try {
    writeFileSync(temporaryPath, `${JSON.stringify(state, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
      flag: "w",
    });
    chmodSync(temporaryPath, 0o600);
    renameSync(temporaryPath, path);
  } finally {
    rmSync(temporaryPath, { force: true });
  }
}

function statePath(): string {
  const raw = process.env.KODEX_E2E_RUN_STATE;
  if (!raw) {
    throw new Error("KODEX_E2E_RUN_STATE is required in discovery mode");
  }
  return isAbsolute(raw) ? raw : resolve(raw);
}

function isMissingFile(error: unknown): boolean {
  return (
    error instanceof Error &&
    "code" in error &&
    (error as NodeJS.ErrnoException).code === "ENOENT"
  );
}
