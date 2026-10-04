import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";

const invalidGateScope = "Invalid owner gate scope";

// Область назначает владелец состояния: пустой projectRef не определяет её.
export function hasValidGateScope(
  gate: OwnerGate,
  organizationRef?: string,
): boolean {
  if (
    typeof gate.organizationRef !== "string" ||
    !gate.organizationRef.trim() ||
    (organizationRef !== undefined && gate.organizationRef !== organizationRef)
  )
    return false;
  const knownScopes: readonly string[] = ["PROJECT", "ORGANIZATION"];
  if (!knownScopes.includes(gate.scopeKind)) return false;
  if (gate.scopeKind === "PROJECT")
    return typeof gate.projectRef === "string" && !!gate.projectRef.trim();
  return gate.projectRef === undefined;
}

export function assertGateScope(
  gate: OwnerGate,
  organizationRef?: string,
): void {
  if (!hasValidGateScope(gate, organizationRef))
    throw new Error(invalidGateScope);
}

export function gateScopeKey(gate: OwnerGate): string {
  assertGateScope(gate);
  return JSON.stringify([
    gate.scopeKind,
    gate.organizationRef,
    gate.projectRef ?? "",
  ]);
}
