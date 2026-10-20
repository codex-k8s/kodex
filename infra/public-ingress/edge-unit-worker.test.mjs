import assert from "node:assert/strict";
import * as fs from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { replaceUnits } from "./edge-unit-worker.mjs";

function setup() {
  const root = fs.mkdtempSync(join(tmpdir(), "kodex-edge-units-")), before = {}, after = {};
  const path = value => typeof value === "string" ? value.replace("/etc/systemd/system", root) : value;
  const adapter = { ...fs, openSync: (p, ...args) => fs.openSync(path(p), ...args),
    renameSync: (a, b) => fs.renameSync(path(a), path(b)), unlinkSync: p => fs.unlinkSync(path(p)),
    fstatSync: fd => Object.create(fs.fstatSync(fd), { uid: { value: 0 } }) };
  for (const index of [0, 1]) for (const extension of ["socket", "service"]) {
    const name = `kodex-public-edge-https-${index}.${extension}`;
    before[name] = `# Kodex public edge plan before\n${name}\n`;
    after[name] = `# Kodex public edge plan after\n${name}\n`;
    fs.writeFileSync(join(root, name), before[name], { mode: 0o644 }); fs.chmodSync(join(root, name), 0o644);
  }
  return { root, before, after, adapter, close: () => fs.rmSync(root, { recursive: true, force: true }) };
}

test("atomic replacement preserves mode, supports exact resume and rejects a third state before any write", () => {
  const fixture = setup();
  try {
    const { root, before, after, adapter } = fixture;
    const name = Object.keys(before)[3]; fs.writeFileSync(join(root, name), "foreign");
    assert.throws(() => replaceUnits(JSON.stringify({ before, after }), adapter), /REJECTED/);
    assert.equal(fs.readFileSync(join(root, Object.keys(before)[0]), "utf8"), before[Object.keys(before)[0]]);
    fs.writeFileSync(join(root, name), before[name]);
    replaceUnits(JSON.stringify({ before, after }), adapter);
    replaceUnits(JSON.stringify({ before, after }), adapter);
    for (const name of Object.keys(after)) {
      assert.equal(fs.readFileSync(join(root, name), "utf8"), after[name]);
      assert.equal(fs.statSync(join(root, name)).mode & 0o777, 0o644);
    }
    assert.equal(fs.readdirSync(root).length, 4);
  } finally { fixture.close(); }
});

test("replacement rejects symlinks, hardlinks and path traversal", () => {
  for (const kind of ["symlink", "hardlink", "path"]) {
    const fixture = setup();
    try {
      const { root, before, after, adapter } = fixture, name = Object.keys(before)[0];
      if (kind === "path") { before["../../foreign"] = "x"; after["../../foreign"] = "x"; }
      else if (kind === "hardlink") fs.linkSync(join(root, name), join(root, "linked"));
      else { fs.unlinkSync(join(root, name)); fs.symlinkSync(join(root, Object.keys(before)[1]), join(root, name)); }
      assert.throws(() => replaceUnits(JSON.stringify({ before, after }), adapter));
      assert.equal(fs.readFileSync(join(root, Object.keys(before)[2]), "utf8"), before[Object.keys(before)[2]]);
    } finally { fixture.close(); }
  }
});
