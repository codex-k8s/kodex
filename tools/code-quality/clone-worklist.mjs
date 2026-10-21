import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

function fingerprint(value) {
  return crypto
    .createHash("sha256")
    .update(JSON.stringify(value))
    .digest("hex");
}

function checkedLocation(value) {
  if (
    !value ||
    typeof value.path !== "string" ||
    !value.path ||
    path.isAbsolute(value.path) ||
    value.path.split("/").includes("..") ||
    !Number.isSafeInteger(value.start) ||
    !Number.isSafeInteger(value.end) ||
    value.start < 1 ||
    value.end < value.start
  ) {
    throw Error("Clone worklist has an invalid source location");
  }
  return value;
}

function orderedLocations(clone) {
  const first = checkedLocation(clone.first);
  const second = checkedLocation(clone.second);
  if (
    first.path < second.path ||
    (first.path === second.path && first.start <= second.start)
  ) {
    return [first, second];
  }
  return [second, first];
}

function materialOverlap(left, right) {
  const common =
    Math.min(left.end, right.end) - Math.max(left.start, right.start) + 1;
  return (
    common > 0 &&
    common * 2 >=
      Math.min(left.end - left.start + 1, right.end - right.start + 1)
  );
}

function adjacent(left, right) {
  return (
    materialOverlap(left.first, right.first) &&
    materialOverlap(left.second, right.second)
  );
}

function interval(members, side) {
  return {
    start: Math.min(...members.map((member) => member[side].start)),
    end: Math.max(...members.map((member) => member[side].end)),
  };
}

// Кластеры упрощают навигацию, но сохраняют каждую исходную группу отдельно.
// Перекрытие координат не означает общего решения о выносе или исключении.
export function buildCloneWorklist(summary) {
  if (
    summary?.generatedBy !== "tools/code-quality/summarize-report.mjs" ||
    !Array.isArray(summary.clones) ||
    typeof summary.reportSHA256 !== "string"
  ) {
    throw Error("A normalized detector report is required");
  }

  const pairs = new Map();
  summary.clones.forEach((clone, index) => {
    if (
      typeof clone.format !== "string" ||
      !clone.format ||
      typeof clone.fragmentSHA256 !== "string" ||
      !/^[0-9a-f]{64}$/.test(clone.fragmentSHA256)
    ) {
      throw Error("Clone worklist has an invalid detector group");
    }
    const [first, second] = orderedLocations(clone);
    const pair = JSON.stringify([clone.format, first.path, second.path]);
    const member = {
      index,
      first,
      second,
      fingerprint: fingerprint([
        clone.format,
        clone.kind,
        first,
        second,
        clone.fragmentSHA256,
      ]),
    };
    if (!pairs.has(pair)) pairs.set(pair, []);
    pairs.get(pair).push(member);
  });

  const clusters = [];
  for (const [pair, members] of [...pairs].sort(([left], [right]) =>
    left.localeCompare(right),
  )) {
    const parent = members.map((_, index) => index);
    const find = (index) => {
      while (parent[index] !== index) index = parent[index];
      return index;
    };
    for (let left = 0; left < members.length; left++) {
      for (let right = left + 1; right < members.length; right++) {
        if (adjacent(members[left], members[right])) {
          parent[find(right)] = find(left);
        }
      }
    }
    const components = new Map();
    members.forEach((member, index) => {
      const root = find(index);
      if (!components.has(root)) components.set(root, []);
      components.get(root).push(member);
    });
    const [format, firstPath, secondPath] = JSON.parse(pair);
    for (const component of components.values()) {
      component.sort((left, right) => left.index - right.index);
      const indices = component.map((member) => member.index);
      clusters.push({
        id: fingerprint([summary.reportSHA256, indices]).slice(0, 24),
        format,
        first: { path: firstPath, ...interval(component, "first") },
        second: { path: secondPath, ...interval(component, "second") },
        members: component.map((member) => ({
          index: member.index,
          fingerprint: member.fingerprint,
        })),
      });
    }
  }
  clusters.sort(
    (left, right) =>
      left.first.path.localeCompare(right.first.path) ||
      left.first.start - right.first.start ||
      left.second.path.localeCompare(right.second.path) ||
      left.second.start - right.second.start ||
      left.id.localeCompare(right.id),
  );
  const indices = clusters.flatMap((cluster) =>
    cluster.members.map((member) => member.index),
  );
  indices.sort((left, right) => left - right);
  if (
    indices.length !== summary.clones.length ||
    indices.some((index, position) => index !== position)
  ) {
    throw Error("Clone worklist lost or duplicated a detector group");
  }
  return {
    generatedBy: "tools/code-quality/clone-worklist.mjs",
    reportSHA256: summary.reportSHA256,
    rawGroups: summary.clones.length,
    filePairs: pairs.size,
    clusters,
  };
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const input = process.argv[2];
  const output = process.argv[3];
  if (
    !input ||
    !output ||
    !path.isAbsolute(input) ||
    !path.isAbsolute(output) ||
    !output.endsWith(".gen.json")
  ) {
    throw Error(
      "Absolute input report and output .gen.json paths are required",
    );
  }
  const result = buildCloneWorklist(JSON.parse(fs.readFileSync(input, "utf8")));
  fs.writeFileSync(output, JSON.stringify(result, null, 2) + "\n", {
    mode: 0o600,
  });
  console.log(
    JSON.stringify({
      output,
      reportSHA256: result.reportSHA256,
      rawGroups: result.rawGroups,
      filePairs: result.filePairs,
      clusters: result.clusters.length,
    }),
  );
}
