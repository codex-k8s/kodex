#!/usr/bin/env node
import * as filesystem from "node:fs";
import { pathToFileURL } from "node:url";

// Privileged helper принимает только четыре закрытых имени и точные preimage.
// Все файлы проверяются до записи, затем заменяются атомарно через rename.
const fail = () => { throw Error("EDGE_UNIT_REPLACEMENT_REJECTED"); };
export function replaceUnits(input, fs = filesystem) {
  const { constants, openSync, closeSync, fstatSync, fchmodSync, readFileSync, writeFileSync, fsyncSync, renameSync, unlinkSync } = fs;
  if (typeof input !== "string" || input.length > 131072) fail();
  const { before, after } = JSON.parse(input), names = Object.keys(after ?? {}).sort();
  if (names.length !== 4 || JSON.stringify(names) !== JSON.stringify(Object.keys(before ?? {}).sort()) ||
    names.some(name => !/^kodex-public-edge-https-[01]\.(socket|service)$/.test(name))) fail();
  const read = path => {
    const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    try { const stat = fstatSync(fd); if (!stat.isFile() || stat.uid !== 0 || stat.nlink !== 1 ||
      (stat.mode & 0o777) !== 0o644 || stat.size > 16384) fail(); return readFileSync(fd, "utf8"); }
    finally { closeSync(fd); }
  };
  for (const name of names) {
    if (typeof before[name] !== "string" || typeof after[name] !== "string" || after[name].length > 16384 ||
      !after[name].startsWith("# Kodex public edge plan ")) fail();
    const current = read(`/etc/systemd/system/${name}`);
    if (current !== before[name] && current !== after[name]) fail();
  }
  for (const name of names) {
    const path = `/etc/systemd/system/${name}`;
    if (read(path) === after[name]) continue;
    const temporary = `/etc/systemd/system/.${name}.kodex-${process.pid}`;
    const fd = openSync(temporary, constants.O_WRONLY | constants.O_CREAT | constants.O_EXCL | constants.O_NOFOLLOW, 0o644);
    try { fchmodSync(fd, 0o644); writeFileSync(fd, after[name]); fsyncSync(fd); } finally { closeSync(fd); }
    if (read(path) !== before[name]) { unlinkSync(temporary); fail(); }
    renameSync(temporary, path);
  }
  const directory = openSync("/etc/systemd/system", constants.O_RDONLY | constants.O_DIRECTORY);
  try { fsyncSync(directory); } finally { closeSync(directory); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    if (process.getuid() !== 0 || process.argv.length !== 2) fail();
    replaceUnits(filesystem.readFileSync(0, "utf8"));
    process.stdout.write("EDGE_UNITS_REPLACED\n");
  } catch { process.stderr.write("EDGE_UNIT_REPLACEMENT_REJECTED\n"); process.exitCode = 1; }
}
