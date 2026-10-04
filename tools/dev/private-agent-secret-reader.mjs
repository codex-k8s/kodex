import { constants, openSync, fstatSync, readSync, closeSync } from "node:fs";
import { parseEnv } from "node:util";

export const agentSecretPath = "/home/s/.codex/agent-secrets.env";
export const allowedSecretKeys = Object.freeze([
  "KODEX_LOCAL_OWNER_USERNAME",
  "KODEX_LOCAL_OWNER_PASSWORD",
  "CONTEXT7_API_KEY",
  "CODEX_GITHUB_AGENT_INTEGRATION_TOKEN",
  "CODEX_GITHUB_AGENT_GIT_TOKEN",
]);
const maximumBytes = 1 << 20;

function invalid() {
  throw new Error("SECRET_INPUT_INVALID");
}

function requireSelectedKeys(keys) {
  if (
    !Array.isArray(keys) ||
    !keys.length ||
    keys.length > allowedSecretKeys.length ||
    new Set(keys).size !== keys.length ||
    keys.some((key) => !allowedSecretKeys.includes(key))
  )
    invalid();
}

// Один assignment разбирается штатным dotenv parser, без исполнения и expansion.
// В результат попадают только явно выбранные ключи; остальные не экспортируются.
export function parseSelectedAgentSecrets(source, keys) {
  if (
    typeof source !== "string" ||
    Buffer.byteLength(source) > maximumBytes ||
    source.includes("\0")
  )
    invalid();
  requireSelectedKeys(keys);
  const selected = Object.create(null);
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index];
    if (/^\s*(?:#.*)?$/.test(line)) continue;
    const match = /^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(
      line,
    );
    if (!match) invalid();
    let assignment = line;
    const [, key, raw] = match;
    if (["'", '"', "`"].includes(raw[0])) {
      const quote = raw[0];
      let tail = raw.slice(1);
      while (!tail.includes(quote)) {
        index++;
        if (index >= lines.length) invalid();
        assignment += `\n${lines[index]}`;
        tail += `\n${lines[index]}`;
      }
      if (!/^\s*(?:#.*)?$/.test(tail.slice(tail.indexOf(quote) + 1))) invalid();
    }
    if (!keys.includes(key)) continue;
    if (Object.hasOwn(selected, key)) invalid();
    const parsed = parseEnv(assignment);
    const value = parsed[key];
    if (
      Object.keys(parsed).length !== 1 ||
      typeof value !== "string" ||
      !value.length ||
      value.length > 16384 ||
      value.includes("\0")
    )
      invalid();
    selected[key] = value;
  }
  if (keys.some((key) => !Object.hasOwn(selected, key))) invalid();
  return selected;
}

// Dependency injection предназначен только для synthetic unit, путь неизменяем.
export function readPrivateAgentSecrets(keys, io = {}) {
  requireSelectedKeys(keys);
  const open = io.open ?? openSync;
  const stat = io.stat ?? fstatSync;
  const read = io.read ?? readSync;
  const close = io.close ?? closeSync;
  const uid = io.uid ?? process.getuid();
  let descriptor;
  let buffer;
  try {
    descriptor = open(
      agentSecretPath,
      constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK,
    );
    const info = stat(descriptor);
    if (
      !info.isFile() ||
      info.uid !== uid ||
      (info.mode & 0o777) !== 0o600 ||
      info.nlink !== 1 ||
      !Number.isSafeInteger(info.size) ||
      info.size <= 0 ||
      info.size > maximumBytes
    )
      invalid();
    buffer = Buffer.alloc(info.size + 1);
    let count = 0;
    while (count < buffer.length) {
      const part = read(descriptor, buffer, count, buffer.length - count, null);
      if (!part) break;
      count += part;
    }
    const after = stat(descriptor);
    if (
      count !== info.size ||
      after.size !== info.size ||
      after.ino !== info.ino ||
      after.dev !== info.dev ||
      after.mtimeMs !== info.mtimeMs ||
      after.ctimeMs !== info.ctimeMs
    )
      invalid();
    const source = buffer.subarray(0, count).toString("utf8");
    if (!Buffer.from(source, "utf8").equals(buffer.subarray(0, count)))
      invalid();
    return parseSelectedAgentSecrets(source, keys);
  } catch {
    invalid();
  } finally {
    buffer?.fill(0);
    if (descriptor !== undefined) {
      try {
        close(descriptor);
      } catch {
        invalid();
      }
    }
  }
}
