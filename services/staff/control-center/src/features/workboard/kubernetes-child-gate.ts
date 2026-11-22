import type {
  KubernetesOwnerGateChild,
  OwnerGate,
} from "@/shared/api/generated/openapi/types.gen";
import { isRecord } from "@/shared/lib/exact-object";

const reference = /^[A-Za-z0-9_-]{8,96}$/;
const digest = /^[a-f0-9]{64}$/;
const label = /^[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?$/;
const namespace = /^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$/;
const uid = /^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$/;
const subresources = new Set([
  "",
  "status",
  "scale",
  "log",
  "eviction",
  "approval",
  "binding",
  "ephemeralcontainers",
]);
const childKeys = [
  "planRef",
  "childRef",
  "operation",
  "planDigest",
  "inputDigest",
  "targetRef",
  "targetRevision",
  "resource",
  "intendedName",
  "intendedGenerateName",
  "preview",
];
const addressKeys = [
  "group",
  "version",
  "resource",
  "namespace",
  "name",
  "uid",
  "resourceVersion",
  "subresource",
  "namespaced",
  "allNamespaces",
];
function exact(
  value: unknown,
  keys: string[],
): value is Record<string, unknown> {
  return (
    isRecord(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}
function text(value: unknown, max: number): value is string {
  return (
    typeof value === "string" &&
    new TextEncoder().encode(value).length <= max &&
    !/[\p{Cc}\uD800-\uDFFF]/u.test(value)
  );
}
function pathName(value: unknown): value is string {
  return (
    text(value, 4096) &&
    value !== "" &&
    value !== "." &&
    value !== ".." &&
    !/[/%\\]/.test(value)
  );
}
const previewReasons = new Set([
  "NOT_AUTHORIZED",
  "SENSITIVE_OR_UNCLASSIFIED_FIELDS_HIDDEN",
  "PROTECTED_MATERIAL_HIDDEN",
  "BUDGET_EXCEEDED",
  "PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE",
]);
function previewValue(kind: unknown, raw: unknown, redacted: unknown): boolean {
  if (
    typeof kind !== "string" ||
    typeof raw !== "string" ||
    typeof redacted !== "boolean"
  )
    return false;
  if (kind === "NONE") return raw === "";
  if (redacted || new TextEncoder().encode(raw).length > 512 * 1024)
    return false;
  try {
    const parsed: unknown = JSON.parse(raw);
    return kind === "NULL"
      ? parsed === null
      : kind === "STRING"
        ? typeof parsed === "string"
        : kind === "BOOLEAN"
          ? typeof parsed === "boolean"
          : kind === "NUMBER"
            ? typeof parsed === "number" && Number.isFinite(parsed)
            : kind === "ARRAY"
              ? Array.isArray(parsed)
              : kind === "OBJECT"
                ? isRecord(parsed)
                : false;
  } catch {
    return false;
  }
}
// Проверяет read-only submitted preview. Значения не используются для формирования команды.
export function validKubernetesGatePreview(
  value: unknown,
  inputDigest: unknown,
): boolean {
  if (
    !exact(value, [
      "inputDigest",
      "options",
      "changes",
      "secretMaterialRef",
      "limitations",
    ]) ||
    value.inputDigest !== inputDigest ||
    typeof inputDigest !== "string" ||
    !digest.test(inputDigest) ||
    !Array.isArray(value.options) ||
    value.options.length > 128 ||
    !Array.isArray(value.changes) ||
    value.changes.length > 2048 ||
    !Array.isArray(value.limitations) ||
    value.limitations.length > 4 ||
    new Set(value.limitations).size !== value.limitations.length ||
    value.limitations.some(
      (reason: unknown) =>
        typeof reason !== "string" || !previewReasons.has(reason),
    ) ||
    typeof value.secretMaterialRef !== "string" ||
    (value.secretMaterialRef !== "" &&
      !/^krs_[A-Za-z0-9_-]{4,92}$/.test(value.secretMaterialRef))
  )
    return false;
  if (new TextEncoder().encode(JSON.stringify(value)).length > 512 * 1024)
    return false;
  if (
    value.limitations.includes("NOT_AUTHORIZED") &&
    (value.options.length !== 0 ||
      value.changes.length !== 0 ||
      value.secretMaterialRef !== "")
  )
    return false;
  const keys = new Set<string>();
  for (const option of value.options) {
    if (
      !exact(option, ["key", "kind", "valueJson", "redacted"]) ||
      !text(option.key, 64) ||
      option.key === "" ||
      keys.has(option.key) ||
      !previewValue(option.kind, option.valueJson, option.redacted) ||
      (option.kind === "NONE" && !option.redacted)
    )
      return false;
    keys.add(option.key);
  }
  for (const change of value.changes) {
    if (
      !exact(change, [
        "operation",
        "path",
        "from",
        "valueKind",
        "valueJson",
        "redacted",
      ]) ||
      typeof change.operation !== "string" ||
      !["SUBMIT", "add", "remove", "replace", "copy", "move", "test"].includes(
        change.operation,
      ) ||
      !text(change.path, 4096) ||
      !change.path.startsWith("/") ||
      !text(change.from, 4096) ||
      (change.from !== "" && !change.from.startsWith("/")) ||
      !previewValue(change.valueKind, change.valueJson, change.redacted)
    )
      return false;
  }
  return true;
}
// Проверяет только response metadata. Решение и свежий допуск принадлежат owner transaction.
export function validKubernetesChildGate(gate: OwnerGate): boolean {
  const value: unknown = gate.kubernetesChild;
  if (value === undefined) return true;
  if (
    gate.integrationIntent !== undefined ||
    gate.reworkPlan !== undefined ||
    !exact(value, childKeys) ||
    !validKubernetesGatePreview(value.preview, value.inputDigest)
  )
    return false;
  for (const key of ["planRef", "childRef", "targetRef"])
    if (typeof value[key] !== "string" || !reference.test(value[key]))
      return false;
  for (const key of ["planDigest", "inputDigest"])
    if (typeof value[key] !== "string" || !digest.test(value[key]))
      return false;
  if (
    typeof value.targetRevision !== "number" ||
    !Number.isSafeInteger(value.targetRevision) ||
    value.targetRevision < 1 ||
    typeof value.operation !== "string" ||
    !/^kubernetes\.[a-z][a-z.-]{1,63}$/.test(value.operation)
  )
    return false;
  if (
    !Array.isArray(gate.allowedDecisions) ||
    gate.allowedDecisions.some(
      (decision) => !["APPROVE", "REJECT", "CANCEL"].includes(decision),
    )
  )
    return false;
  const preview = value.preview as Record<string, unknown>;
  const limitations = preview.limitations as string[];
  const hasForwardDisclosure = limitations.includes(
    "PORT_FORWARD_LOGICAL_STREAM_16_ACTIVE",
  );
  if (
    hasForwardDisclosure !==
    (value.operation === "kubernetes.port-forward" &&
      !limitations.includes("NOT_AUTHORIZED"))
  )
    return false;
  if (
    value.operation === "kubernetes.port-forward" &&
    (preview.options as Array<Record<string, unknown>>).some(
      (option) => option.key !== "ports" && option.key !== "timeoutSeconds",
    )
  )
    return false;
  const resource = value.resource;
  if (!exact(resource, addressKeys)) return false;
  for (const key of [
    "group",
    "version",
    "resource",
    "namespace",
    "name",
    "uid",
    "resourceVersion",
    "subresource",
  ])
    if (typeof resource[key] !== "string") return false;
  if (
    !label.test(resource.version as string) ||
    !label.test(resource.resource as string) ||
    !text(resource.group, 253) ||
    (resource.group !== "" &&
      !resource.group.split(".").every((part) => namespace.test(part))) ||
    typeof resource.namespaced !== "boolean" ||
    resource.allNamespaces !== false ||
    !subresources.has(resource.subresource as string)
  )
    return false;
  if (
    resource.namespaced
      ? !namespace.test(resource.namespace as string)
      : resource.namespace !== ""
  )
    return false;
  if (
    value.operation === "kubernetes.port-forward" &&
    (resource.group !== "" ||
      resource.version !== "v1" ||
      resource.resource !== "pods" ||
      resource.subresource !== "" ||
      !resource.namespaced)
  )
    return false;
  if (["kubernetes.create", "kubernetes.run"].includes(value.operation)) {
    if (
      value.operation === "kubernetes.run" &&
      (resource.group !== "" ||
        resource.version !== "v1" ||
        resource.resource !== "pods" ||
        !resource.namespaced ||
        value.intendedName === "" ||
        value.intendedGenerateName !== "")
    )
      return false;
    if (
      resource.name !== "" ||
      resource.uid !== "" ||
      resource.resourceVersion !== "" ||
      resource.subresource !== "" ||
      (value.intendedName === "") === (value.intendedGenerateName === "")
    )
      return false;
    return (
      (value.intendedName === "" && pathName(value.intendedGenerateName)) ||
      (value.intendedGenerateName === "" && pathName(value.intendedName))
    );
  }
  return (
    value.intendedName === "" &&
    value.intendedGenerateName === "" &&
    pathName(resource.name) &&
    uid.test(resource.uid as string) &&
    resource.uid !== "00000000-0000-0000-0000-000000000000" &&
    text(resource.resourceVersion, 256) &&
    resource.resourceVersion !== "" &&
    !/\s/u.test(resource.resourceVersion)
  );
}
export function kubernetesChildGateSummary(
  gate: OwnerGate,
): KubernetesOwnerGateChild | undefined {
  return validKubernetesChildGate(gate) ? gate.kubernetesChild : undefined;
}
