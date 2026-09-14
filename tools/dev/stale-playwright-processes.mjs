#!/usr/bin/env node

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import {
  chmodSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  readlinkSync,
  readdirSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { homedir, platform, uptime, userInfo } from "node:os";
import { basename, dirname, isAbsolute, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const confirmation = "CLEAN_STALE_PLAYWRIGHT";
const defaultMinimumAgeSeconds = 3600;
const minimumAllowedAgeSeconds = 300;

function fail(message) {
  throw new Error(message);
}

function sha256(value) {
  return createHash("sha256").update(value).digest("hex");
}

function safeOutputPath(raw, label) {
  if (!raw || !isAbsolute(raw)) fail(`${label} must be an absolute path`);
  return resolve(raw);
}

function writePrivateJSON(path, value) {
  mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
  const temporary = `${path}.${process.pid}.tmp`;
  try {
    writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, {
      encoding: "utf8",
      mode: 0o600,
      flag: "wx",
    });
    chmodSync(temporary, 0o600);
    renameSync(temporary, path);
  } finally {
    rmSync(temporary, { force: true });
  }
}

function readPrivateJSON(path, label) {
  const info = lstatSync(path);
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) {
    fail(`${label} must be a regular non-symlink file readable only by its owner`);
  }
  return JSON.parse(readFileSync(path, "utf8"));
}

function processStat(raw) {
  const close = raw.lastIndexOf(")");
  if (close < 0) fail("process stat is malformed");
  const fields = raw.slice(close + 2).trim().split(/\s+/);
  if (fields.length < 20) fail("process stat is incomplete");
  return {
    ppid: Number(fields[1]),
    sessionId: Number(fields[3]),
    startTicks: fields[19],
  };
}

function clockTicks() {
  const raw = execFileSync("getconf", ["CLK_TCK"], { encoding: "utf8" }).trim();
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value < 1) fail("system clock tick rate is invalid");
  return value;
}

function snapshotProcesses() {
  if (platform() !== "linux") fail("stale Playwright cleanup is supported only on Linux");
  const uid = process.getuid?.();
  if (!Number.isSafeInteger(uid)) fail("current uid is unavailable");
  const ticks = clockTicks();
  const systemUptime = uptime();
  const snapshots = [];
  for (const entry of readdirSync("/proc")) {
    if (!/^\d+$/.test(entry)) continue;
    const pid = Number(entry);
    const root = `/proc/${entry}`;
    try {
      if (statSync(root).uid !== uid) continue;
      const stat = processStat(readFileSync(`${root}/stat`, "utf8"));
      const executable = readlinkSync(`${root}/exe`);
      const argv = readFileSync(`${root}/cmdline`)
        .toString("utf8")
        .split("\0")
        .filter(Boolean);
      const ageSeconds = Math.max(0, Math.floor(systemUptime - Number(stat.startTicks) / ticks));
      snapshots.push({
        pid,
        ...stat,
        ageSeconds,
        executable,
        argv,
        cmdlineSHA256: sha256(argv.join("\0")),
      });
    } catch (error) {
      if (error?.code === "ENOENT" || error?.code === "ESRCH" || error?.code === "EACCES") continue;
      throw error;
    }
  }
  return snapshots;
}

function isPlaywrightBrowser(item, home) {
  const cacheRoot = `${home}/.cache/ms-playwright/`;
  return (
    item.executable.startsWith(cacheRoot) &&
    basename(item.executable) === "chrome-headless-shell" &&
    item.argv.some((argument) =>
      /^--user-data-dir=\/tmp\/playwright_chromiumdev_profile-[A-Za-z0-9_-]+$/.test(argument),
    )
  );
}

function processIdentity(item) {
  return {
    pid: item.pid,
    ppid: item.ppid,
    sessionId: item.sessionId,
    startTicks: item.startTicks,
    ageSeconds: item.ageSeconds,
    executable: item.executable,
    cmdlineSHA256: item.cmdlineSHA256,
  };
}

export function selectStaleTrees(processes, home, minimumAgeSeconds) {
  const byPID = new Map(processes.map((item) => [item.pid, item]));
  const roots = new Map();
  for (const browser of processes) {
    if (browser.ageSeconds < minimumAgeSeconds || !isPlaywrightBrowser(browser, home)) continue;
    let root = browser;
    while (byPID.has(root.ppid)) {
      const parent = byPID.get(root.ppid);
      if (parent.ageSeconds < minimumAgeSeconds) break;
      root = parent;
    }
    if (root.ppid !== 1) continue;
    const current = roots.get(root.pid) ?? { root, browserPIDs: [] };
    current.browserPIDs.push(browser.pid);
    roots.set(root.pid, current);
  }
  return [...roots.values()]
    .sort((left, right) => left.root.pid - right.root.pid)
    .map(({ root, browserPIDs }) => ({
      root: processIdentity(root),
      browserPIDs: [...new Set(browserPIDs)].sort((left, right) => left - right),
    }));
}

