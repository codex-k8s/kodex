import { createHash } from "node:crypto";
import { fingerprint } from "./scoped-release.mjs";

export const policyBase = "kodex-image-admission-policy";
export const namespace = "kodex-system";
export const toolsDigestAnnotation = "kodex.dev/admission-tools-sha256";
const digestPattern = /^sha256:[a-f0-9]{64}$/;
const requireValue = (value, code) => { if (!value) throw new Error(code); };

// Один и тот же exact переход используется при записи policy и проверке её происхождения.
export function applyIssuerAdmissionTransition(spec) {
  const result = structuredClone(spec);
  const before = "variables.pod.initContainers[1].image == params.spec.authorityImage";
  const after = "variables.pod.initContainers[1].image == (has(params.spec.authorityIssuerImage) ? params.spec.authorityIssuerImage : params.spec.authorityImage)";
  const matches = result.validations.filter(rule => rule.expression.includes(before) || rule.expression.includes(after));
  requireValue(matches.length === 1, "EXACT_ISSUER_ADMISSION_RULE_REQUIRED");
  matches[0].expression = matches[0].expression.replace(before, after);
  return result;
}

export function policyToolsDigest(policy) {
  const image = policy?.data?.toolsImage;
  requireValue(typeof image === "string" && /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(image), "EXACT_TOOLS_IMAGE_REQUIRED");
  return image.split("@")[1];
}

export function policyDigest(data) {
  const entries = Object.entries(data).filter(([key]) => !["orchestrationRevision", "policySHA256"].includes(key));
  requireValue(entries.every(([key, value]) => /^[A-Za-z][A-Za-z0-9]*$/.test(key) && typeof value === "string" && /^[\x20-\x7e]*$/.test(value)), "POLICY_STRING_MAP_REQUIRED");
  entries.sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0);
  return createHash("sha256").update(`${JSON.stringify(Object.fromEntries(entries))}\n`).digest("hex");
}

export function requireStaging(resource) {
  const m = resource?.metadata;
  requireValue(m?.namespace === namespace && /^[a-f0-9-]{36}$/.test(m.uid ?? "") && /^\d+$/.test(m.resourceVersion ?? "") &&
    !m.deletionTimestamp && m.labels?.["app.kubernetes.io/part-of"] === "kodex" &&
    m.labels?.["kodex.dev/local-profile"] === "hot-reload", "EXACT_STAGING_RESOURCE_REQUIRED");
}

export function requireIdle(state, now = Date.now()) {
  requireValue(Number.isFinite(Date.parse(state?.at)) && now - Date.parse(state.at) >= -5000 && now - Date.parse(state.at) < 30_000 &&
    ["openBuilds", "pendingAdmissions", "pendingPromotions", "activeRuntimeRuns", "claimedRuntimeLeases"].every((key) => state[key] === 0) &&
    Number.isSafeInteger(state.promotedArtifactCount) && state.promotedArtifactCount >= 0 && /^[a-f0-9]{64}$/.test(state.promotedPinsSHA256 ?? ""), "FRESH_IDLE_OWNER_STATE_REQUIRED");
}

