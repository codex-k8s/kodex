import type { IntegrationIntent } from "@/shared/api/generated/openapi/types.gen";

export interface IntegrationEffectField {
  key: string;
  value?: string | number | boolean;
  hidden: boolean;
  truncated: boolean;
}

// Совпадают с bounded scalar projection владельца OwnerGate; raw input не читается.
const fieldBytes = 4096;
const contentBytes = 16384;
const fieldCount = 64;
const privateField =
  /credential|password|token|header|secret|authorization|cookie|attachments|content_base64|workflow_inputs/i;
const fieldKey = /^[A-Za-z][A-Za-z0-9_]{0,127}$/;
const encoder = new TextEncoder();

function record(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function boundedText(
  value: string,
  limit: number,
): { value: string; bytes: number; truncated: boolean } {
  let bytes = 0;
  const characters: string[] = [];
  for (const character of value) {
    const size = encoder.encode(character).length;
    if (bytes + size > limit) break;
    characters.push(character);
    bytes += size;
  }
  const text = characters.join("");
  return { value: text, bytes, truncated: text.length !== value.length };
}

export function integrationEffectPreview(
  intent: IntegrationIntent | undefined,
) {
  const preview = intent?.effectPreview;
  const fields: IntegrationEffectField[] = [];
  const supplied = Array.isArray(preview?.fields) ? preview.fields : [];
  let complete =
    preview?.contentComplete === true && Array.isArray(preview.fields);
  let remaining = contentBytes;
  const seen = new Set<string>();
  const bounded = supplied.slice(0, fieldCount);
  const duplicateKeys = new Set<string>();
  for (const candidate of bounded) {
    if (!record(candidate) || typeof candidate.key !== "string") continue;
    if (seen.has(candidate.key)) duplicateKeys.add(candidate.key);
    seen.add(candidate.key);
  }
  seen.clear();
  if (supplied.length > fieldCount) complete = false;
  for (const candidate of bounded) {
    if (
      !record(candidate) ||
      typeof candidate.key !== "string" ||
      !fieldKey.test(candidate.key)
    ) {
      complete = false;
      continue;
    }
    if (seen.has(candidate.key)) continue;
    seen.add(candidate.key);
    const field: IntegrationEffectField = {
      key: candidate.key,
      hidden: true,
      truncated: false,
    };
    if (
      candidate.opaque === false &&
      !privateField.test(candidate.key) &&
      !duplicateKeys.has(candidate.key)
    ) {
      if (candidate.type === "STRING" && typeof candidate.value === "string") {
        const text = boundedText(
          candidate.value,
          Math.min(fieldBytes, remaining),
        );
        field.value = text.value;
        remaining -= text.bytes;
        field.hidden = false;
        field.truncated = candidate.truncated !== false || text.truncated;
      } else if (
        candidate.type === "INTEGER" &&
        Number.isSafeInteger(candidate.value)
      ) {
        field.value = candidate.value as number;
        field.hidden = false;
        field.truncated =
          candidate.truncated !== undefined && candidate.truncated !== false;
      } else if (
        candidate.type === "BOOLEAN" &&
        typeof candidate.value === "boolean"
      ) {
        field.value = candidate.value;
        field.hidden = false;
        field.truncated =
          candidate.truncated !== undefined && candidate.truncated !== false;
      }
    }
    if (field.hidden || field.truncated) complete = false;
    fields.push(field);
  }
  const scope = intent?.resourceScope;
  const owner = scope?.values.owner;
  const repository = scope?.values.repository;
  const repositoryName =
    scope?.kind === "GITHUB_REPOSITORY" &&
    typeof owner === "string" &&
    /^[A-Za-z0-9][A-Za-z0-9-]{0,38}$/.test(owner) &&
    typeof repository === "string" &&
    /^[A-Za-z0-9_.-]{1,100}$/.test(repository)
      ? `${owner}/${repository}`
      : undefined;
  const issue = fields.find(
    (field) =>
      field.key === "issue_number" && !field.hidden && !field.truncated,
  );
  const commentIssue =
    intent?.definitionKey === "github" &&
    intent.operation === "github.issue.comment.create" &&
    intent.capabilityKey === "github.issue.comment.create" &&
    repositoryName &&
    typeof issue?.value === "number" &&
    issue.value > 0
      ? issue.value
      : undefined;
  const technical: Record<string, string | number | boolean> = {};
  if (
    typeof preview?.inputDigest === "string" &&
    /^(?:sha256:)?[a-f0-9]{64}$/.test(preview.inputDigest)
  )
    technical.inputDigest = preview.inputDigest;
  if (
    Number.isSafeInteger(preview?.inputBytes) &&
    Number(preview?.inputBytes) >= 0
  )
    technical.inputBytes = preview?.inputBytes as number;
  if (typeof preview?.contentComplete === "boolean")
    technical.contentComplete = preview.contentComplete;
  if (
    typeof preview?.approvalPolicy === "string" &&
    ["NONE", "HUMAN_EACH_EFFECT", "HUMAN_SCOPED"].includes(
      preview.approvalPolicy,
    )
  )
    technical.approvalPolicy = preview.approvalPolicy;
  // fields и неизвестные preview keys не возвращаются в raw technical fallback.
  return { fields, complete, repositoryName, commentIssue, technical };
}
