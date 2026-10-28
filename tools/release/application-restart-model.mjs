import { applicationDeploymentBoundary, fingerprint, isApplicationTargetName, requireInstanceGrants } from "./scoped-release.mjs";
import { activeDeploymentPods } from "./stable-archive-transition.mjs";

export const restartAnnotation = "kodex.dev/application-restart";
const uuid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const sha40 = /^[a-f0-9]{40}$/;
const sha64 = /^[a-f0-9]{64}$/;
// Только наши статические guard codes можно вывести; backend/filesystem Error остаётся закрытым fallback.
class RestartFailure extends Error {}
export function restartGuard(ok, code) { if (!ok) throw new RestartFailure(code); }
export function restartFailureCode(error) { return error instanceof RestartFailure ? error.message : "APPLICATION_RESTART_FAILED"; }
const requireValue = restartGuard;
const exact = (value, fields) => value && typeof value === "object" && !Array.isArray(value) &&
  Object.keys(value).length === fields.length && fields.every(key => Object.hasOwn(value, key));

// Считывает только закрытые provenance pins; неизвестные annotations/env никогда не попадают в план.
function sourcePins(template) {
  const annotations = template.metadata?.annotations;
  const pins = { sourceSHA: annotations?.["kodex.dev/source-sha"],
    provenanceSHA256: annotations?.["kodex.dev/release-provenance-sha256"],
    releaseID: annotations?.["kodex.dev/application-release"] };
  requireValue(sha40.test(pins.sourceSHA ?? "") && pins.sourceSHA !== "0".repeat(40) &&
    sha64.test(pins.provenanceSHA256 ?? "") && pins.provenanceSHA256 !== "0".repeat(64) &&
    uuid.test(pins.releaseID ?? ""), "IMMUTABLE_SOURCE_PROVENANCE_REQUIRED");
  return pins;
}

// После shared release checks допускается лишь stable binary: source mounts/Air и подмена release не поддерживаются.
function stableBoundary(deployment, name) {
  const boundary = applicationDeploymentBoundary(deployment, { name });
  const { pod, containers, index, beforeImage } = boundary;
  const app = containers[index];
  requireValue(pod.metadata?.labels?.["kodex.dev/local-profile"] === "stable" &&
    !(pod.spec.volumes ?? []).some(volume => volume.hostPath) &&
    (name === "staff-control-center" ? app.command === undefined && app.args === undefined :
      JSON.stringify(app.command) === JSON.stringify([`/usr/local/bin/${name}`])) &&
    beforeImage.includes(`/${name}@sha256:`), "STABLE_APPLICATION_REQUIRED");
  requireValue(boundary.spec.replicas <= 16 && Number.isSafeInteger(boundary.metadata.generation) &&
    boundary.metadata.generation > 0 && /^[1-9][0-9]*$/.test(boundary.metadata.resourceVersion), "RESTART_DEPLOYMENT_BOUNDS");
  requireInstanceGrants(pod);
  return { ...boundary, ...sourcePins(pod) };
}

// ReplicaSet template отличается от Deployment лишь назначенным controller hash label.
function replicaTemplate(replica) {
  const template = structuredClone(replica.spec?.template);
  if (template?.metadata?.labels) delete template.metadata.labels["pod-template-hash"];
  return template;
}

