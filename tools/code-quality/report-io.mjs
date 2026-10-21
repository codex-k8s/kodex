import fs from "node:fs";
import path from "node:path";

// Общий контракт путей CLI не меняет смысл отдельных отчётов.
// Отчёты остаются вне дерева исходников.
export function reportPaths(args) {
  const [input, output] = args;
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
  return { input, output };
}

export function writePrivateReport(output, result) {
  fs.writeFileSync(output, JSON.stringify(result, null, 2) + "\n", {
    mode: 0o600,
  });
}
