import type {
  RunNode,
  RunGraph,
} from "@/shared/api/generated/openapi/types.gen";

const opaque = /^[A-Za-z0-9_-]{8,128}$/u;
const key = /^[A-Za-z0-9_-]{1,96}$/u;
const digest = /^[a-f0-9]{64}$/u;
const head = /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/u;
const repository = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/u;
const kinds = [
  "ARTIFACT",
  "ARTIFACT_REVIEW",
  "PULL_REQUEST",
  "REVIEW",
  "QA",
  "INTEGRATION_RECEIPT",
  "DEPLOYMENT",
];
type RecordValue = Record<string, unknown>;
function object(value: unknown): value is RecordValue {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
function text(value: unknown, maximum: number): value is string {
  return typeof value === "string" && Array.from(value).length <= maximum;
}
function validResult(value: unknown): value is RecordValue {
  if (
    !object(value) ||
    !text(value.key, 96) ||
    !key.test(value.key) ||
    !text(value.kind, 96) ||
    !kinds.includes(value.kind) ||
    !text(value.digest, 64) ||
    !digest.test(value.digest)
  )
    return false;
  const fields = [
    "key",
    "kind",
    "digest",
    "artifactRef",
    "artifactRevision",
    "receiptRef",
    "repository",
    "number",
    "headSha",
    "verdict",
    "targetRef",
    "bundleSha256",
    "imageDigest",
    "healthOk",
  ];
  if (Object.keys(value).some((field) => !fields.includes(field))) return false;
  for (const field of ["artifactRef", "receiptRef"])
    if (
      value[field] !== undefined &&
      (!text(value[field], 128) || !opaque.test(value[field]))
    )
      return false;
  for (const [field, maximum] of [
    ["artifactRevision", 96],
    ["repository", 200],
    ["headSha", 64],
    ["targetRef", 96],
  ] as const)
    if (value[field] !== undefined && !text(value[field], maximum))
      return false;
  if (
    value.number !== undefined &&
    (typeof value.number !== "number" ||
      !Number.isSafeInteger(value.number) ||
      value.number < 0)
  )
    return false;
  if (
    value.verdict !== undefined &&
    value.verdict !== "PASS" &&
    (!["ARTIFACT_REVIEW", "REVIEW", "QA"].includes(value.kind) ||
      value.verdict !== "CHANGES_REQUIRED")
  )
    return false;
  if (value.healthOk !== undefined && typeof value.healthOk !== "boolean")
    return false;
  if (
    value.bundleSha256 !== undefined &&
    (!text(value.bundleSha256, 64) || !digest.test(value.bundleSha256))
  )
    return false;
  if (
    value.imageDigest !== undefined &&
    (!text(value.imageDigest, 71) ||
      !/^sha256:[a-f0-9]{64}$/u.test(value.imageDigest))
  )
    return false;
  return true;
}

// Отрицательные facts не становятся accepted results; private provenance не сохраняется в cache.
export function validWorkflowAttemptEvidence(node: RunNode): boolean {
  if (
    !object(node) ||
    (node.workflowResults !== undefined && !Array.isArray(node.workflowResults))
  )
    return false;
  const history: unknown[] = node.workflowResults ?? [];
  if (history.length > 4) return false;
  const seen = new Set<number>();
  return history.every((attempt: unknown) => {
    if (
      !object(attempt) ||
      Object.keys(attempt).some(
        (field) =>
          ![
            "attempt",
            "runRef",
            "accepted",
            "reason",
            "results",
            "evidence",
            "effectiveState",
          ].includes(field),
      )
    )
      return false;
    if (
      typeof attempt.attempt !== "number" ||
      !Number.isInteger(attempt.attempt) ||
      attempt.attempt < 1 ||
      attempt.attempt > 4 ||
      seen.has(attempt.attempt)
    )
      return false;
    seen.add(attempt.attempt);
    if (
      !text(attempt.runRef, 128) ||
      !opaque.test(attempt.runRef) ||
      typeof attempt.accepted !== "boolean" ||
      !text(attempt.reason, 96)
    )
      return false;
    if (
      !Array.isArray(attempt.results) ||
      attempt.results.length > 4 ||
      attempt.results.some((result: unknown) => !validResult(result))
    )
      return false;
    if (!attempt.accepted && attempt.results.length !== 0) return false;
    if (
      attempt.results.some(
        (result: RecordValue) =>
          result.verdict !== undefined && result.verdict !== "PASS",
      )
    )
      return false;
    if (attempt.effectiveState !== undefined && !["CURRENT", "HISTORICAL", "STALE_DEPENDENCY", "REWORK_PENDING", "WAITING_OWNER", "BLOCKED"].includes(String(attempt.effectiveState))) return false;
    if (attempt.evidence === undefined) return true;
    if (
      !Array.isArray(attempt.evidence) ||
      attempt.accepted ||
      attempt.reason !== "CHANGES_REQUIRED" ||
      attempt.evidence.length < 1 ||
      attempt.evidence.length > 4
    )
      return false;
    if (attempt.evidence.some((result: unknown) => !validResult(result)))
      return false;
    const facts = attempt.evidence as RecordValue[];
    if (
      !facts.some(
        (result) =>
          ["ARTIFACT_REVIEW", "REVIEW", "QA"].includes(String(result.kind)) &&
          result.verdict === "CHANGES_REQUIRED",
      )
    )
      return false;
    return facts.every((result) => {
      if (result.kind === "ARTIFACT_REVIEW") return !!result.artifactRef && !!result.artifactRevision && ["PASS", "CHANGES_REQUIRED"].includes(String(result.verdict));
      if (result.kind !== "REVIEW" && result.kind !== "QA")
        return result.verdict === undefined || result.verdict === "PASS";
      if (
        !text(result.repository, 200) ||
        !repository.test(result.repository) ||
        typeof result.number !== "number" ||
        result.number < 1 ||
        !text(result.headSha, 64) ||
        !head.test(result.headSha) ||
        !["PASS", "CHANGES_REQUIRED"].includes(String(result.verdict))
      )
        return false;
      if (result.kind === "REVIEW") return !!result.receiptRef;
      return !!result.artifactRef && !!result.artifactRevision;
    });
  });
}

// Проверка общего history envelope предшествует любому изменению store из HTTP/WS graph.
export function assertWorkflowGraphEvidence(graph: RunGraph): void {
  if (
    !Array.isArray(graph.nodes) ||
    graph.nodes.some((node) => !validWorkflowAttemptEvidence(node))
  )
    throw new Error("Invalid workflow graph evidence");
}
