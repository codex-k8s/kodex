import { randomUUID } from "node:crypto";
import {
  constants,
  closeSync,
  fstatSync,
  openSync,
  readFileSync,
  realpathSync,
} from "node:fs";
import { mkdir, open, realpath, rename, rm } from "node:fs/promises";
import { basename, dirname, resolve } from "node:path";

const maximumStorageStateBytes = 1 << 20;
const apiCookieNames = new Set(["__Host-kodex-session", "__Host-kodex-csrf"]);

interface StorageCookie {
  readonly domain: string;
  readonly expires: number;
  readonly httpOnly: boolean;
  readonly name: string;
  readonly partitionKey?: string;
  readonly path: string;
  readonly sameSite: "Lax" | "None" | "Strict";
  readonly secure: boolean;
  readonly value: string;
}

interface StorageOrigin {
  readonly localStorage: StorageValue[];
  readonly origin: string;
}

interface StorageValue {
  readonly name: string;
  readonly value: string;
}

export interface BrowserStorageState {
  readonly cookies: StorageCookie[];
  readonly origins: StorageOrigin[];
}

export function withoutKodexAPICookies(value: unknown): BrowserStorageState {
  const parsed = parseStorageState(value, false);
  return {
    cookies: parsed.cookies.filter(
      (cookie) => !apiCookieNames.has(cookie.name),
    ),
    origins: parsed.origins,
  };
}

export function readStorageState(rawPath: string): BrowserStorageState {
  const path = resolve(rawPath);
  validateParentDirectorySync(dirname(path));
  const descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = fstatSync(descriptor);
    validateStorageFile(info, path);
    if (info.size < 1 || info.size > maximumStorageStateBytes) {
      throw new Error("E2E storage state file size is invalid");
    }
    const raw = readFileSync(descriptor, "utf8");
    return parseStorageState(JSON.parse(raw) as unknown, true);
  } catch (error) {
    if (error instanceof SyntaxError) {
      throw new Error("E2E storage state JSON is invalid", { cause: error });
    }
    throw error;
  } finally {
    closeSync(descriptor);
  }
}

export async function writeStorageState(
  rawPath: string,
  value: unknown,
): Promise<void> {
  await writeParsedStorageState(rawPath, parseStorageState(value, true));
}

export async function writeAuthenticatedStorageState(
  rawPath: string,
  value: unknown,
  expectedOrigin: string,
): Promise<void> {
  const origin = exactHTTPSOrigin(expectedOrigin);
  const state = parseStorageState(value, false);
  const matchingCookies = state.cookies.filter(
    (cookie) =>
      cookie.domain.replace(/^\./, "") === origin.hostname &&
      cookie.path === "/" &&
      cookie.secure,
  );
  const csrf = matchingCookies.find(
    (cookie) => cookie.name === "__Host-kodex-csrf",
  );
  const session = matchingCookies.find(
    (cookie) => cookie.name === "__Host-kodex-session",
  );
  if (
    !csrf ||
    csrf.value.length < 43 ||
    csrf.value.length > 256 ||
    !session ||
    !session.httpOnly ||
    session.value.length < 32 ||
    session.value.length > 16_384
  ) {
    throw new Error(
      "Authenticated E2E storage state does not contain the exact Kodex API cookies",
    );
  }
  await writeParsedStorageState(rawPath, state);
}

async function writeParsedStorageState(
  rawPath: string,
  state: BrowserStorageState,
): Promise<void> {
  const path = resolve(rawPath);
  const parent = dirname(path);
  await mkdir(parent, { recursive: true, mode: 0o700 });
  const directory = await openSafeDirectory(parent);
  const temporaryPath = `${parent}/.${basename(path)}.${String(process.pid)}.${randomUUID()}.tmp`;
  let temporaryCreated = false;
  try {
    await validateExistingTarget(path);
    const payload = `${JSON.stringify(state)}\n`;
    if (Buffer.byteLength(payload) > maximumStorageStateBytes) {
      throw new Error("E2E storage state file size is invalid");
    }
    const temporary = await open(
      temporaryPath,
      constants.O_WRONLY |
        constants.O_CREAT |
        constants.O_EXCL |
        constants.O_NOFOLLOW,
      0o600,
    );
    temporaryCreated = true;
    try {
      await temporary.writeFile(payload, "utf8");
      await temporary.sync();
      validateStorageFile(await temporary.stat(), temporaryPath);
    } finally {
      await temporary.close();
    }
    await rename(temporaryPath, path);
    temporaryCreated = false;
    const written = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try {
      validateStorageFile(await written.stat(), path);
    } finally {
      await written.close();
    }
    await directory.sync();
  } finally {
    if (temporaryCreated) await rm(temporaryPath, { force: true });
    await directory.close();
  }
}

function exactHTTPSOrigin(value: string): URL {
  const parsed = new URL(value);
  if (
    parsed.protocol !== "https:" ||
    parsed.origin !== value ||
    parsed.username ||
    parsed.password
  ) {
    throw new Error("Authenticated E2E origin must be an exact HTTPS origin");
  }
  return parsed;
}

