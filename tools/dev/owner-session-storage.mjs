import { randomUUID } from "node:crypto";
import {
  closeSync,
  constants,
  fstatSync,
  fsyncSync,
  openSync,
  readSync,
  realpathSync,
  renameSync,
  unlinkSync,
  writeSync,
} from "node:fs";
import { basename, dirname, isAbsolute } from "node:path";

const maximumBytes = 1 << 20;
export const sessionCookieNames = ["__Host-kodex-session", "__Host-kodex-csrf"];
export const proxyCookieName = "_kodex_control_center_oauth2";
export const maximumProxyCookies = 4;
export const maximumProxyAge = 8 * 60 * 60;

// Имена OAuth2 CSRF относятся только к login, а не к authenticated API transport.
export function isProxySessionCookie(name) {
  return (
    typeof name === "string" &&
    (name === proxyCookieName ||
      (name.startsWith(`${proxyCookieName}_`) &&
        name !== `${proxyCookieName}_csrf` &&
        !name.startsWith(`${proxyCookieName}_csrf_`)))
  );
}

export function proxySessionCookies(cookies, origin, now) {
  const selected = cookies.filter((cookie) =>
    isProxySessionCookie(cookie?.name),
  );
  requireSession(
    selected.length <= maximumProxyCookies,
    "proxy cookie count is invalid",
  );
  selected.sort((left, right) => left.name.localeCompare(right.name));
  for (const [index, cookie] of selected.entries()) {
    requireSession(
      cookie.name ===
        (selected.length === 1
          ? proxyCookieName
          : `${proxyCookieName}_${index}`) &&
        cookie.domain === new URL(origin).hostname &&
        cookie.path === "/" &&
        cookie.secure === true &&
        cookie.httpOnly === true &&
        cookie.sameSite === "Lax" &&
        cookie.partitionKey === undefined &&
        Number.isFinite(cookie.expires) &&
        cookie.expires * 1000 > now &&
        cookie.expires * 1000 <= now + maximumProxyAge * 1000 &&
        typeof cookie.value === "string" &&
        cookie.value.length > 0 &&
        cookie.value.length <= 4096 &&
        /^[\x21\x23-\x2b\x2d-\x3a\x3c-\x5b\x5d-\x7e]+$/.test(cookie.value),
      "proxy cookie boundary is invalid",
    );
  }
  return selected;
}

export function requireSession(condition, message) {
  if (!condition) throw new Error(`Owner session acceptance ${message}`);
}

export function exactOrigin(value) {
  const url = new URL(value);
  requireSession(
    url.protocol === "https:" &&
      url.pathname === "/" &&
      !url.username &&
      !url.password &&
      !url.search &&
      !url.hash &&
      !/prod(?:uction)?/i.test(url.hostname),
    "origin is invalid",
  );
  return url.origin;
}

export function selectedSessionCookies(storage, origin, now = Date.now()) {
  origin = exactOrigin(origin);
  requireSession(Array.isArray(storage?.cookies), "cookies are absent");
  const hostname = new URL(origin).hostname;
  const selected = sessionCookieNames.map((name) => {
    const candidates = storage.cookies.filter(
      (cookie) => cookie?.name === name,
    );
    requireSession(candidates.length === 1, "cookie is ambiguous");
    const cookie = candidates[0];
    requireSession(
      cookie.domain === hostname &&
        cookie.path === "/" &&
        cookie.secure === true &&
        cookie.sameSite === "Strict" &&
        cookie.httpOnly === (name === sessionCookieNames[0]) &&
        cookie.partitionKey === undefined &&
        Number.isFinite(cookie.expires) &&
        (cookie.expires === -1 || cookie.expires * 1000 > now) &&
        typeof cookie.value === "string" &&
        (name === sessionCookieNames[0]
          ? /^v1\.[A-Za-z0-9_-]+$/.test(cookie.value)
          : /^[A-Za-z0-9_-]{43}$/.test(cookie.value)) &&
        cookie.value.length >= 43 &&
        cookie.value.length <= (name === sessionCookieNames[0] ? 4096 : 43),
      "cookie boundary is invalid",
    );
    return cookie;
  });
  return [...selected, ...proxySessionCookies(storage.cookies, origin, now)];
}

export function sessionHeaders(storage, origin, now = Date.now()) {
  origin = exactOrigin(origin);
  const selected = selectedSessionCookies(storage, origin, now);
  return {
    Accept: "application/json",
    Origin: origin,
    Cookie: selected
      .map((cookie) => `${cookie.name}=${cookie.value}`)
      .join("; "),
    "X-CSRF-Token": selected[1].value,
  };
}

