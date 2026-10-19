import {
  getProjectPurgeRecovery,
  resolveProjectPurgeRecovery as resolveRecovery,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { ProjectPurgeRecovery } from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export async function loadProjectPurgeRecovery(
  projectRef: string,
  signal: AbortSignal,
): Promise<ProjectPurgeRecovery> {
  return (
    await unwrap(
      getProjectPurgeRecovery({
        path: { projectRef },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}

export async function resolveProjectPurgeRecovery(
  preview: ProjectPurgeRecovery,
  key: string,
  signal: AbortSignal,
): Promise<ProjectPurgeRecovery> {
  return (
    await mutate(
      (headers) =>
        resolveRecovery({
          path: { projectRef: preview.projectRef },
          body: {
            snapshotDigest: preview.snapshotDigest,
            acknowledgeUnknownOutcome: true,
          },
          headers: {
            "Idempotency-Key": headers["Idempotency-Key"],
            "X-CSRF-Token": headers["X-CSRF-Token"],
            "If-Match": headers["If-Match"] ?? "",
          },
          signal: requestSignal(signal),
        }),
      preview.version,
      key,
    )
  ).data;
}
