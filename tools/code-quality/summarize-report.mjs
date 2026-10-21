import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";

const root = process.argv[4] ? path.resolve(process.argv[4]) : path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const directory = process.argv[2];
if (!directory || !path.isAbsolute(directory)) throw Error("An absolute report directory is required");
const bytes = fs.readFileSync(path.join(directory, "jscpd-report.json"));
const report = JSON.parse(bytes);
const inventory = JSON.parse(fs.readFileSync(path.join(directory, "source-inventory.json")));
const clones = report.duplicates.map((clone) => {
  const location = (file) => {
    const relative = path.relative(root, file.name);
    if (!relative || relative.startsWith("../") || path.isAbsolute(relative) || !inventory.sources.includes(relative))
      throw Error("Clone refers to a file outside the source inventory");
    return { path: relative, start: file.start, end: file.end };
  };
  return { format: clone.format, kind: clone.kind, tokens: clone.tokens, lines: clone.lines,
    first: location(clone.firstFile), second: location(clone.secondFile),
    fragmentSHA256: crypto.createHash("sha256").update(clone.fragment).digest("hex") };
});
const sourceSHA = execFileSync("git", ["rev-parse", "HEAD"], { cwd: root, encoding: "utf8" }).trim();
const summary = { generatedBy: "tools/code-quality/summarize-report.mjs", sourceSHA,
  detector: "jscpd@5.4.2", minTokens: 100, minLines: 10, mode: "weak", ignoreIdentifiers: "consistent",
  reportSHA256: crypto.createHash("sha256").update(bytes).digest("hex"),
  sources: inventory.sources.length, excluded: inventory.excluded, statistics: report.statistics, clones };
const output = process.argv[3] ?? path.join(directory, "summary.gen.json");
if (!path.isAbsolute(output) || !output.endsWith(".gen.json"))
  throw Error("An absolute .gen.json report path is required");
fs.writeFileSync(output, JSON.stringify(summary, null, 2) + "\n", { mode: 0o600 });
console.log(JSON.stringify({ output, sources: summary.sources, excluded: summary.excluded.length, clones: clones.length, reportSHA256: summary.reportSHA256 }));
