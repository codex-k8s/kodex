import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { execFileSync } from "node:child_process";
import { inventory } from "./source-inventory.mjs";

const toolDirectory = path.dirname(fileURLToPath(import.meta.url));
const rootIndex = process.argv.indexOf("--root");
const root = rootIndex < 0 ? path.resolve(toolDirectory, "../..") : path.resolve(process.argv[rootIndex + 1]);
const output = process.argv[2];
if (!output || !path.isAbsolute(output)) throw Error("An absolute report directory is required");
fs.mkdirSync(output, { recursive: true, mode: 0o700 });
const files = inventory(root);
fs.writeFileSync(path.join(output, "source-inventory.json"), JSON.stringify(files, null, 2) + "\n", { mode: 0o600 });
if (!files.sources.length) throw Error("Source inventory is empty");
if (process.argv[3] === "--inventory-only") {
  console.log(JSON.stringify({ sources: files.sources.length, excluded: files.excluded.length, output }));
  process.exit(0);
}
execFileSync(path.join(toolDirectory, "node_modules/.bin/jscpd"), [
  ...files.sources,
  "--min-tokens", "100", "--min-lines", "10", "--mode", "weak",
  "--ignore-identifiers=consistent", "--max-size", "20mb", "--workers", "4",
  "--reporters", "json", "--output", output, "--no-colors", "--no-tips", "--absolute",
], { cwd: root, encoding: "utf8", timeout: 240_000, maxBuffer: 32 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });
console.log(JSON.stringify({ sources: files.sources.length, excluded: files.excluded.length, output }));
