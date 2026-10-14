import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const target = "spiffe://kodex.local/ns/kodex-system/sa/control-plane";
const localModes = new Set(["USER_CREDENTIAL_REQUIRED", "SERVICE_OWNER_RESOLVED"]);
// Controller имеет только эту закрытую READ-пару в trusted-профиле;
// protected producer отсутствует, никакие WRITE/новые методы из неё не выводятся.
const trustedControllerReads = new Map([
  ["platform.role-images.admission.recovery-terminal.get", "/controlplane.v1.RoleImageService/GetImageAdmissionRecoveryTerminal"],
  ["platform.role-images.supply-work.get", "/controlplane.v1.RoleImageService/GetImageSupplyWorkAvailability"],
]);
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value !== null && typeof value === "object") return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
  return value;
}
export function bindingDigest(binding) {
  return createHash("sha256").update(JSON.stringify(canonical(binding))).digest("hex");
}

// Классификация закрепляет полный исходный binding. Изменение permission,
// provenance или request profile требует явного изменения classification;
// обновление общего registry само по себе не расширяет локальный allowlist.
export function buildServicePolicy(source, classification) {
  if (classification?.version !== 1 || classification.target_spiffe_id !== target || !Array.isArray(classification.operations) || !Array.isArray(source?.policy?.operation_bindings)) throw new Error("service policy inputs rejected");
  const records = new Map();
  for (const record of classification.operations) {
    const local = record.trusted_binding;
    if (local) {
      const expected = {actor_mode:"SERVICE_OWNER_RESOLVED",caller_spiffe_id:"spiffe://kodex.local/ns/kodex-system/sa/image-admission-controller",
        full_method:trustedControllerReads.get(record.operation_id),operation_id:record.operation_id,permission:record.operation_id,project_required:false};
      if (!expected.full_method || record.actor_mode !== expected.actor_mode || record.source_binding_sha256 !== undefined ||
          JSON.stringify(canonical(local)) !== JSON.stringify(canonical(expected)) || record.trusted_binding_sha256 !== bindingDigest(local)) throw new Error("trusted controller read classification rejected");
    }
    if (typeof record.operation_id !== "string" || records.has(record.operation_id) || (!local && !/^[a-f0-9]{64}$/.test(record.source_binding_sha256)) || (!localModes.has(record.actor_mode) && record.actor_mode !== "PRESERVE_DELEGATION")) throw new Error("service operation classification rejected");
    records.set(record.operation_id,record);
  }
  const bindings = [];
  const seen = new Set();
  for (const record of records.values()) {
    if (!record.trusted_binding) continue;
    if (source.policy.operation_bindings.some(binding => binding.operation_id === record.operation_id)) throw new Error("trusted controller read has protected authority");
    bindings.push(structuredClone(record.trusted_binding)); seen.add(record.operation_id);
  }
  for (const binding of source.policy.operation_bindings.filter((value) => value.target_spiffe_id === target)) {
    const record = records.get(binding.operation_id);
    if (!record || seen.has(binding.operation_id) || record.source_binding_sha256 !== bindingDigest(binding)) throw new Error("service operation classification drift");
    seen.add(binding.operation_id);
    if (record.actor_mode === "PRESERVE_DELEGATION") {
      if (!binding.continuation) throw new Error("delegated service operation profile rejected");
      continue;
    }
    if (binding.continuation || binding.authority_sources.includes("RUNTIME_EXECUTION")) throw new Error("delegation cannot become ordinary service authority");
    const user = binding.caller_workload_id === "control-api-gateway";
    if (user !== (record.actor_mode === "USER_CREDENTIAL_REQUIRED") || typeof binding.project_required !== "boolean") throw new Error("service actor classification rejected");
    bindings.push({caller_spiffe_id:binding.caller_spiffe_id,full_method:binding.full_method,operation_id:binding.operation_id,permission:binding.permission,actor_mode:record.actor_mode,project_required:binding.project_required});
  }
  if (seen.size !== records.size || bindings.length === 0) throw new Error("service operation coverage incomplete");
  bindings.sort((a,b) => a.operation_id < b.operation_id ? -1 : a.operation_id > b.operation_id ? 1 : 0);
  return {version:1,target_spiffe_id:target,bindings};
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [mode, sourcePath, classificationPath, outputPath, ...extra] = process.argv.slice(2);
  if (!["check","generate"].includes(mode) || !sourcePath || !classificationPath || !outputPath || extra.length) throw new Error("usage: service-identity-policy.mjs check|generate SOURCE CLASSIFICATION OUTPUT");
  // Go consumer требует JCS bytes: порядок ключей канонический, whitespace
  // и завершающий перевод строки не добавляются. Формат сверяется Go-тестом.
  const output = JSON.stringify(canonical(buildServicePolicy(JSON.parse(readFileSync(sourcePath,"utf8")),JSON.parse(readFileSync(classificationPath,"utf8")))));
  if (mode === "generate") writeFileSync(outputPath,output);
  else if (readFileSync(outputPath,"utf8") !== output) throw new Error("generated service policy drift");
  process.stdout.write("service policy verified\n");
}
