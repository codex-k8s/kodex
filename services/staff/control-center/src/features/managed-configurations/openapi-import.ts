import type { OpenApiInspectionResult } from "@/shared/api/generated/openapi/types.gen";

export type OpenAPIWriteRisk = "WRITE" | "SENSITIVE" | "DESTRUCTIVE";
export type OpenAPIWriteApproval = "HUMAN_EACH_EFFECT" | "HUMAN_SCOPED";

export function openAPIImportContent(input: {
  source: string;
  inspectedSource: string;
  inspection: OpenApiInspectionResult;
  name: string;
  version: string;
  selectedIds: string[];
  healthOperationId: string;
  risks: Record<string, OpenAPIWriteRisk>;
  approvals: Record<string, OpenAPIWriteApproval>;
}): string {
  if (
    !input.source ||
    input.source !== input.inspectedSource ||
    new TextEncoder().encode(input.source).length > 128 * 1024 ||
    !input.name.trim() ||
    input.name.length > 160 ||
    !/^[1-9]\d*\.\d+\.\d+$/.test(input.version) ||
    input.selectedIds.length === 0 ||
    input.selectedIds.length > 48 ||
    new Set(input.selectedIds).size !== input.selectedIds.length
  )
    throw new Error("OpenAPI import selection is invalid");
  const byId = new Map(
    input.inspection.operations.map((operation) => [
      operation.operationId,
      operation,
    ]),
  );
  const health = byId.get(input.healthOperationId);
  if (
    !health?.healthCandidate ||
    !input.selectedIds.includes(input.healthOperationId)
  )
    throw new Error("OpenAPI health operation is invalid");
  const choices = input.selectedIds.map((operationId) => {
    const operation = byId.get(operationId);
    if (!operation?.candidate || !operationId)
      throw new Error("OpenAPI operation is unavailable");
    if (operation.method === "GET")
      return { operationId, risk: "READ", approvalPolicy: "NONE" };
    const risk = input.risks[operationId] ?? "WRITE";
    const approvalPolicy = input.approvals[operationId] ?? "HUMAN_EACH_EFFECT";
    if (
      !["WRITE", "SENSITIVE", "DESTRUCTIVE"].includes(risk) ||
      !["HUMAN_EACH_EFFECT", "HUMAN_SCOPED"].includes(approvalPolicy)
    )
      throw new Error("OpenAPI write policy is invalid");
    return { operationId, risk, approvalPolicy };
  });
  const content = JSON.stringify({
    source: input.source,
    options: {
      name: input.name.trim(),
      version: input.version,
      healthOperationId: input.healthOperationId,
      choices,
    },
  });
  if (new TextEncoder().encode(content).length > 256 * 1024)
    throw new Error("OpenAPI import payload is too large");
  return content;
}
