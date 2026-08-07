import { spawnSync } from "node:child_process";
import {
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
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

const semanticNames = [
  ['"GREGORIAN" | "BUSINESS"', "ScheduleCalendar"],
  ['"FORBID" | "SKIP" | "QUEUE"', "ScheduleOverlapPolicy"],
  [
    '"SKIP" | "RUN_ONCE" | "CATCH_UP" | "WITHIN_GRACE"',
    "ScheduleMisfirePolicy",
  ],
  ['"AT_LEAST_ONCE" | "EXACTLY_ONCE_EFFECT"', "ScheduleDeliveryPolicy"],
  ['"NEW" | "PERSISTENT" | "ROLLING"', "ScheduleSessionPolicy"],
  [
    '"ALWAYS" | "ON_ACTION" | "ON_FAILURE" | "ON_ACTION_OR_FAILURE" | "AUDIT_ONLY"',
    "ScheduleNotificationPolicy",
  ],
  ['"AGENT" | "PLAYBOOK"', "ScheduleTargetType"],
  ["cron: string;", "CronScheduleProjection"],
  ["intervalSeconds: number;", "IntervalScheduleProjection"],
  [
    '"AWAITING_DELIVERY_PROOF" | "READY" | "TERMINAL" | "EXPIRED"',
    "OwnerGateDeliveryState",
  ],
  [
    '"WAIT_FOR_DELIVERY" | "RESOLVE" | "READ_TERMINAL" | "NONE"',
    "OwnerGateNextAction",
  ],
];

const generatedFiles = readdirSync(output).filter((name) =>
  name.endsWith(".ts"),
);
const anonymousFiles = generatedFiles.filter((name) =>
  /^AnonymousSchema_\d+\.ts$/.test(name),
);
const replacements = new Map();
for (const filename of anonymousFiles) {
  const source = readFileSync(resolve(output, filename), "utf8");
  const match = semanticNames.find(([signature]) => source.includes(signature));
  if (!match)
    throw new Error(
      `AsyncAPI anonymous model ${filename} has no semantic name`,
    );
  const oldName = filename.slice(0, -3);
  const newName = match[1];
  if ([...replacements.values()].includes(newName)) {
    throw new Error(`AsyncAPI semantic model ${newName} is duplicated`);
  }
  replacements.set(oldName, newName);
}
if (replacements.size !== semanticNames.length) {
  throw new Error("AsyncAPI anonymous model set is incomplete");
}

for (const filename of generatedFiles) {
  const path = resolve(output, filename);
  let source = readFileSync(path, "utf8");
  for (const [oldName, newName] of replacements) {
    source = source.replaceAll(oldName, newName);
  }
  writeFileSync(path, source, "utf8");
}
for (const [oldName, newName] of replacements) {
  renameSync(
    resolve(output, `${oldName}.ts`),
    resolve(output, `${newName}.ts`),
  );
}