// Отдельная configuration-фаза сохраняет старые policy, helper image и published pins.
export function preparePolicy(policy, parameters, catalog, runnerDigest, authorityIssuerImage) {
  [policy, parameters, catalog].forEach(requireStaging);
  requireValue(policy.kind === "ConfigMap" && policy.immutable === true && policy.metadata.labels["kodex.dev/owner-intent"] === "true" &&
    parameters.kind === "ImageAdmissionPolicyParameters" && parameters.metadata.name === policy.metadata.name &&
    fingerprint(parameters.spec) === fingerprint(policy.data) && policyDigest(policy.data) === policy.data.policySHA256 &&
    (policy.metadata.name === policyBase || policy.metadata.name === `${policyBase}-${policy.data.policySHA256.slice(0, 32)}`), "CURRENT_POLICY_BINDING_INVALID");
  requireValue(catalog.kind === "ConfigMap" && typeof catalog.data?.["catalog.json"] === "string" &&
    Object.keys(catalog.data).length === 1 && digestPattern.test(runnerDigest) && (runnerDigest !== policy.data.trustedRoleBaseDigest || authorityIssuerImage !== undefined), "NEW_RUNNER_AND_EXACT_CATALOG_REQUIRED");
  const issuerChange = authorityIssuerImage !== undefined;
  requireValue(!issuerChange || /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(authorityIssuerImage) && authorityIssuerImage !== (policy.data.authorityIssuerImage ?? policy.data.authorityImage), "NEW_EXACT_AUTHORITY_ISSUER_REQUIRED");
  const revision = Number(policy.data.policyRevision);
  requireValue(Number.isSafeInteger(revision) && revision > 0 && revision < Number.MAX_SAFE_INTEGER, "POLICY_REVISION_INVALID");
  const data = { ...policy.data, policyRevision: String(revision + 1), trustedRoleBaseDigest: runnerDigest, ...(issuerChange ? {authorityIssuerImage} : {}) };
  data.policySHA256 = policyDigest(data);
  const name = `${policyBase}-${data.policySHA256.slice(0, 32)}`;
  const toolsDigest = policyToolsDigest(policy);
  requireValue(policy.metadata.annotations?.[toolsDigestAnnotation] === toolsDigest, "TOOLS_ANNOTATION_MISMATCH");
  const nextCatalog = JSON.parse(catalog.data["catalog.json"]);
  requireValue(nextCatalog.schemaVersion === 1 && Array.isArray(nextCatalog.environments), "CATALOG_SCHEMA_INVALID");
  const bases = nextCatalog.environments.filter((item) => item.key === "standard");
  requireValue(bases.length === 1 && bases[0].available === true && bases[0].baseImageReference === data.trustedRoleBaseRepository &&
    bases[0].baseImageDigest === policy.data.trustedRoleBaseDigest, "CATALOG_BASE_BINDING_INVALID");
  bases[0].baseImageDigest = runnerDigest;
  const catalogData = runnerDigest === policy.data.trustedRoleBaseDigest ? {...catalog.data} : { "catalog.json": `${JSON.stringify(nextCatalog, null, 2)}\n` };
  const catalogName = runnerDigest === policy.data.trustedRoleBaseDigest ? catalog.metadata.name : `kodex-role-environments-${fingerprint(catalogData).slice(0, 32)}`;
  const metadata = (original, nextName) => ({ name: nextName, namespace, labels: { ...original.metadata.labels, "kodex.dev/owner-intent": "true" } });
  return { version: 1, previous: { policyName: policy.metadata.name, policyUID: policy.metadata.uid, policySHA256: policy.data.policySHA256,
    catalogName: catalog.metadata.name, catalogUID: catalog.metadata.uid, catalogSHA256: fingerprint(catalog.data) },
  resources: [
    { apiVersion: "v1", kind: "ConfigMap", metadata: { ...metadata(policy, name), annotations: { [toolsDigestAnnotation]: toolsDigest } }, immutable: true, data },
    { apiVersion: parameters.apiVersion, kind: parameters.kind, metadata: metadata(parameters, name), spec: data },
    { apiVersion: "v1", kind: "ConfigMap", metadata: metadata(catalog, catalogName), immutable: true, data: catalogData },
  ] };
}

function application(deployment) {
  requireStaging(deployment);
  const name = deployment.metadata.name;
  requireValue(deployment.kind === "Deployment" && ["control-plane", "role-image-builder", "image-admission-controller"].includes(name) &&
    !deployment.spec.paused && deployment.spec.replicas > 0, "EXACT_DEPLOYMENT_REQUIRED");
  requireValue(deployment.status?.observedGeneration >= deployment.metadata.generation &&
    ["replicas", "updatedReplicas", "readyReplicas", "availableReplicas"].every((key) => deployment.status[key] === deployment.spec.replicas), "COMPLETED_HEALTHY_ROLLOUT_REQUIRED");
  const next = structuredClone(deployment.spec);
  const containers = next.template.spec.containers.filter((item) => item.name === name);
  requireValue(containers.length === 1, "APPLICATION_CONTAINER_REQUIRED");
  return { next, app: containers[0] };
}

