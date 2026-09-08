#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { openSync, closeSync, writeSync, fsyncSync, readFileSync, lstatSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { fingerprint } from "./scoped-release.mjs";
import { namespace, preparePolicy, requireIdle, planDeployment, planBinding, planGatewayMaintenance } from "./runner-policy-model.mjs";

const requireValue = (value, code) => { if (!value) throw new Error(code); };
const phases = ["maintenance", "reader", "schema", "admission", "resources", "binding", "control-plane", "role-image-builder", "controller", "resume", "open"];
function privateRead(path) {
  const stat = lstatSync(path);
  requireValue(stat.isFile() && (stat.mode & 0o077) === 0 && stat.size < 8 << 20, "PRIVATE_INPUT_REQUIRED");
  return JSON.parse(readFileSync(path, "utf8"));
}
function record(path, value) {
  const fd = openSync(path, "wx", 0o600);
  try { writeSync(fd, `${JSON.stringify(value)}\n`); fsyncSync(fd); } finally { closeSync(fd); }
}
function env(deployment, key) {
  const apps = deployment.spec.template.spec.containers.filter((item) => item.name === deployment.metadata.name);
  const entries = apps[0]?.env?.filter((item) => item.name === key) ?? [];
  requireValue(apps.length === 1 && entries.length === 1 && typeof entries[0].value === "string" && !entries[0].valueFrom, "EXACT_LITERAL_CONFIG_REQUIRED");
  return entries[0].value;
}
function content(resource) {
  return resource.kind === "ConfigMap" ? { data: resource.data, immutable: resource.immutable === true } : resource.kind === "Role" ? resource.rules : resource.spec;
}

export function samePlan(saved, current) {
  requireValue(saved.version === 1 && ["context", "clusterUID", "namespaceUID", "bundleSHA256", "phase", "readerImage"]
    .every((key) => saved[key] === current[key]) && fingerprint(saved.operations) === fingerprint(current.operations) &&
    fingerprint(saved.guards) === fingerprint(current.guards), "PLAN_PRECONDITION_CHANGED");
}

