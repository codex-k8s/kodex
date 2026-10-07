import { createHash } from "node:crypto";
import { gunzipSync } from "node:zlib";
import {
  readFileSync,
  readdirSync,
  lstatSync,
  mkdirSync,
  writeFileSync,
  realpathSync,
  symlinkSync,
  unlinkSync,
} from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const MAX_ARCHIVE = 8 * 1024 * 1024;
const MAX_EXPANDED = 32 * 1024 * 1024;
const VERSION = /^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/;
export const securityFloors = {
  "brace-expansion": "5.0.11",
  "ip-address": "10.3.1",
  "js-yaml": "4.3.2",
  pnpm: "11.11.0",
  tar: "7.5.21",
  undici: "6.28.1",
};
export const commands = {
  codex: "@openai/codex",
  tsc: "typescript",
  tsserver: "typescript",
  "create-vue": "create-vue",
  vite: "vite",
  "vue-tsc": "vue-tsc",
  vitest: "vitest",
  eslint: "eslint",
  prettier: "prettier",
  pnpm: "pnpm",
  pnpx: "pnpm",
  pn: "pnpm",
  pnx: "pnpm",
  "openapi-ts": "@hey-api/openapi-ts",
  wscat: "wscat",
  playwright: "playwright",
  "playwright-mcp": "@playwright/mcp",
  "wait-on": "wait-on",
  yarn: "yarn",
  yarnpkg: "yarn",
};
const requiredPackages = [
  ...new Set([...Object.values(commands), "vue", "@playwright/test"]),
].sort();
function fail(code) {
  throw new Error(code);
}
function json(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}
function entries(value) {
  return JSON.stringify(Object.entries(value ?? {}).sort());
}
export function checkVersion(name, version) {
  if (!VERSION.test(version ?? "")) fail("NPM_VERSION_INVALID");
  const floor = securityFloors[name];
  if (!floor || version.split(".")[0] !== floor.split(".")[0]) return;
  const actual = version.split(".").map(Number),
    minimum = floor.split(".").map(Number);
  for (let index = 0; index < 3; index++) {
    if (actual[index] < minimum[index]) fail("NPM_SECURITY_VERSION_REJECTED");
    if (actual[index] > minimum[index]) return;
  }
  if (version.includes("-")) fail("NPM_SECURITY_VERSION_REJECTED");
}
export function verifyLock(root, sourceNpm = false) {
  const manifest = json(join(root, "package.json")),
    lock = json(join(root, "package-lock.json"));
  if (
    lock.lockfileVersion !== 3 ||
    !lock.packages ||
    !manifest.private ||
    entries(lock.packages[""].dependencies) !== entries(manifest.dependencies)
  )
    fail("NPM_LOCK_MISMATCH");
  if (
    !sourceNpm &&
    JSON.stringify(Object.keys(manifest.dependencies).sort()) !==
      JSON.stringify(requiredPackages)
  )
    fail("NPM_TOOLCHAIN_PACKAGE_SET_INVALID");
  if (
    sourceNpm &&
    (manifest.name !== "npm" ||
      manifest.version !== "12.2.0" ||
      manifest.bundleDependencies ||
      manifest.bundledDependencies)
  )
    fail("NPM_SOURCE_MANIFEST_INVALID");
  for (const [path, entry] of Object.entries(lock.packages)) {
    if (!path) continue;
    if (
      !/^node_modules\/(?:[a-zA-Z0-9@_.+-]+\/)*[a-zA-Z0-9_.+-]+$/.test(path) ||
      entry.inBundle ||
      entry.link ||
      !/^https:\/\/registry\.npmjs\.org\//.test(entry.resolved ?? "") ||
      !/^sha512-[a-zA-Z0-9+/]+={0,2}$/.test(entry.integrity ?? "")
    )
      fail("NPM_LOCK_SOURCE_INVALID");
    checkVersion(path.split("node_modules/").at(-1), entry.version);
  }
  for (const [name, version] of Object.entries(manifest.dependencies)) {
    if (!sourceNpm && !VERSION.test(version))
      fail("NPM_DIRECT_VERSION_NOT_PINNED");
    if (!lock.packages[`node_modules/${name}`])
      fail("NPM_LOCK_DEPENDENCY_MISSING");
  }
  return manifest;
}
function tarString(buffer) {
  return buffer.toString("utf8").replace(/\0.*$/s, "");
}
function octal(buffer) {
  const text = tarString(buffer).trim();
  if (!/^[0-7]+$/.test(text)) fail("NPM_ARCHIVE_HEADER_INVALID");
  return Number.parseInt(text, 8);
}
export function npmSourceEntries(archive, integrity) {
  if (
    archive.length > MAX_ARCHIVE ||
    createHash("sha512").update(archive).digest("base64") !==
      integrity.replace(/^sha512-/, "")
  )
    fail("NPM_SOURCE_INTEGRITY_MISMATCH");
  const tar = gunzipSync(archive, { maxOutputLength: MAX_EXPANDED });
  const files = [],
    seen = new Set();
  let pendingPath;
  for (let offset = 0; offset + 512 <= tar.length; ) {
    const header = tar.subarray(offset, offset + 512);
    offset += 512;
    if (header.every((byte) => byte === 0)) break;
    const checksum = octal(header.subarray(148, 156));
    const actualChecksum = header.reduce(
      (sum, byte, index) => sum + (index >= 148 && index < 156 ? 32 : byte),
      0,
    );
    const size = octal(header.subarray(124, 136)),
      mode = octal(header.subarray(100, 108)),
      type = String.fromCharCode(header[156]);
    if (checksum !== actualChecksum || offset + size > tar.length)
      fail("NPM_ARCHIVE_HEADER_INVALID");
    const body = tar.subarray(offset, offset + size);
    offset += Math.ceil(size / 512) * 512;
    const prefix = tarString(header.subarray(345, 500));
    let name =
      pendingPath ??
      [prefix, tarString(header.subarray(0, 100))].filter(Boolean).join("/");
    pendingPath = undefined;
    if (type === "L") {
      pendingPath = tarString(body).replace(/\n$/, "");
      continue;
    }
    if (type === "x") {
      let cursor = 0;
      while (cursor < body.length) {
        const separator = body.indexOf(32, cursor),
          length = Number(body.subarray(cursor, separator).toString());
        if (
          separator < cursor ||
          !Number.isSafeInteger(length) ||
          length < 4 ||
          cursor + length > body.length ||
          body[cursor + length - 1] !== 10
        )
          fail("NPM_ARCHIVE_HEADER_INVALID");
        const record = body
          .subarray(separator + 1, cursor + length - 1)
          .toString("utf8");
        if (record.startsWith("path=")) pendingPath = record.slice(5);
        if (record.startsWith("linkpath=")) fail("NPM_ARCHIVE_LINK_REJECTED");
        cursor += length;
      }
      continue;
    }
    name = name.replace(/\/$/, "");
    if (name === "package" && type === "5") continue;
    if (
      !name.startsWith("package/") ||
      name.includes("\\") ||
      name.includes("\0") ||
      name.split("/").some((part) => !part || part === ".." || part === ".")
    )
      fail("NPM_ARCHIVE_PATH_REJECTED");
    const path = name.slice(8);
    if (path === "node_modules" || path.startsWith("node_modules/")) continue;
    if (type !== "0" && type !== "\0" && type !== "5")
      fail("NPM_ARCHIVE_LINK_REJECTED");
    if (type === "5") continue;
    if (seen.has(path)) fail("NPM_ARCHIVE_DUPLICATE_REJECTED");
    seen.add(path);
    files.push({ path, body, mode: mode & 0o111 ? 0o755 : 0o644 });
  }
  if (
    !files.some((file) => file.path === "bin/npm-cli.js") ||
    !files.some((file) => file.path === "lib/cli.js")
  )
    fail("NPM_SOURCE_INCOMPLETE");
  return files;
}
export function extractNpmSource(root, archive, source) {
  if (
    source.name !== "npm" ||
    source.version !== "12.2.0" ||
    source.url !==
      `https://registry.npmjs.org/npm/-/npm-${source.version}.tgz` ||
    !/^sha512-[a-zA-Z0-9+/]{86}==$/.test(source.integrity)
  )
    fail("NPM_SOURCE_DESCRIPTOR_INVALID");
  if (
    lstatSync(root).isSymbolicLink() ||
    readdirSync(root).some(
      (name) => !["package.json", "package-lock.json"].includes(name),
    )
  )
    fail("NPM_SOURCE_DESTINATION_NOT_FRESH");
  const manifest = verifyLock(root, true),
    files = npmSourceEntries(archive, source.integrity);
  const upstream = JSON.parse(
    files.find((file) => file.path === "package.json")?.body.toString("utf8") ??
      "null",
  );
  if (
    upstream?.name !== source.name ||
    upstream?.version !== source.version ||
    entries(upstream.engines) !== entries(manifest.engines)
  )
    fail("NPM_SOURCE_VERSION_MISMATCH");
  const dependencies = { ...upstream.dependencies, tar: "7.5.22" };
  if (
    JSON.stringify(Object.entries(dependencies).sort()) !==
    JSON.stringify(Object.entries(manifest.dependencies).sort())
  )
    fail("NPM_SOURCE_DEPENDENCIES_MISMATCH");
  for (const file of files) {
    if (
      ["package.json", "package-lock.json", "npm-shrinkwrap.json"].includes(
        file.path,
      )
    )
      continue;
    const destination = join(root, file.path);
    mkdirSync(dirname(destination), { recursive: true });
    writeFileSync(destination, file.body, { flag: "wx", mode: file.mode });
  }
}
export function verifyInstalled(root, sourceNpm = false) {
  const manifest = verifyLock(root, sourceNpm),
    versions = {};
  let visited = 0;
  function walk(directory) {
    for (const entry of readdirSync(directory, { withFileTypes: true })) {
      if (entry.name.startsWith(".")) continue;
      const path = join(directory, entry.name);
      if (!entry.isDirectory() || entry.isSymbolicLink())
        fail("NPM_INSTALLED_LINK_REJECTED");
      if (entry.name.startsWith("@")) {
        walk(path);
        continue;
      }
      if (++visited > 2000) fail("NPM_INSTALLED_TREE_EXCEEDS_BOUND");
      const pkg = json(join(path, "package.json"));
      checkVersion(pkg.name, pkg.version);
      if (securityFloors[pkg.name])
        (versions[pkg.name] ??= new Set()).add(pkg.version);
      if (readdirSync(path).includes("node_modules"))
        walk(join(path, "node_modules"));
    }
  }
  walk(join(root, "node_modules"));
  for (const [name, requested] of Object.entries(manifest.dependencies)) {
    const installed = json(join(root, "node_modules", name, "package.json"));
    const locked = json(join(root, "package-lock.json")).packages[
      `node_modules/${name}`
    ].version;
    if (
      installed.version !== locked ||
      (!sourceNpm && installed.version !== requested)
    )
      fail("NPM_INSTALLED_VERSION_MISMATCH");
  }
  return Object.fromEntries(
    Object.entries(versions).map(([name, list]) => [name, [...list].sort()]),
  );
}
function link(target, destination) {
  mkdirSync(dirname(destination), { recursive: true });
  try {
    if (!lstatSync(destination).isSymbolicLink()) fail("NPM_PUBLISH_COLLISION");
    unlinkSync(destination);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  symlinkSync(relative(dirname(destination), target), destination);
}
function binTarget(root, name, command) {
  const directory = join(root, "node_modules", name),
    pkg = json(join(directory, "package.json"));
  const path = typeof pkg.bin === "string" ? pkg.bin : pkg.bin?.[command];
  if (
    typeof path !== "string" ||
    path.startsWith("/") ||
    path.split("/").includes("..")
  )
    fail("NPM_BIN_TARGET_INVALID");
  const target = realpathSync(join(directory, path));
  if (!target.startsWith(realpathSync(directory) + "/"))
    fail("NPM_BIN_TARGET_INVALID");
  return target;
}
export function publishLinks(
  root,
  globalModules,
  globalBin,
  npmRoot = join(root, "npm-cli"),
) {
  const manifest = verifyLock(root);
  verifyInstalled(root);
  verifyInstalled(npmRoot, true);
  for (const name of Object.keys(manifest.dependencies))
    link(join(root, "node_modules", name), join(globalModules, name));
  link(npmRoot, join(globalModules, "npm"));
  for (const [command, name] of Object.entries(commands))
    link(binTarget(root, name, command), join(globalBin, command));
  for (const command of ["npm", "npx"])
    link(join(npmRoot, "bin", `${command}-cli.js`), join(globalBin, command));
  if (process.platform !== "linux" || !["x64", "arm64"].includes(process.arch))
    fail("NPM_CODEX_PLATFORM_UNSUPPORTED");
  const nativeDirectory = join(
    root,
    "node_modules",
    "@openai",
    `codex-linux-${process.arch}`,
  );
  const triple =
    process.arch === "x64"
      ? "x86_64-unknown-linux-musl"
      : "aarch64-unknown-linux-musl";
  const native = realpathSync(
    join(nativeDirectory, "vendor", triple, "bin", "codex-code-mode-host"),
  );
  if (
    !native.startsWith(realpathSync(nativeDirectory) + "/") ||
    !lstatSync(native).isFile() ||
    !(lstatSync(native).mode & 0o111)
  )
    fail("NPM_CODEX_NATIVE_TARGET_INVALID");
  link(native, join(globalBin, "codex-code-mode-host"));
}
async function main() {
  const root = dirname(fileURLToPath(import.meta.url)),
    action = process.argv[2];
  if (process.argv.length !== 3 || root !== "/opt/kodex/npm-toolchain")
    fail("NPM_INSTALL_ARGUMENTS_INVALID");
  if (action === "prepare-npm") {
    verifyLock(root);
    verifyLock(join(root, "npm-cli"), true);
    const source = json(join(root, "npm-source.json"));
    if (source.url !== "https://registry.npmjs.org/npm/-/npm-12.2.0.tgz")
      fail("NPM_SOURCE_DESCRIPTOR_INVALID");
    const response = await fetch(source.url, {
      redirect: "error",
      signal: AbortSignal.timeout(120000),
    });
    if (!response.ok) fail("NPM_SOURCE_FETCH_FAILED");
    const parts = [];
    let size = 0;
    for await (const part of response.body) {
      size += part.length;
      if (size > MAX_ARCHIVE) fail("NPM_SOURCE_EXCEEDS_BOUND");
      parts.push(part);
    }
    extractNpmSource(join(root, "npm-cli"), Buffer.concat(parts), source);
  } else if (action === "verify") {
    console.log(
      JSON.stringify({
        tools: verifyInstalled(root),
        npm: verifyInstalled(join(root, "npm-cli"), true),
      }),
    );
  } else if (action === "publish-links")
    publishLinks(root, "/usr/local/lib/node_modules", "/usr/local/bin");
  else fail("NPM_INSTALL_ARGUMENTS_INVALID");
}
if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
)
  main().catch(() => {
    console.error("NPM_TOOLCHAIN_INSTALL_FAILED");
    process.exitCode = 1;
  });
