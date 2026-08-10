import { spawnSync } from "node:child_process";
import { readFileSync, readdirSync, rmSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(fileURLToPath(new URL("..", import.meta.url)));
const output = resolve(root, "src/shared/api/generated/asyncapi");
if (!output.startsWith(`${root}/src/shared/api/generated/`)) {
  throw new Error("AsyncAPI output path is outside the generated boundary");
}
rmSync(output, { force: true, recursive: true });

const cli = resolve(root, "node_modules/.bin/asyncapi");
const contract = resolve(
  root,
  "../../../contracts/asyncapi/control-api-gateway/v1/asyncapi.yaml",
);
const result = spawnSync(
  cli,
  [
    "generate",
    "models",
    "typescript",
    contract,
    "--tsModelType",
    "interface",
    "--tsEnumType",
    "union",
    "--tsExportType",
    "named",
    "--tsModuleSystem",
    "ESM",
    "--tsIncludeComments",
    "--no-interactive",
    "--output",
    output,
  ],
  {
    cwd: root,
    env: {
      ...process.env,
      NODE_ENV: "development",
      NODE_CONFIG_ENV: "development",
      NODE_CONFIG: JSON.stringify({ analytics: { enabled: false } }),
      SUPPRESS_NO_CONFIG_WARNING: "true",
    },
    stdio: "inherit",
  },
);
if (result.status !== 0) {
  throw new Error(
    `AsyncAPI generation failed with exit code ${result.status ?? "unknown"}`,
  );
}

const generatedFiles = readdirSync(output).filter((name) =>
  name.endsWith(".ts"),
);
const anonymousFiles = generatedFiles.filter((name) =>
  /^AnonymousSchema_\d+\.ts$/.test(name),
);
if (anonymousFiles.length > 0) {
  throw new Error(
    `AsyncAPI contract generated anonymous models: ${anonymousFiles.join(", ")}`,
  );
}
for (const filename of generatedFiles) {
  const source = readFileSync(resolve(output, filename), "utf8");
  if (/\bAnonymousSchema_\d+\b/.test(source)) {
    throw new Error(
      `AsyncAPI contract generated an anonymous symbol in ${filename}`,
    );
  }
}
