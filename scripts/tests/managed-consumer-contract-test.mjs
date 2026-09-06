// Исходная OpenAPI и generated TypeScript обязаны выражать одну CAS-семантику.
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(join(root, "services/staff/control-center/package.json"));
const Ajv = require("ajv");
const document = JSON.parse(execFileSync("yq", ["-o=json", ".", join(root, "contracts/openapi/control-api-gateway/v1/openapi.yaml")], { timeout: 10000 }));
const definitions = document.components.schemas;
const names = ["OpaqueRef", "ManagedConfigurationConsumer", "ManagedConfigurationConsumerInput", "ManagedConfigurationConsumerAbsent", "ManagedConfigurationConsumerMatch", "ManagedConfigurationRebindInput"];
const schema = JSON.parse(JSON.stringify({ $ref: "#/components/schemas/ManagedConfigurationConsumerInput", $defs: Object.fromEntries(names.map(name => [name, definitions[name]])) }).replaceAll("#/components/schemas/", "#/$defs/"));
// Переносим только контейнер refs; int64 проверяется numeric bounds исходной schema.
const ajv = new Ajv({ strict: true, validateFormats: false, allErrors: true });
const validate = ajv.compile(schema);
const identity = { kind: "STT_SERVICE", ref: "stt-tts-service" };
const absent = { ...identity, expectedAbsent: true };
const match = { ...identity, revisionRef: "mrev_previous", version: 7 };
for (const value of [absent, match, { ...match, expectedAbsent: false }]) assert.equal(validate(value), true);
for (const value of [identity, { ...identity, expectedAbsent: false }, { ...absent, revisionRef: "" }, { ...absent, revisionRef: null }, { ...absent, version: 0 }, { ...absent, version: null }, { ...match, expectedAbsent: true }, { ...match, expectedAbsent: null }, { ...match, expectedAbsent: "false" }, { ...match, revisionRef: null }, { ...match, version: 0 }, { ...match, actor: "owner" }]) assert.equal(validate(value), false);
assert.equal(definitions.ManagedConfigurationRebindInput.properties.consumers.items.$ref, "#/components/schemas/ManagedConfigurationConsumerInput");
assert.deepEqual(definitions.ManagedConfigurationConsumer.required, ["kind", "ref", "revisionRef", "version"]);
assert.equal("expectedAbsent" in definitions.ManagedConfigurationConsumer.properties, false);

const directory = mkdtempSync(join(tmpdir(), "kodex-consumer-types-"));
try {
  const source = join(directory, "contract.ts");
  const types = join(root, "services/staff/control-center/src/shared/api/generated/openapi/types.gen");
  writeFileSync(source, `import type { ManagedConfigurationConsumerInput as Input, ManagedConfigurationConsumer as Read } from ${JSON.stringify(types)};
const absent: Input = {kind:"STT_SERVICE", ref:"stt-tts-service", expectedAbsent:true};
const match: Input = {kind:"STT_SERVICE", ref:"stt-tts-service", revisionRef:"mrev_previous", version:7};
const explicit: Input = {...match, expectedAbsent:false};
// @ts-expect-error отсутствие требует явного expectedAbsent
const missing: Input = {kind:"STT_SERVICE", ref:"stt-tts-service"};
// @ts-expect-error ABSENT не принимает прежние pins
const contradictory: Input = {kind:"STT_SERVICE", ref:"stt-tts-service", expectedAbsent:true, revisionRef:"mrev_previous", version:7};
// @ts-expect-error MATCH требует version
const incomplete: Input = {kind:"STT_SERVICE", ref:"stt-tts-service", expectedAbsent:false, revisionRef:"mrev_previous"};
// @ts-expect-error read DTO не содержит write expectation
const read: Read = {kind:"STT_SERVICE", ref:"stt-tts-service", revisionRef:"mrev_previous", version:7, expectedAbsent:false};
void [absent, match, explicit, missing, contradictory, incomplete, read];
`);
  execFileSync(process.execPath, [require.resolve("typescript/bin/tsc"), "--noEmit", "--strict", "--skipLibCheck", "--moduleResolution", "bundler", "--module", "esnext", "--target", "es2022", source], { timeout: 30000, stdio: "pipe" });
} finally {
  rmSync(directory, { recursive: true, force: true });
}
console.log("Managed consumer OpenAPI and generated TypeScript CAS contract passed");