function main(args) {
  const command = args.shift(), options = {};
  requireValue(["prepare", "inspect", "plan", "apply"].includes(command), "INVALID_COMMAND");
  while (args.length) {
    const key = args.shift();
    requireValue(["--context", "--authority-issuer-image", "--runner-digest", "--reader-image", "--bundle", "--phase", "--output", "--plan", "--evidence", "--confirm"].includes(key) && !Object.hasOwn(options, key) && args.length, "INVALID_ARGUMENTS");
    options[key] = args.shift();
  }
  const context = options["--context"];
  requireValue(typeof context === "string" && context.length > 0 && !/prod/i.test(context), "STAGING_CONTEXT_REQUIRED");
  const kubectl = (args, input) => execFileSync("kubectl", ["--context", context, "--namespace", namespace, ...args],
    { encoding: "utf8", input, timeout: 310_000, maxBuffer: 16 << 20, stdio: [input ? "pipe" : "ignore", "pipe", "pipe"] });
  const get = (kind, name) => JSON.parse(kubectl(["get", kind, name, "-o", "json"]));
  const clusterUID = get("namespace", "kube-system").metadata.uid;
  const namespaceUID = get("namespace", namespace).metadata.uid;
  const controller = get("deployment", "image-admission-controller");
  const readerImage = options["--reader-image"];
  if (command === "prepare") {
    requireValue(!options["--confirm"] && options["--output"], "READ_ONLY_PREPARATION_REQUIRED");
    const oldName = env(controller, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP");
    const cp = get("deployment", "control-plane");
    const catalogs = cp.spec.template.spec.volumes.filter((item) => item.name === "role-environments");
    requireValue(catalogs.length === 1, "EXACT_CATALOG_REQUIRED");
    const bundle = preparePolicy(get("configmap", oldName), get("imageadmissionpolicyparameters", oldName), get("configmap", catalogs[0].configMap.name), options["--runner-digest"], options["--authority-issuer-image"]);
    record(options["--output"], { ...bundle, clusterUID, namespaceUID });
    process.stdout.write(`${JSON.stringify({ status: "PREPARED", policy: bundle.resources[0].metadata.name, digest: bundle.resources[0].data.policySHA256 })}\n`);
    return;
  }
  const bundle = privateRead(options["--bundle"]);
  requireValue(bundle.clusterUID === clusterUID && bundle.namespaceUID === namespaceUID, "CLUSTER_IDENTITY_CHANGED");
  const recomputed = preparePolicy(get("configmap", bundle.previous.policyName), get("imageadmissionpolicyparameters", bundle.previous.policyName),
    get("configmap", bundle.previous.catalogName), bundle.resources[0].data.trustedRoleBaseDigest, bundle.resources[0].data.authorityIssuerImage !== get("configmap", bundle.previous.policyName).data.authorityIssuerImage ? bundle.resources[0].data.authorityIssuerImage : undefined);
  requireValue(fingerprint({ ...recomputed, clusterUID, namespaceUID }) === fingerprint(bundle), "BUNDLE_OR_PREDECESSOR_CHANGED");
  const readDB = () => JSON.parse(kubectl(["exec", "-i", "kodex-postgresql-0", "--", "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "control_plane"], readFileSync(new URL("./runner-policy-readback.sql", import.meta.url), "utf8")));
  const database = readDB();
  const phase = command === "apply" ? privateRead(options["--plan"]).phase : options["--phase"];
  const cp = get("deployment", "control-plane"), builder = get("deployment", "role-image-builder"), gateway = get("deployment", "control-api-gateway");
  const binding = get("validatingadmissionpolicybinding", "kodex-image-admission-controller-jobs");
  const admission = get("validatingadmissionpolicy", "kodex-image-admission-controller-jobs");
  requireValue(admission.spec?.failurePolicy === "Fail", "CLOSED_ADMISSION_POLICY_REQUIRED");
  const role = get("role", "image-admission-controller");
  const jobs = JSON.parse(kubectl(["get", "jobs", "-l", "kodex.dev/image-admission-orchestrated=true", "-o", "json"])).items;
  const pvcs = JSON.parse(kubectl(["get", "persistentvolumeclaims", "-l", "kodex.dev/image-admission-orchestrated=true", "-o", "json"])).items;
  const activeJobs = jobs.filter((job) => !job.status?.conditions?.some((item) => ["Complete", "Failed"].includes(item.type) && item.status === "True"));
  const guards = { cp: { uid: cp.metadata.uid, spec: fingerprint(cp.spec) }, builder: { uid: builder.metadata.uid, spec: fingerprint(builder.spec) },
    controller: { uid: controller.metadata.uid, spec: fingerprint(controller.spec) }, binding: { uid: binding.metadata.uid, spec: fingerprint(binding.spec) },
    role: { uid: role.metadata.uid, rules: fingerprint(role.rules) }, admission: { uid: admission.metadata.uid, spec: fingerprint(admission.spec) },
    gateway: { uid: gateway.metadata.uid, spec: fingerprint(gateway.spec), annotations: fingerprint(gateway.metadata.annotations ?? {}) },
    promotedArtifactCount: database.promotedArtifactCount, promotedPinsSHA256: database.promotedPinsSHA256 };
  if (command === "inspect") {
    requireValue(options["--output"] && !options["--confirm"], "INSPECTION_OUTPUT_REQUIRED");
    record(options["--output"], { at: new Date().toISOString(), clusterUID, namespaceUID, guards, database,
      activeAdmissionJobs: activeJobs.length, admissionWorkspaces: pvcs.length, policyName: env(controller, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP") });
    process.stdout.write(`${JSON.stringify({ status: "INSPECTED", activeAdmissionJobs: activeJobs.length, admissionWorkspaces: pvcs.length })}\n`);
    return;
  }
  requireValue(phases.includes(phase), "INVALID_PHASE");
  requireIdle(database);
  if (phase !== "maintenance") {
    const maintenance = JSON.parse(gateway.metadata.annotations?.["kodex.dev/runner-policy-maintenance"] ?? "null");
    requireValue(gateway.spec.replicas === 0 && (gateway.status?.replicas ?? 0) === 0 && maintenance?.bundleSHA256 === fingerprint(bundle), "APPLICATION_MAINTENANCE_REQUIRED");
  }
  if (!["maintenance", "reader", "open"].includes(phase)) {
    requireValue(env(controller, "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS") === "true" &&
      controller.spec.template.spec.containers.find((item) => item.name === controller.metadata.name)?.image === readerImage &&
      activeJobs.length === 0 && pvcs.length === 0, "PAUSED_COMPATIBLE_IDLE_READER_REQUIRED");
  }
  const operations = [];
  const patch = (resource, field, next) => {
    if (fingerprint(resource[field]) === fingerprint(next)) return;
    operations.push({ type: "patch", kind: resource.kind, name: resource.metadata.name, uid: resource.metadata.uid,
      patch: [{ op: "test", path: "/metadata/uid", value: resource.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: resource.metadata.resourceVersion },
        { op: "replace", path: `/${field}`, value: next }], afterSHA256: fingerprint(next), field });
  };
  if (phase === "maintenance" || phase === "open") {
    if (phase === "open") requireValue(env(controller, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP") === bundle.resources[0].metadata.name &&
      env(controller, "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS") === "false" &&
      [cp, builder, controller].every((item) => item.status?.availableReplicas === item.spec.replicas), "RESUMED_CONSUMERS_REQUIRED");
    const change = planGatewayMaintenance(gateway, fingerprint(bundle), phase === "open");
    operations.push({ type: "patch", kind: gateway.kind, name: gateway.metadata.name, uid: gateway.metadata.uid, field: "spec", afterSHA256: fingerprint(change.spec),
      patch: [{ op: "test", path: "/metadata/uid", value: gateway.metadata.uid }, { op: "test", path: "/metadata/resourceVersion", value: gateway.metadata.resourceVersion },
        { op: "add", path: "/metadata/annotations", value: change.annotations }, { op: "replace", path: "/spec", value: change.spec }] });
  } else if (phase === "schema" || phase === "admission") {
    requireValue(bundle.resources[0].data.authorityIssuerImage && env(controller,"IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS") === "true" &&
      controller.spec.template.spec.containers.find(c=>c.name==="image-admission-controller").image === readerImage, "COMPATIBLE_PAUSED_ISSUER_READER_REQUIRED");
    if (phase === "schema") {
      const crd=get("customresourcedefinition","imageadmissionpolicyparameters.supplychain.kodex.dev"),spec=structuredClone(crd.spec);
      const property={type:"string",pattern:"^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$"};
      requireValue(spec.versions?.length===1 && spec.versions[0].schema?.openAPIV3Schema?.properties?.spec?.properties?.authorityImage,"EXACT_PARAMETERS_SCHEMA_REQUIRED");
      const fields=spec.versions[0].schema.openAPIV3Schema.properties.spec.properties;
      requireValue(!fields.authorityIssuerImage || fingerprint(fields.authorityIssuerImage)===fingerprint(property),"ISSUER_SCHEMA_DRIFT");fields.authorityIssuerImage=property;patch(crd,"spec",spec);
    } else {
      const admission=get("validatingadmissionpolicy","kodex-image-admission-controller-jobs"),spec=structuredClone(admission.spec);
      const before="variables.pod.initContainers[1].image == params.spec.authorityImage",after="variables.pod.initContainers[1].image == (has(params.spec.authorityIssuerImage) ? params.spec.authorityIssuerImage : params.spec.authorityImage)";
      const matches=spec.validations.filter(rule=>rule.expression.includes(before)||rule.expression.includes(after));
      requireValue(matches.length===1,"EXACT_ISSUER_ADMISSION_RULE_REQUIRED");matches[0].expression=matches[0].expression.replace(before,after);patch(admission,"spec",spec);
    }
  } else if (phase === "resources") {
    if (bundle.resources[0].data.authorityIssuerImage) {
      const crd=get("customresourcedefinition","imageadmissionpolicyparameters.supplychain.kodex.dev");
      const property=crd.spec.versions?.[0]?.schema?.openAPIV3Schema?.properties?.spec?.properties?.authorityIssuerImage;
      const admission=get("validatingadmissionpolicy","kodex-image-admission-controller-jobs");
      requireValue(property?.type==="string" && property.pattern==="^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$" &&
        admission.spec.validations.filter(rule=>rule.expression.includes("variables.pod.initContainers[1].image == (has(params.spec.authorityIssuerImage) ? params.spec.authorityIssuerImage : params.spec.authorityImage)")).length===1,
        "ADDITIVE_ISSUER_SCHEMA_AND_ADMISSION_REQUIRED");
    }
    for (const resource of bundle.resources) {
      const current = JSON.parse(kubectl(["get", resource.kind, resource.metadata.name, "--ignore-not-found", "-o", "json"]) || "null");
      if (current) requireValue(fingerprint(content(current)) === fingerprint(content(resource)) && current.metadata.labels?.["kodex.dev/owner-intent"] === "true", "EXISTING_REVISION_CONFLICT");
      else operations.push({ type: "create", resource });
    }
  } else {
    if (phase !== "reader") for (const resource of bundle.resources) {
      requireValue(fingerprint(content(get(resource.kind, resource.metadata.name))) === fingerprint(content(resource)), "PREPARED_RESOURCES_REQUIRED");
    }
    if (phase === "binding") {
      const change = planBinding(binding, role, bundle);
      patch(role, "rules", change.rules); patch(binding, "spec", change.spec);
    } else {
      if (!["reader"].includes(phase)) requireValue(binding.spec.paramRef.name === bundle.resources[0].metadata.name &&
        binding.spec.paramRef.parameterNotFoundAction === "Deny", "NEW_CLOSED_BINDING_REQUIRED");
      if (["controller", "resume"].includes(phase)) {
        requireValue(env(cp, "CONTROL_PLANE_IMAGE_POLICY_SHA256") === bundle.resources[0].data.policySHA256 &&
          cp.spec.template.spec.volumes.find((item) => item.name === "role-environments")?.configMap?.name === bundle.resources[2].metadata.name &&
          builder.spec.template.spec.volumes.find((item) => item.name === "role-environments")?.configMap?.name === bundle.resources[2].metadata.name &&
          builder.spec.template.metadata.annotations?.["kodex.dev/trusted-role-base-digest"] === bundle.resources[0].data.trustedRoleBaseDigest, "ALL_CONSUMERS_MUST_SWITCH_FIRST");
      }
      const target = phase === "control-plane" ? cp : phase === "role-image-builder" ? builder : controller;
      if (phase === "resume") {
        const rules = planBinding(binding, role, bundle).rules;
        for (const rule of rules) if (rule.resources?.some((resource) => ["configmaps", "imageadmissionpolicyparameters"].includes(resource))) {
          rule.resourceNames = [bundle.resources[0].metadata.name];
        }
        patch(role, "rules", rules);
      }
      patch(target, "spec", planDeployment(target, bundle, phase, readerImage).next);
    }
  }
  const current = { version: 1, context, clusterUID, namespaceUID, phase, readerImage, bundleSHA256: fingerprint(bundle), guards, operations };
  if (command === "plan") {
    requireValue(options["--output"] && !options["--confirm"], "READ_ONLY_PLAN_REQUIRED");
    record(options["--output"], { ...current, id: randomUUID(), at: new Date().toISOString() });
    process.stdout.write(`${JSON.stringify({ status: "PLANNED", phase, operations: operations.length })}\n`); return;
  }
  requireValue(options["--confirm"] === "APPLY-STAGING-RUNNER-POLICY" && options["--evidence"], "EXPLICIT_CONFIRMATION_REQUIRED");
  const saved = privateRead(options["--plan"]); samePlan(saved, current);
  const fd = openSync(options["--evidence"], "wx", 0o600);
  const journal = (entry) => { writeSync(fd, `${JSON.stringify({ at: new Date().toISOString(), id: saved.id, phase, ...entry })}\n`); fsyncSync(fd); };
  let attempted = false;
  try {
    journal({ status: "STARTED", bundleSHA256: current.bundleSHA256, guards });
    for (const operation of operations) {
      requireIdle(readDB());
      journal({ status: "INTENT", kind: operation.kind ?? operation.resource.kind, name: operation.name ?? operation.resource.metadata.name });
      attempted = true;
      if (operation.type === "create") {
        kubectl(["create", "-f", "-"], JSON.stringify(operation.resource));
        requireValue(fingerprint(content(get(operation.resource.kind, operation.resource.metadata.name))) === fingerprint(content(operation.resource)), "CREATE_READBACK_MISMATCH");
      } else {
        kubectl(["patch", operation.kind, operation.name, "--type=json", "-p", JSON.stringify(operation.patch)]);
        const readback = get(operation.kind, operation.name);
        requireValue(readback.metadata.uid === operation.uid && fingerprint(readback[operation.field]) === operation.afterSHA256, "PATCH_READBACK_MISMATCH");
        if (operation.kind === "Deployment") kubectl(["rollout", "status", `deployment/${operation.name}`, "--timeout=300s"]);
      }
      journal({ status: "APPLIED", kind: operation.kind ?? operation.resource.kind, name: operation.name ?? operation.resource.metadata.name });
      attempted = false;
    }
    const finalDB = readDB();
    requireValue(finalDB.promotedArtifactCount === guards.promotedArtifactCount && finalDB.promotedPinsSHA256 === guards.promotedPinsSHA256, "PUBLISHED_ARTIFACTS_CHANGED");
    journal({ status: "PASS", promotedPinsSHA256: finalDB.promotedPinsSHA256 });
    process.stdout.write(`${JSON.stringify({ status: "PASS", phase, id: saved.id })}\n`);
  } catch (error) {
    journal({ status: attempted ? "UNKNOWN" : "FAIL", code: "RUNNER_POLICY_TRANSITION_FAILED" }); throw error;
  } finally { closeSync(fd); }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(process.argv.slice(2)); } catch { process.stderr.write("Runner policy transition failed; inspect authoritative state before another operation\n"); process.exitCode = 1; }
}
