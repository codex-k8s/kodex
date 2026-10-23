import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const marker =
  /^\s*(?:\/\/|#|\/\*|<!--)\s*jscpd:ignore-(start|end)(?:\s*\*\/|\s*-->)?\s*$/;

// Реестр связывает каждый подавленный фрагмент с причиной и точными байтами.
// Новый или расширенный маркер требует отдельного пересмотра решения.
export function checkAddressedIgnores(sources, registry) {
  if (
    registry?.version !== 1 ||
    !Array.isArray(registry.regions) ||
    !(sources instanceof Map)
  ) {
    throw Error("A versioned ignore registry and source map are required");
  }
  const expected = new Map();
  for (const region of registry.regions) {
    if (
      !region ||
      typeof region.path !== "string" ||
      !region.path ||
      expected.has(region.path) ||
      !Number.isSafeInteger(region.maxLines) ||
      region.maxLines < 1 ||
      typeof region.sha256 !== "string" ||
      !/^[0-9a-f]{64}$/.test(region.sha256) ||
      typeof region.reason !== "string" ||
      region.reason.trim().length < 40
    ) {
      throw Error("Invalid addressed ignore registration");
    }
    expected.set(region.path, region);
  }
  const seen = new Set();
  for (const [file, source] of sources) {
    const lines = source.split("\n");
    let start = -1;
    let found = 0;
    lines.forEach((line, index) => {
      const match = marker.exec(line);
      if (!match) return;
      if (match[1] === "start") {
        if (start >= 0 || found)
          throw Error(`Nested or repeated ignore marker: ${file}`);
        start = index;
        return;
      }
      if (start < 0) throw Error(`Unpaired ignore marker: ${file}`);
      const region = expected.get(file);
      if (!region) throw Error(`Unregistered ignore marker: ${file}`);
      const body = lines.slice(start + 1, index).join("\n");
      const size = index - start - 1;
      const digest = crypto.createHash("sha256").update(body).digest("hex");
      if (size > region.maxLines || digest !== region.sha256) {
        throw Error(`Changed addressed ignore region: ${file}`);
      }
      seen.add(file);
      found++;
      start = -1;
    });
    if (start >= 0) throw Error(`Unclosed ignore marker: ${file}`);
  }
  for (const file of expected.keys()) {
    if (!seen.has(file))
      throw Error(`Registered ignore marker is absent: ${file}`);
  }
  return { checked: seen.size };
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const inventoryPath = process.argv[2];
  if (!inventoryPath || !path.isAbsolute(inventoryPath)) {
    throw Error("An absolute source inventory path is required");
  }
  const root = path.resolve(
    path.dirname(fileURLToPath(import.meta.url)),
    "../..",
  );
  const inventory = JSON.parse(fs.readFileSync(inventoryPath, "utf8"));
  if (!Array.isArray(inventory.sources))
    throw Error("Source inventory is invalid");
  const sources = new Map();
  for (const file of inventory.sources) {
    if (typeof file !== "string" || !file || path.isAbsolute(file)) {
      throw Error("Source inventory contains an invalid path");
    }
    const full = path.resolve(root, file);
    const relative = path.relative(root, full);
    if (
      !relative ||
      relative === ".." ||
      relative.startsWith(".." + path.sep) ||
      path.isAbsolute(relative)
    ) {
      throw Error("Source inventory contains an invalid path");
    }
    const descriptor = fs.openSync(
      full,
      fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW,
    );
    try {
      if (!fs.fstatSync(descriptor).isFile())
        throw Error("Source inventory contains a non-file");
      sources.set(file, fs.readFileSync(descriptor, "utf8"));
    } finally {
      fs.closeSync(descriptor);
    }
  }
  const registry = JSON.parse(
    fs.readFileSync(new URL("./ignored-clones.json", import.meta.url), "utf8"),
  );
  console.log(JSON.stringify(checkAddressedIgnores(sources, registry)));
}
