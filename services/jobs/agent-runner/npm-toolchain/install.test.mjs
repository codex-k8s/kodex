import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { gzipSync } from "node:zlib";
import {
  mkdtempSync,
  readFileSync,
  writeFileSync,
  rmSync,
  existsSync,
  symlinkSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";
import {
  checkVersion,
  commands,
  extractNpmSource,
  npmSourceEntries,
  securityFloors,
  verifyLock,
} from "./install.mjs";

const root = fileURLToPath(new URL("./", import.meta.url));
function archive(entries) {
  const parts = [];
  for (const [name, body, type = "0"] of entries) {
    const bytes = Buffer.from(body),
      header = Buffer.alloc(512);
    header.write(name, 0, 100);
    header.write("0000644\0", 100, 8);
    header.write(`${bytes.length.toString(8).padStart(11, "0")}\0`, 124, 12);
    header.fill(32, 148, 156);
    header.write(type, 156, 1);
    header.write("ustar\0", 257, 6);
    header.write(
      `${header
        .reduce((sum, byte) => sum + byte, 0)
        .toString(8)
        .padStart(6, "0")}\0 `,
      148,
      8,
    );
    parts.push(header, bytes, Buffer.alloc((512 - (bytes.length % 512)) % 512));
  }
  const bytes = gzipSync(Buffer.concat([...parts, Buffer.alloc(1024)]));
  return {
    bytes,
    integrity: `sha512-${createHash("sha512").update(bytes).digest("base64")}`,
  };
}
const base = [
  ["package/bin/npm-cli.js", "upstream npm CLI"],
  ["package/lib/cli.js", "upstream npm library"],
];
function temporary(action) {
  const directory = mkdtempSync(join(tmpdir(), "kodex-npm-unit-"));
  try {
    return action(directory);
  } finally {
    rmSync(directory, { recursive: true });
  }
}
for (const [name, floor] of Object.entries(securityFloors)) {
  test(`принимает exact fix ${name}, закрыто отклоняет прежнюю patch version`, () => {
    const old = floor.split(".").map(Number);
    if (old[2] > 0) old[2]--;
    else old[1]--;
    assert.throws(
      () => checkVersion(name, old.join(".")),
      /NPM_SECURITY_VERSION_REJECTED/,
    );
    assert.throws(
      () => checkVersion(name, `${floor}-rc.1`),
      /NPM_SECURITY_VERSION_REJECTED/,
    );
    assert.doesNotThrow(() => checkVersion(name, floor));
  });
}
test("сохраняет прежние CLI и закрытые manifest/lock без bundled deps", () => {
  assert.doesNotThrow(() => verifyLock(root));
  assert.doesNotThrow(() => verifyLock(join(root, "npm-cli"), true));
  for (const command of [
    "codex",
    "tsc",
    "vite",
    "vue-tsc",
    "vitest",
    "eslint",
    "prettier",
    "pnpm",
    "yarn",
    "openapi-ts",
    "wscat",
    "playwright",
    "playwright-mcp",
    "wait-on",
  ])
    assert.ok(commands[command]);
});
test("проверяет digest до использования source", () => {
  const { bytes } = archive(base);
  assert.throws(
    () => npmSourceEntries(bytes, `sha512-${"A".repeat(86)}==`),
    /NPM_SOURCE_INTEGRITY_MISMATCH/,
  );
});
for (const path of [
  "package/../outside",
  "/outside",
  "package/a/../../outside",
  "package/a\\outside",
  "package//outside",
]) {
  test(`отклоняет traversal ${path} до файлового эффекта`, () => {
    const { bytes, integrity } = archive([...base, [path, "unsafe"]]);
    assert.throws(
      () => npmSourceEntries(bytes, integrity),
      /NPM_ARCHIVE_PATH_REJECTED/,
    );
  });
}
for (const type of ["1", "2"]) {
  test(`не материализует source link type ${type}`, () => {
    const { bytes, integrity } = archive([
      ...base,
      ["package/evil", "unsafe", type],
    ]);
    assert.throws(
      () => npmSourceEntries(bytes, integrity),
      /NPM_ARCHIVE_LINK_REJECTED/,
    );
  });
}
test("не переносит bundled node_modules и upstream shrinkwrap в новый source project", () =>
  temporary((directory) => {
    const manifest = JSON.parse(
      readFileSync(join(root, "npm-cli/package.json")),
    );
    for (const file of ["package.json", "package-lock.json"])
      writeFileSync(
        join(directory, file),
        readFileSync(join(root, "npm-cli", file)),
      );
    const upstream = {
      ...manifest,
      dependencies: { ...manifest.dependencies, tar: "^7.5.22" },
    };
    const { bytes, integrity } = archive([
      ...base,
      ["package/package.json", JSON.stringify(upstream)],
      ["package/npm-shrinkwrap.json", "unpatched lock"],
      ["package/node_modules/brace-expansion/package.json", "unpatched bundle"],
    ]);
    extractNpmSource(directory, bytes, {
      name: "npm",
      version: "12.2.0",
      url: "https://registry.npmjs.org/npm/-/npm-12.2.0.tgz",
      integrity,
    });
    assert.equal(
      readFileSync(join(directory, "bin/npm-cli.js"), "utf8"),
      "upstream npm CLI",
    );
    assert.equal(existsSync(join(directory, "node_modules")), false);
    assert.equal(existsSync(join(directory, "npm-shrinkwrap.json")), false);
    assert.deepEqual(
      JSON.parse(readFileSync(join(directory, "package.json"))),
      manifest,
    );
  }));
test("отклоняет другую версию upstream до записи source", () =>
  temporary((directory) => {
    const manifest = JSON.parse(
      readFileSync(join(root, "npm-cli/package.json")),
    );
    for (const file of ["package.json", "package-lock.json"])
      writeFileSync(
        join(directory, file),
        readFileSync(join(root, "npm-cli", file)),
      );
    const upstream = { ...manifest, version: "11.21.0" };
    const { bytes, integrity } = archive([
      ...base,
      ["package/package.json", JSON.stringify(upstream)],
    ]);
    assert.throws(
      () =>
        extractNpmSource(directory, bytes, {
          name: "npm",
          version: "12.2.0",
          url: "https://registry.npmjs.org/npm/-/npm-12.2.0.tgz",
          integrity,
        }),
      /NPM_SOURCE_VERSION_MISMATCH/,
    );
    assert.equal(existsSync(join(directory, "bin")), false);
  }));

test("отклоняет stale lock и новый source в symlink destination", () =>
  temporary((directory) => {
    for (const file of ["package.json", "package-lock.json"])
      writeFileSync(
        join(directory, file),
        readFileSync(join(root, "npm-cli", file)),
      );
    const lock = JSON.parse(readFileSync(join(directory, "package-lock.json")));
    lock.packages[""].dependencies.tar = "7.5.16";
    writeFileSync(join(directory, "package-lock.json"), JSON.stringify(lock));
    assert.throws(() => verifyLock(directory, true), /NPM_LOCK_MISMATCH/);
    const alias = `${directory}-link`;
    symlinkSync(directory, alias);
    try {
      assert.throws(
        () =>
          extractNpmSource(
            alias,
            Buffer.alloc(0),
            JSON.parse(readFileSync(join(root, "npm-source.json"))),
          ),
        /NPM_SOURCE_DESTINATION_NOT_FRESH/,
      );
    } finally {
      rmSync(alias);
    }
  }));
