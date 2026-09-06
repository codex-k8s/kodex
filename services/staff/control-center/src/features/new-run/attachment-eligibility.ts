import { requestSignal } from "@/shared/api/client";
import { getRunAttachmentEligibility } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  GetRunAttachmentEligibilityData,
  RunAttachmentEligibility,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";

export type AttachmentEligibilityScope =
  GetRunAttachmentEligibilityData["query"] & { projectRef: string };

export async function loadAttachmentEligibility(
  scope: AttachmentEligibilityScope,
  signal: AbortSignal,
): Promise<RunAttachmentEligibility> {
  const { projectRef, ...query } = scope;
  const result = (
    await unwrap(
      getRunAttachmentEligibility({
        path: { projectRef },
        query,
        signal: AbortSignal.any([signal, requestSignal()]),
      }),
    )
  ).data;
  if (
    result.projectRef !== projectRef ||
    result.targetType !== scope.targetType ||
    result.targetRef !== scope.targetRef ||
    (result.runRef || undefined) !== (scope.runRef || undefined) ||
    typeof result.eligible !== "boolean" ||
    ![
      "AVAILABLE",
      "TARGET_UNAVAILABLE",
      "RUNTIME_NOT_READY",
      "AGENT_CAPABILITY_REQUIRED",
      "SESSION_UNAVAILABLE",
    ].includes(result.reason) ||
    result.eligible !== (result.reason === "AVAILABLE") ||
    typeof result.digest !== "string" ||
    !result.digest ||
    !Number.isSafeInteger(result.runVersion) ||
    result.runVersion < 0
  )
    throw new Error("Invalid attachment eligibility scope or state");
  return result;
}
