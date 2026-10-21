import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { reportPaths, writePrivateReport } from "./report-io.mjs";

function checkedPath(value) {
  if (
    typeof value !== "string" ||
    !value ||
    path.isAbsolute(value) ||
    value.split("/").some((part) => part === ".." || part === ".")
  ) {
    throw Error("Clone decision has an invalid source path");
  }
  return value;
}

function checkedKey(value) {
  if (
    !value ||
    typeof value.format !== "string" ||
    !value.format ||
    typeof value.fragmentSHA256 !== "string" ||
    !/^[0-9a-f]{64}$/.test(value.fragmentSHA256)
  ) {
    throw Error("Clone decision has an invalid detector key");
  }
  const firstPath = checkedPath(value.firstPath);
  const secondPath = checkedPath(value.secondPath);
  return JSON.stringify([
    value.format,
    ...[firstPath, secondPath].sort(),
    value.fragmentSHA256,
  ]);
}

// Каждое решение привязано к точным файлам, содержимому фрагмента и числу
// совпадений; сдвиг строк от комментариев не маскирует новые дубликаты.
export function assessClones(summary, registry) {
  if (
    summary?.generatedBy !== "tools/code-quality/summarize-report.mjs" ||
    !Array.isArray(summary.clones) ||
    typeof summary.reportSHA256 !== "string" ||
    registry?.version !== 1 ||
    !Array.isArray(registry.entries)
  ) {
    throw Error(
      "A normalized detector report and decision registry are required",
    );
  }
  const occurrences = new Map();
  summary.clones.forEach((clone, index) => {
    const key = checkedKey({
      format: clone.format,
      firstPath: clone.first?.path,
      secondPath: clone.second?.path,
      fragmentSHA256: clone.fragmentSHA256,
    });
    if (!occurrences.has(key)) occurrences.set(key, []);
    occurrences.get(key).push(index);
  });

  const retained = [];
  const resolved = new Set();
  const registered = new Set();
  for (const entry of registry.entries) {
    const key = checkedKey(entry);
    if (
      registered.has(key) ||
      !Number.isSafeInteger(entry.count) ||
      entry.count < 1 ||
      typeof entry.reason !== "string" ||
      entry.reason.trim().length < 80
    ) {
      throw Error("Clone decision is duplicated or lacks a bounded reason");
    }
    registered.add(key);
    const indices = occurrences.get(key) ?? [];
    if (indices.length !== entry.count) {
      throw Error(
        "Retained clone decision is stale or covers a new occurrence",
      );
    }
    indices.forEach((index) => resolved.add(index));
    retained.push({
      key: JSON.parse(key),
      count: indices.length,
      indices,
      reason: entry.reason,
    });
  }
  const unresolved = summary.clones.flatMap((clone, index) =>
    resolved.has(index)
      ? []
      : [
          {
            index,
            format: clone.format,
            first: clone.first,
            second: clone.second,
            fragmentSHA256: clone.fragmentSHA256,
          },
        ],
  );
  return {
    generatedBy: "tools/code-quality/assess-clones.mjs",
    sourceSHA: summary.sourceSHA,
    reportSHA256: summary.reportSHA256,
    rawGroups: summary.clones.length,
    retainedGroups: resolved.size,
    unresolvedGroups: unresolved.length,
    retained,
    unresolved,
  };
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const { input, output } = reportPaths(process.argv.slice(2));
  const summary = JSON.parse(fs.readFileSync(input, "utf8"));
  const registry = JSON.parse(
    fs.readFileSync(new URL("./retained-clones.json", import.meta.url), "utf8"),
  );
  const result = assessClones(summary, registry);
  writePrivateReport(output, result);
  console.log(
    JSON.stringify({
      reportSHA256: result.reportSHA256,
      rawGroups: result.rawGroups,
      retainedGroups: result.retainedGroups,
      unresolvedGroups: result.unresolvedGroups,
      output,
    }),
  );
}
