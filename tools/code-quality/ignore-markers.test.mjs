import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import test from "node:test";

test("pinned detector honors addressed ignore markers even in weak mode", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "kodex-jscpd-marker-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const binary = path.join(path.dirname(fileURLToPath(import.meta.url)), "node_modules/.bin/jscpd");
  const body = "func calculate() int {\n value := 0\n" +
    Array.from({ length: 15 }, (_, i) => ` value += ${i + 1}`).join("\n") + "\n return value\n}\n";
  for (const marked of [false, true]) {
    const input = path.join(directory, marked ? "marked" : "plain");
    fs.mkdirSync(input);
    fs.writeFileSync(path.join(input, "first.go"), "package fixture\n" + body);
    fs.writeFileSync(path.join(input, "second.go"), "package fixture\n" +
      (marked ? "// jscpd:ignore-start\n" : "") + body +
      (marked ? "// jscpd:ignore-end\n" : ""));
    const output = path.join(directory, marked ? "marked-report" : "plain-report");
    execFileSync(binary, [input, "--min-tokens", "50", "--min-lines", "3", "--mode", "weak",
      "--reporters", "json", "--output", output, "--workers", "1", "--no-tips"], {
      timeout: 30_000, stdio: ["ignore", "pipe", "pipe"],
    });
    const report = JSON.parse(fs.readFileSync(path.join(output, "jscpd-report.json")));
    if (marked) assert.equal(report.duplicates.length, 0);
    else assert.ok(report.duplicates.length > 0);
  }
});