// Browser handoff не принимает provider credential и не переносит IdP state.
export async function replaceAuthenticatedCookies(
  context,
  origin,
  previous,
  cookies,
) {
  try {
    origin = exactOrigin(origin);
    const now = Date.now();
    const before = selectedSessionCookies(previous, origin, now);
    const next = selectedSessionCookies({ cookies }, origin, now);
    requireSession(
      next.length === cookies.length,
      "cookie handoff contains foreign state",
    );
    const fingerprint = (items) =>
      JSON.stringify(
        items.map((cookie) => [
          cookie.name,
          cookie.value,
          cookie.domain,
          cookie.path,
          cookie.expires,
          cookie.secure,
          cookie.httpOnly,
          cookie.sameSite,
        ]),
      );
    const current = selectedSessionCookies(
      { cookies: await context.cookies() },
      origin,
      now,
    );
    requireSession(
      fingerprint(current) === fingerprint(before),
      "browser session changed during Node-only authorization",
    );
    for (const cookie of before) {
      if (!next.some((item) => item.name === cookie.name))
        await context.clearCookies({
          name: cookie.name,
          domain: cookie.domain,
          path: cookie.path,
        });
    }
    await context.addCookies(structuredClone(next));
    const observed = selectedSessionCookies(
      { cookies: await context.cookies() },
      origin,
      Date.now(),
    );
    requireSession(
      fingerprint(observed) === fingerprint(next),
      "browser cookie readback changed",
    );
  } catch {
    throw new Error("Owner session acceptance browser handoff failed");
  }
}

function openDirectory(path) {
  requireSession(
    isAbsolute(path) && realpathSync(dirname(path)) === dirname(path),
    "storage path is invalid",
  );
  const descriptor = openSync(
    dirname(path),
    constants.O_RDONLY | constants.O_DIRECTORY | constants.O_NOFOLLOW,
  );
  const info = fstatSync(descriptor);
  if (
    !info.isDirectory() ||
    info.uid !== process.getuid() ||
    (info.mode & 0o777) !== 0o700
  ) {
    closeSync(descriptor);
    requireSession(false, "storage directory is not private");
  }
  return descriptor;
}

function validateFile(info, allowEmpty = false) {
  requireSession(
    info.isFile() &&
      info.nlink === 1 &&
      info.uid === process.getuid() &&
      (info.mode & 0o077) === 0 &&
      (allowEmpty || info.size > 0) &&
      info.size <= maximumBytes,
    "storage file is invalid",
  );
}

// Snapshot уже аутентифицирован браузером; bootstrap reader PWA его не читает.
export function readAuthenticatedState(path) {
  const directory = openDirectory(path);
  try {
    const input = openSync(
      path,
      constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
    );
    try {
      const info = fstatSync(input);
      validateFile(info);
      const buffer = Buffer.alloc(maximumBytes + 1);
      try {
        let size = 0;
        while (size < buffer.length) {
          const count = readSync(
            input,
            buffer,
            size,
            buffer.length - size,
            null,
          );
          if (!count) break;
          size += count;
        }
        requireSession(size === info.size, "storage size changed");
        try {
          return JSON.parse(buffer.subarray(0, size).toString("utf8"));
        } catch {
          throw new Error("Owner session acceptance storage JSON is invalid");
        }
      } finally {
        buffer.fill(0);
      }
    } finally {
      closeSync(input);
    }
  } finally {
    closeSync(directory);
  }
}

// Только owner-private каталог; rename не проходит через symlink цели.
export function writeAuthenticatedState(path, value) {
  const directory = openDirectory(path);
  const temporary = `${dirname(path)}/.${basename(path)}.${randomUUID()}.tmp`;
  let created = false;
  const payload = Buffer.from(`${JSON.stringify(value)}\n`);
  try {
    requireSession(
      payload.length > 0 && payload.length <= maximumBytes,
      "storage size is invalid",
    );
    try {
      readAuthenticatedState(path);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    const output = openSync(
      temporary,
      constants.O_WRONLY |
        constants.O_CREAT |
        constants.O_EXCL |
        constants.O_NOFOLLOW,
      0o600,
    );
    created = true;
    try {
      validateFile(fstatSync(output), true);
      let offset = 0;
      while (offset < payload.length) {
        const size = writeSync(
          output,
          payload,
          offset,
          payload.length - offset,
        );
        requireSession(size > 0, "storage write is incomplete");
        offset += size;
      }
      fsyncSync(output);
    } finally {
      closeSync(output);
    }
    renameSync(temporary, path);
    created = false;
    fsyncSync(directory);
    requireSession(
      JSON.stringify(readAuthenticatedState(path)) === JSON.stringify(value),
      "storage readback changed",
    );
  } finally {
    payload.fill(0);
    if (created) unlinkSync(temporary);
    closeSync(directory);
  }
}
