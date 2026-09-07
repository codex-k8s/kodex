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

// CFG copy сохраняет закрытый выбор источника, OCC и server-owned provenance.
const copyNames = ["OpaqueRef", "RoleImageConfigurationCopyInput", "RoleImageRecipeCopyInput", "RoleImageManagedCopyInput", "IntegrationDefinitionConfigurationCopyInput", "IntegrationDefinitionShippedCopyInput", "ManagedConfigurationSourceCopyInput", "ShippedIntegrationDefinitionCopySource"];
for (const [name, variants] of [
  ["RoleImageConfigurationCopyInput", [{name:"Копия", projectRef:"prj_fixture01", recipeRef:"recipe_fixture01"}, {name:"Копия", projectRef:"prj_fixture01", configurationRef:"mcfg_fixture01"}]],
  ["IntegrationDefinitionConfigurationCopyInput", [{name:"Копия", shipped:{key:"github", definitionVersion:"1.0.0", digest:"a".repeat(64)}}, {name:"Копия", configurationRef:"mcfg_fixture01"}]],
]) {
  const source = JSON.parse(JSON.stringify({$ref:`#/components/schemas/${name}`, $defs:Object.fromEntries(copyNames.map(key => [key, definitions[key]]))}).replaceAll("#/components/schemas/", "#/$defs/"));
  const check = new Ajv({strict:true, validateFormats:false}).compile(source);
  for (const variant of variants) assert.equal(check(variant), true);
  for (const invalid of [{}, {...variants[0], ...variants[1]}, {...variants[0], actorRef:"usr_fixture01"}, {...variants[1], configurationRef:null}, {...variants[0], copyProvenance:{origin:"UI"}}]) assert.equal(check(invalid), false);
}
for (const [path, operation] of [
  ["/api/v1/role-image-configurations/copies", "copyRoleImageConfiguration"],
  ["/api/v1/integration-definition-configurations/copies", "copyIntegrationDefinitionConfiguration"],
  ["/api/v1/role-image-configurations/{configurationRef}/archive", "archiveRoleImageConfiguration"],
  ["/api/v1/integration-definition-configurations/{configurationRef}/archive", "archiveIntegrationDefinitionConfiguration"],
]) {
  const value = document.paths[path].post;
  assert.equal(value.operationId, operation);
  for (const header of ["IfMatch", "IdempotencyKey", "CsrfToken"]) assert.ok(value.parameters.some(p => p.$ref?.endsWith(`/${header}`)), header);
}

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
import type { RoleImageConfigurationCopyInput as RoleCopy, IntegrationDefinitionConfigurationCopyInput as IntegrationCopy, ManagedConfigurationCopyProvenance as Provenance } from ${JSON.stringify(types)};
const recipe: RoleCopy = {name:"Копия", projectRef:"prj_fixture01", recipeRef:"recipe_fixture01"};
const managed: RoleCopy = {name:"Копия", projectRef:"prj_fixture01", configurationRef:"mcfg_fixture01"};
const shipped: IntegrationCopy = {name:"Копия", shipped:{key:"github", definitionVersion:"1.0.0", digest:"a".repeat(64)}};
// @ts-expect-error источник обязателен
const noSource: RoleCopy = {name:"Копия", projectRef:"prj_fixture01"};
// @ts-expect-error immutable pins обязательны
const noDigest: IntegrationCopy = {name:"Копия", shipped:{key:"github", definitionVersion:"1.0.0"}};
// @ts-expect-error origin закрыт
const badOrigin: Provenance = {origin:"OWNER",sourceRef:"x",sourceRevision:"1",sourceVersion:1,sourceDigest:"a".repeat(64)};
void [recipe, managed, shipped, noSource, noDigest, badOrigin];
`);
  execFileSync(process.execPath, [require.resolve("typescript/bin/tsc"), "--noEmit", "--strict", "--skipLibCheck", "--moduleResolution", "bundler", "--module", "esnext", "--target", "es2022", source], { timeout: 30000, stdio: "pipe" });
} finally {
  rmSync(directory, { recursive: true, force: true });
}
console.log("Managed consumer OpenAPI and generated TypeScript CAS contract passed");