function parseArguments(argv) {
  const [command, ...rest] = argv;
  const options = { command, minimumAgeSeconds: defaultMinimumAgeSeconds };
  for (let index = 0; index < rest.length; index += 2) {
    const key = rest[index];
    const value = rest[index + 1];
    if (!value) fail(`missing value for ${key ?? "argument"}`);
    if (key === "--output") options.output = safeOutputPath(value, "output");
    else if (key === "--plan") options.plan = safeOutputPath(value, "plan");
    else if (key === "--evidence") options.evidence = safeOutputPath(value, "evidence");
    else if (key === "--confirm") options.confirm = value;
    else if (key === "--minimum-age-seconds") options.minimumAgeSeconds = Number(value);
    else fail(`unsupported argument: ${key}`);
  }
  if (
    !Number.isSafeInteger(options.minimumAgeSeconds) ||
    options.minimumAgeSeconds < minimumAllowedAgeSeconds
  ) {
    fail(`minimum age must be at least ${minimumAllowedAgeSeconds} seconds`);
  }
  return options;
}

function exactIdentity(expected, actual) {
  return (
    actual &&
    expected.pid === actual.pid &&
    expected.startTicks === actual.startTicks &&
    expected.executable === actual.executable &&
    expected.cmdlineSHA256 === actual.cmdlineSHA256
  );
}

function descendants(processes, rootPID) {
  const children = new Map();
  for (const item of processes) {
    const values = children.get(item.ppid) ?? [];
    values.push(item);
    children.set(item.ppid, values);
  }
  const found = [];
  const pending = [rootPID];
  const visited = new Set();
  while (pending.length > 0) {
    const pid = pending.pop();
    if (visited.has(pid)) continue;
    visited.add(pid);
    const item = processes.find((candidate) => candidate.pid === pid);
    if (item) found.push(item);
    for (const child of children.get(pid) ?? []) pending.push(child.pid);
  }
  return found;
}

function aliveWithIdentity(expected) {
  return snapshotProcesses().find((item) => exactIdentity(expected, item));
}

async function waitForExit(targets, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let alive = targets.filter(aliveWithIdentity);
  while (alive.length > 0 && Date.now() < deadline) {
    await new Promise((resolvePromise) => setTimeout(resolvePromise, 100));
    alive = targets.filter(aliveWithIdentity);
  }
  return alive;
}

async function plan(options) {
  if (!options.output || options.plan || options.evidence || options.confirm) {
    fail("plan requires only --output and optional --minimum-age-seconds");
  }
  const processes = snapshotProcesses();
  const document = {
    version: 1,
    status: "PLANNED",
    generatedAt: new Date().toISOString(),
    username: userInfo().username,
    home: homedir(),
    minimumAgeSeconds: options.minimumAgeSeconds,
    candidates: selectStaleTrees(processes, homedir(), options.minimumAgeSeconds),
  };
  writePrivateJSON(options.output, document);
  console.log(`Stale Playwright cleanup plan: ${document.candidates.length} process trees`);
}

async function apply(options) {
  if (!options.plan || !options.evidence || options.output || options.confirm !== confirmation) {
    fail(`apply requires --plan, --evidence and --confirm ${confirmation}`);
  }
  const document = readPrivateJSON(options.plan, "plan");
  if (
    document.version !== 1 ||
    document.status !== "PLANNED" ||
    document.username !== userInfo().username ||
    document.home !== homedir() ||
    !Number.isSafeInteger(document.minimumAgeSeconds) ||
    document.minimumAgeSeconds < minimumAllowedAgeSeconds ||
    !Array.isArray(document.candidates)
  ) {
    fail("plan has an unsupported or foreign schema");
  }
  const current = snapshotProcesses();
  const selected = selectStaleTrees(current, document.home, document.minimumAgeSeconds);
  const selectedByRoot = new Map(selected.map((candidate) => [candidate.root.pid, candidate]));
  const stopped = [];
  const missing = [];
  const forced = [];
  for (const candidate of document.candidates) {
    const actual = selectedByRoot.get(candidate.root.pid);
    if (!actual || !exactIdentity(candidate.root, actual.root)) {
      missing.push(candidate.root.pid);
      continue;
    }
    const tree = descendants(snapshotProcesses(), candidate.root.pid).map(processIdentity);
    for (const target of tree) {
      try {
        process.kill(target.pid, "SIGTERM");
      } catch (error) {
        if (error?.code !== "ESRCH") throw error;
      }
    }
    const survivors = await waitForExit(tree, 5000);
    for (const target of survivors) {
      try {
        process.kill(target.pid, "SIGKILL");
        forced.push(target.pid);
      } catch (error) {
        if (error?.code !== "ESRCH") throw error;
      }
    }
    const finalSurvivors = await waitForExit(survivors, 2000);
    if (finalSurvivors.length > 0) fail("a pinned stale Playwright process survived cleanup");
    stopped.push(candidate.root.pid);
  }
  const evidence = {
    version: 1,
    status: "APPLIED",
    appliedAt: new Date().toISOString(),
    planSHA256: sha256(readFileSync(options.plan)),
    stoppedRootPIDs: stopped,
    missingRootPIDs: missing,
    forcedPIDs: forced,
    remainingCandidates: selectStaleTrees(
      snapshotProcesses(),
      document.home,
      document.minimumAgeSeconds,
    ).length,
  };
  writePrivateJSON(options.evidence, evidence);
  console.log(
    `Stale Playwright cleanup applied: stopped=${stopped.length} missing=${missing.length} remaining=${evidence.remainingCandidates}`,
  );
}

async function main() {
  const options = parseArguments(process.argv.slice(2));
  if (options.command === "plan") await plan(options);
  else if (options.command === "apply") await apply(options);
  else fail("command must be plan or apply");
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  main().catch((error) => {
    console.error(`Stale Playwright cleanup failed: ${error.message}`);
    process.exitCode = 1;
  });
}