function setLiteral(app, key, value, expected) {
  const entries = app.env?.filter((item) => item.name === key) ?? [];
  requireValue(entries.length === 1 && entries[0].valueFrom === undefined && typeof entries[0].value === "string" &&
    (expected === undefined || entries[0].value === expected), "EXACT_LITERAL_CONFIG_REQUIRED");
  entries[0].value = value;
}

export function planDeployment(deployment, bundle, phase, readerImage) {
  const { next, app } = application(deployment);
  const name = deployment.metadata.name;
  const policy = bundle.resources[0], catalog = bundle.resources[2];
  if (phase === "reader") {
    requireValue(name === "image-admission-controller" && /^registry\.local\.kodex\/kodex\/image-admission@sha256:[a-f0-9]{64}$/.test(readerImage), "EXACT_READER_IMAGE_REQUIRED");
    const pauses = app.env.filter((item) => item.name === "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS");
    requireValue(pauses.length === 0 || pauses.length === 1 && pauses[0].value === "false" && !pauses[0].valueFrom, "READER_TRANSITION_ALREADY_STARTED");
    setLiteral(app, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP", bundle.previous.policyName, bundle.previous.policyName);
    app.image = readerImage;
    if (pauses.length) pauses[0].value = "true"; else app.env.push({ name: "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS", value: "true" });
  } else if (phase === "controller" || phase === "resume") {
    requireValue(name === "image-admission-controller" && app.image === readerImage, "COMPATIBLE_READER_REQUIRED");
    setLiteral(app, "IMAGE_ADMISSION_CONTROLLER_PAUSE_NEW_RUNS", phase === "resume" ? "false" : "true", "true");
    setLiteral(app, "IMAGE_ADMISSION_CONTROLLER_POLICY_CONFIG_MAP", policy.metadata.name,
      phase === "resume" ? policy.metadata.name : bundle.previous.policyName);
  } else {
    requireValue((phase === "control-plane" || phase === "role-image-builder") && name === phase, "PHASE_TARGET_MISMATCH");
    const volumes = next.template.spec.volumes.filter((item) => item.name === "role-environments");
    requireValue(volumes.length === 1 && volumes[0].configMap?.name === bundle.previous.catalogName, "CURRENT_CATALOG_MISMATCH");
    volumes[0].configMap.name = catalog.metadata.name;
    next.template.metadata.annotations ??= {};
    next.template.metadata.annotations["kodex.dev/runtime-admission-policy-sha256"] = policy.data.policySHA256;
    if (phase === "control-plane") {
      setLiteral(app, "CONTROL_PLANE_IMAGE_POLICY_REVISION", policy.data.policyRevision, String(Number(policy.data.policyRevision) - 1));
      setLiteral(app, "CONTROL_PLANE_IMAGE_POLICY_SHA256", policy.data.policySHA256, bundle.previous.policySHA256);
      setLiteral(app, "CONTROL_PLANE_TRUSTED_ROLE_BASE_DIGEST", policy.data.trustedRoleBaseDigest);
    } else {
      requireValue(next.template.metadata.annotations["kodex.dev/trusted-role-base-repository"] === policy.data.trustedRoleBaseRepository, "BUILDER_BASE_REPOSITORY_MISMATCH");
      for (const suffix of ["REPOSITORY", "DIGEST"]) {
        const entries = app.env.filter((item) => item.name === `ROLE_IMAGE_BUILDER_TRUSTED_ROLE_BASE_${suffix}`);
        requireValue(entries.length === 1 && entries[0].value === undefined &&
          entries[0].valueFrom?.fieldRef?.fieldPath === `metadata.annotations['kodex.dev/trusted-role-base-${suffix.toLowerCase()}']`, "BUILDER_DOWNWARD_CONFIG_REQUIRED");
      }
      next.template.metadata.annotations["kodex.dev/trusted-role-base-digest"] = policy.data.trustedRoleBaseDigest;
    }
  }
  return { next, patch: [
    { op: "test", path: "/metadata/uid", value: deployment.metadata.uid },
    { op: "test", path: "/metadata/resourceVersion", value: deployment.metadata.resourceVersion },
    { op: "replace", path: "/spec", value: next },
  ], afterSHA256: fingerprint(next) };
}

export function planBinding(binding, role, bundle) {
  requireStaging(role);
  requireValue(binding.kind === "ValidatingAdmissionPolicyBinding" && binding.metadata.name === "kodex-image-admission-controller-jobs" &&
    /^[a-f0-9-]{36}$/.test(binding.metadata.uid ?? "") && /^\d+$/.test(binding.metadata.resourceVersion ?? "") &&
    binding.spec.policyName === binding.metadata.name && [bundle.previous.policyName, bundle.resources[0].metadata.name].includes(binding.spec.paramRef?.name) &&
    binding.spec.paramRef.namespace === namespace && binding.spec.paramRef.parameterNotFoundAction === "Deny" &&
    fingerprint(binding.spec.validationActions) === fingerprint(["Deny"]) && role.metadata.name === "image-admission-controller", "EXACT_ADMISSION_BOUNDARY_REQUIRED");
  const spec = structuredClone(binding.spec), rules = structuredClone(role.rules);
  spec.paramRef.name = bundle.resources[0].metadata.name;
  for (const resource of ["configmaps", "imageadmissionpolicyparameters"]) {
    const matches = rules.filter((item) => item.resources?.includes(resource));
    requireValue(matches.length === 1 && fingerprint(matches[0].verbs) === fingerprint(["get"]) &&
      [fingerprint([bundle.previous.policyName]), fingerprint([bundle.previous.policyName, bundle.resources[0].metadata.name])].includes(fingerprint(matches[0].resourceNames)), "EXACT_POLICY_READ_PERMISSION_REQUIRED");
    // Старое право сохраняется до завершения работающего reader; оба имени точные.
    if (!matches[0].resourceNames.includes(bundle.resources[0].metadata.name)) matches[0].resourceNames.push(bundle.resources[0].metadata.name);
  }
  return { spec, rules };
}

export function planGatewayMaintenance(deployment, bundleSHA256, restore = false) {
  requireStaging(deployment);
  requireValue(deployment.kind === "Deployment" && deployment.metadata.name === "control-api-gateway" &&
    /^[a-f0-9]{64}$/.test(bundleSHA256), "EXACT_GATEWAY_REQUIRED");
  const annotations = { ...deployment.metadata.annotations };
  const key = "kodex.dev/runner-policy-maintenance";
  const spec = structuredClone(deployment.spec);
  if (restore) {
    const previous = JSON.parse(annotations[key] ?? "null");
    requireValue(previous?.bundleSHA256 === bundleSHA256 && Number.isSafeInteger(previous.replicas) && previous.replicas > 0 &&
      spec.replicas === 0 && (deployment.status?.replicas ?? 0) === 0, "EXACT_MAINTENANCE_REQUIRED");
    spec.replicas = previous.replicas;
    delete annotations[key];
  } else {
    requireValue(!annotations[key] && Number.isSafeInteger(spec.replicas) && spec.replicas > 0 &&
      deployment.status?.availableReplicas === spec.replicas && !spec.paused, "HEALTHY_GATEWAY_REQUIRED");
    annotations[key] = JSON.stringify({ bundleSHA256, replicas: spec.replicas });
    spec.replicas = 0;
  }
  return { spec, annotations };
}