async function openSafeDirectory(path: string) {
  if ((await realpath(path)) !== path) {
    throw new Error("E2E storage state directory must not contain symlinks");
  }
  const directory = await open(
    path,
    constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
  );
  const info = await directory.stat();
  if (
    !info.isDirectory() ||
    info.uid !== currentUID() ||
    (info.mode & 0o777) !== 0o700
  ) {
    await directory.close();
    throw new Error(
      "E2E storage state directory must be owner-controlled with mode 0700",
    );
  }
  return directory;
}

function validateParentDirectorySync(path: string): void {
  if (realpathSync(path) !== path) {
    throw new Error("E2E storage state directory must not contain symlinks");
  }
  const descriptor = openSync(
    path,
    constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
  );
  try {
    const info = fstatSync(descriptor);
    if (
      !info.isDirectory() ||
      info.uid !== currentUID() ||
      (info.mode & 0o777) !== 0o700
    ) {
      throw new Error(
        "E2E storage state directory must be owner-controlled with mode 0700",
      );
    }
  } finally {
    closeSync(descriptor);
  }
}

async function validateExistingTarget(path: string): Promise<void> {
  let existing;
  try {
    existing = await open(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  } catch (error) {
    if (isFileSystemError(error, "ENOENT")) return;
    throw new Error("Existing E2E storage state file is unsafe", {
      cause: error,
    });
  }
  try {
    validateStorageFile(await existing.stat(), path);
  } finally {
    await existing.close();
  }
}

function validateStorageFile(
  info: { isFile(): boolean; mode: number; uid: number },
  path: string,
): void {
  if (
    !info.isFile() ||
    info.uid !== currentUID() ||
    (info.mode & 0o777) !== 0o600
  ) {
    throw new Error(
      `E2E storage state must be an owner-controlled regular file with mode 0600: ${path}`,
    );
  }
}

function currentUID(): number {
  const uid = process.getuid?.();
  if (uid === undefined) {
    throw new Error("E2E storage state requires a POSIX owner identity");
  }
  return uid;
}

function parseStorageState(
  value: unknown,
  rejectAPICookies: boolean,
): BrowserStorageState {
  const root = exactObject(value, ["cookies", "origins"]);
  if (!Array.isArray(root.cookies) || !Array.isArray(root.origins)) {
    throw new Error("E2E storage state schema is invalid");
  }
  const cookies = root.cookies.map((raw) => {
    const cookie = exactObject(
      raw,
      [
        "name",
        "value",
        "domain",
        "path",
        "expires",
        "httpOnly",
        "secure",
        "sameSite",
      ],
      ["partitionKey"],
    );
    if (
      !isString(cookie.name) ||
      !isString(cookie.value) ||
      !isString(cookie.domain) ||
      !isString(cookie.path) ||
      typeof cookie.expires !== "number" ||
      !Number.isFinite(cookie.expires) ||
      typeof cookie.httpOnly !== "boolean" ||
      typeof cookie.secure !== "boolean" ||
      typeof cookie.sameSite !== "string" ||
      !["Lax", "None", "Strict"].includes(cookie.sameSite) ||
      (cookie.partitionKey !== undefined && !isString(cookie.partitionKey))
    ) {
      throw new Error("E2E storage state cookie schema is invalid");
    }
    if (rejectAPICookies && apiCookieNames.has(cookie.name)) {
      throw new Error(
        "E2E bootstrap storage state contains a Kodex API cookie",
      );
    }
    return cookie as unknown as StorageCookie;
  });
  const origins = root.origins.map((raw) => {
    const origin = exactObject(raw, ["origin", "localStorage"]);
    if (!isString(origin.origin) || !isExactOrigin(origin.origin)) {
      throw new Error("E2E storage state origin schema is invalid");
    }
    if (!Array.isArray(origin.localStorage)) {
      throw new Error("E2E storage state localStorage schema is invalid");
    }
    const localStorage = origin.localStorage.map((rawValue) => {
      const item = exactObject(rawValue, ["name", "value"]);
      if (!isString(item.name) || !isString(item.value)) {
        throw new Error("E2E storage state localStorage value is invalid");
      }
      return item as unknown as StorageValue;
    });
    return { origin: origin.origin, localStorage };
  });
  return { cookies, origins };
}

function exactObject(
  value: unknown,
  required: readonly string[],
  optional: readonly string[] = [],
): Record<string, unknown> {
  if (
    typeof value !== "object" ||
    value === null ||
    Array.isArray(value) ||
    Object.getPrototypeOf(value) !== Object.prototype
  ) {
    throw new Error("E2E storage state schema is invalid");
  }
  const object = value as Record<string, unknown>;
  const keys = Object.keys(object);
  if (
    required.some((key) => !Object.hasOwn(object, key)) ||
    keys.some((key) => !required.includes(key) && !optional.includes(key))
  ) {
    throw new Error("E2E storage state schema is invalid");
  }
  return object;
}

function isString(value: unknown): value is string {
  return typeof value === "string" && value.length <= 16_384;
}

function isExactOrigin(value: string): boolean {
  try {
    const parsed = new URL(value);
    return (
      parsed.protocol === "https:" &&
      parsed.origin === value &&
      !parsed.username &&
      !parsed.password
    );
  } catch {
    return false;
  }
}

function isFileSystemError(error: unknown, code: string): boolean {
  return (
    error instanceof Error &&
    "code" in error &&
    (error as NodeJS.ErrnoException).code === code
  );
}