// Использует общий owner-lineage/terminal-pod predicate; Ready Deployment без точных действующих Pods недостаточен.
function servingLineage(deployment, resources, templateSHA256, expected, excluded = []) {
  requireValue(Array.isArray(resources?.items) && resources.items.length <= 256 &&
    resources.items.every(item => ["ReplicaSet", "Pod"].includes(item.kind) &&
      item.metadata?.namespace === deployment.metadata.namespace), "RESTART_RESOURCE_LIST_INVALID");
  const pods = activeDeploymentPods(resources, deployment.metadata.uid);
  requireValue(pods.length === deployment.spec.replicas && pods.length > 0 &&
    new Set(pods.map(pod => pod.metadata.uid)).size === pods.length, "RESTART_POD_CARDINALITY");
  const replicas = resources.items.filter(item => item.kind === "ReplicaSet" &&
    item.metadata?.ownerReferences?.some(owner => owner.uid === deployment.metadata.uid && owner.kind === "Deployment" && owner.controller === true) &&
    fingerprint(replicaTemplate(item)) === templateSHA256);
  requireValue(replicas.length === 1 && uuid.test(replicas[0].metadata.uid), "RESTART_REPLICASET_REQUIRED");
  const digest = expected.image.split("@").at(-1);
  for (const pod of pods) {
    const app = pod.spec?.containers?.filter(container => container.name === expected.name) ?? [];
    const status = pod.status?.containerStatuses?.filter(container => container.name === expected.name) ?? [];
    requireValue(uuid.test(pod.metadata.uid) && !excluded.includes(pod.metadata.uid) && !pod.metadata.deletionTimestamp &&
      pod.metadata.ownerReferences?.some(owner => owner.kind === "ReplicaSet" && owner.uid === replicas[0].metadata.uid && owner.controller === true) &&
      pod.status?.phase === "Running" && pod.status.conditions?.some(condition => condition.type === "Ready" && condition.status === "True") &&
      app.length === 1 && app[0].image === expected.image && status.length === 1 && status[0].ready === true && status[0].state?.running &&
      typeof status[0].imageID === "string" && status[0].imageID.endsWith(`@${digest}`), "RESTART_EXACT_READY_POD_REQUIRED");
    requireValue(expected.restartID === undefined || pod.metadata.annotations?.[restartAnnotation] === expected.restartID,
      "RESTART_POD_ORIGIN_CHANGED");
    requireValue(fingerprint(sourcePins({ metadata: pod.metadata })) === fingerprint({ sourceSHA: expected.sourceSHA,
      provenanceSHA256: expected.provenanceSHA256, releaseID: expected.releaseID }), "RESTART_POD_SOURCE_CHANGED");
  }
  return { replicaSetUID: replicas[0].metadata.uid, podUIDs: pods.map(pod => pod.metadata.uid).sort() };
}

// Единственная разрешённая разница полного spec — собственная restart annotation, без source/release/image изменений.
function restartedSpec(spec, restartID) {
  const desired = structuredClone(spec);
  desired.template.metadata.annotations[restartAnnotation] = restartID;
  return desired;
}

// Закрытый формат плана не хранит spec/env/Secret references; hash связывает их без раскрытия.
export function validateRestartPlan(plan) {
  requireValue(exact(plan, ["schema", "context", "accessProfile", "clusterUID", "namespaceUID", "executorSHA", "restartID", "target"]) &&
    plan.schema === "kodex.application-restart/v1" && typeof plan.context === "string" &&
    /^[A-Za-z0-9_.:@/-]{1,160}$/.test(plan.context) && !/prod(?:uction)?/i.test(plan.context) &&
    ["kubectl", "k3s-sudo"].includes(plan.accessProfile) && uuid.test(plan.clusterUID) && uuid.test(plan.namespaceUID) &&
    sha40.test(plan.executorSHA) && plan.executorSHA !== "0".repeat(40) && uuid.test(plan.restartID), "RESTART_PLAN_INVALID");
  const target = plan.target;
  requireValue(exact(target, ["name", "uid", "resourceVersion", "generation", "replicas", "image", "sourceSHA", "provenanceSHA256", "releaseID",
    "beforeSpecSHA256", "afterSpecSHA256", "beforeTemplateSHA256", "afterTemplateSHA256", "replicaSetUID", "podUIDs"]) &&
    isApplicationTargetName(target.name) && uuid.test(target.uid) && /^[1-9][0-9]*$/.test(target.resourceVersion) &&
    Number.isSafeInteger(target.generation) && target.generation > 0 && Number.isSafeInteger(target.replicas) && target.replicas > 0 && target.replicas <= 16 &&
    /^[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}$/.test(target.image) && !target.image.endsWith("0".repeat(64)) &&
    sha40.test(target.sourceSHA) && target.sourceSHA !== "0".repeat(40) &&
    sha64.test(target.provenanceSHA256) && target.provenanceSHA256 !== "0".repeat(64) && uuid.test(target.releaseID) &&
    [target.beforeSpecSHA256, target.afterSpecSHA256, target.beforeTemplateSHA256, target.afterTemplateSHA256].every(value => sha64.test(value)) &&
    target.beforeSpecSHA256 !== target.afterSpecSHA256 && uuid.test(target.replicaSetUID) &&
    Array.isArray(target.podUIDs) && target.podUIDs.length === target.replicas && target.podUIDs.every(value => uuid.test(value)) &&
    new Set(target.podUIDs).size === target.podUIDs.length, "RESTART_TARGET_INVALID");
  return plan;
}

