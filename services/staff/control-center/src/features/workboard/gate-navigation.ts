import { getOwnerGate } from "@/shared/api/generated/openapi/sdk.gen";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

export function gateSelection(
  references: string[],
  selected: string,
  preferred: string,
): string {
  if (preferred) return "";
  return references.includes(selected) ? selected : (references[0] ?? "");
}

export async function readAddressedGate(
  gateRef: string,
  projectRef: string | undefined,
  signal: AbortSignal,
): Promise<OwnerGate> {
  const combined = AbortSignal.any([signal, requestSignal()]);
  const gate = (
    await unwrap(
      getOwnerGate({ path: { gateRef }, signal: combined, cache: "no-store" }),
    )
  ).data;
  combined.throwIfAborted();
  if (
    gate.ref !== gateRef ||
    (projectRef && gate.projectRef !== projectRef) ||
    !Number.isSafeInteger(gate.version) ||
    gate.version < 1 ||
    ![
      "OPEN",
      "APPROVED",
      "REJECTED",
      "CHANGES_REQUESTED",
      "CANCELLED",
      "EXPIRED",
    ].includes(gate.state) ||
    ![
      gate.title,
      gate.contextSummary,
      gate.consequencesSummary,
      gate.requestedBy.displayName,
    ].every((value) => typeof value === "string") ||
    !Array.isArray(gate.nextActions) ||
    !Array.isArray(gate.allowedDecisions) ||
    !Array.isArray(gate.decisionConsequences)
  )
    throw new Error("Invalid owner gate readback");
  return gate;
}