// План фиксирует ровно одно готовое приложение и текущие Pods; авторизация staging остаётся у operator transport.
export function planApplicationRestart({ context, accessProfile, clusterUID, namespaceUID, executorSHA, restartID, deployment, resources, name }) {
  const current = stableBoundary(deployment, name);
  requireValue(current.pod.metadata.annotations[restartAnnotation] !== restartID, "RESTART_ID_ALREADY_CURRENT");
  const desired = restartedSpec(current.spec, restartID);
  const target = { name, uid: current.metadata.uid, resourceVersion: current.metadata.resourceVersion,
    generation: current.metadata.generation, replicas: current.spec.replicas, image: current.beforeImage,
    sourceSHA: current.sourceSHA, provenanceSHA256: current.provenanceSHA256, releaseID: current.releaseID,
    beforeSpecSHA256: fingerprint(current.spec), afterSpecSHA256: fingerprint(desired),
    beforeTemplateSHA256: fingerprint(current.pod), afterTemplateSHA256: fingerprint(desired.template) };
  Object.assign(target, servingLineage(deployment, resources, target.beforeTemplateSHA256, target));
  return validateRestartPlan({ schema: "kodex.application-restart/v1", context, accessProfile, clusterUID, namespaceUID, executorSHA, restartID, target });
}

// Пересчёт плана проверяет все pins до UID/RV CAS; patch не содержит env или исходного spec.
export function prepareApplicationRestart(plan, deployment, resources) {
  validateRestartPlan(plan);
  const actual = planApplicationRestart({ ...plan, name: plan.target.name, deployment, resources });
  requireValue(fingerprint(actual) === fingerprint(plan), "RESTART_BASELINE_CHANGED");
  return [
    { op: "test", path: "/metadata/uid", value: plan.target.uid },
    { op: "test", path: "/metadata/resourceVersion", value: plan.target.resourceVersion },
    { op: "add", path: "/spec/template/metadata/annotations/kodex.dev~1application-restart", value: plan.restartID },
  ];
}

// UNKNOWN никогда не разрешает повтор mutation; прежний spec также не доказывает, что эффекта никогда не было.
export function observeApplicationRestart(plan, deployment, resources) {
  validateRestartPlan(plan);
  requireValue(deployment?.metadata?.uid === plan.target.uid && deployment.metadata.namespace === "kodex-system" &&
    deployment.metadata.name === plan.target.name, "RESTART_DEPLOYMENT_REPLACED");
  const actual = fingerprint(deployment.spec);
  if (actual === plan.target.beforeSpecSHA256)
    return { status: "NOT_CURRENT", code: "RESTART_NOT_CURRENT" };
  requireValue(actual === plan.target.afterSpecSHA256 && deployment.metadata.generation === plan.target.generation + 1 &&
    deployment.spec.template.metadata.annotations[restartAnnotation] === plan.restartID, "RESTART_SPEC_CHANGED");
  const current = stableBoundary(deployment, plan.target.name);
  requireValue(current.sourceSHA === plan.target.sourceSHA && current.provenanceSHA256 === plan.target.provenanceSHA256 &&
    current.releaseID === plan.target.releaseID && current.beforeImage === plan.target.image, "RESTART_SOURCE_CHANGED");
  const lineage = servingLineage(deployment, resources, plan.target.afterTemplateSHA256,
    { ...plan.target, restartID: plan.restartID }, plan.target.podUIDs);
  requireValue(lineage.replicaSetUID !== plan.target.replicaSetUID, "RESTART_REPLICASET_UNCHANGED");
  return { status: "PASS", image: plan.target.image, sourceSHA: plan.target.sourceSHA,
    provenanceSHA256: plan.target.provenanceSHA256, releaseID: plan.target.releaseID, ...lineage };
}
