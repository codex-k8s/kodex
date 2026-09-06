import { listArtifactBindingTargets } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  Artifact,
  ArtifactBindingTarget,
  ArtifactBindingTargetReason,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { AppProblem, unwrap } from "@/shared/api/problem";

const reasons = new Set<ArtifactBindingTargetReason>([
  "AVAILABLE",
  "ALREADY_BOUND",
  "NOT_BOUND",
  "AGENT_CAPABILITY_REQUIRED",
  "AGENT_ARCHIVED",
  "ARTIFACT_UNAVAILABLE",
]);
const states = new Set<ArtifactBindingTarget["state"]>([
  "DRAFT",
  "READY",
  "RUNNING",
  "DISABLED",
  "ARCHIVED",
]);

export function bindingTargetEditable(
  target: ArtifactBindingTarget | undefined,
): boolean {
  return target ? (target.bound ? target.canUnbind : target.canBind) : false;
}

export async function loadBindingTargets(
  artifact: Pick<Artifact, "ref" | "version" | "projectRef">,
  query: string,
  pageToken: string | undefined,
  digest: string | undefined,
  signal: AbortSignal,
) {
  const page = (
    await unwrap(
      listArtifactBindingTargets({
        path: { artifactRef: artifact.ref },
        query: { query, pageSize: 30, pageToken },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  signal.throwIfAborted();
  if (
    page.artifactRef !== artifact.ref ||
    page.projectRef !== artifact.projectRef ||
    !Number.isSafeInteger(page.artifactVersion) ||
    page.artifactVersion < 1 ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    !page.digest ||
    !page.evaluatedAt ||
    new Set(page.items.map((item) => item.agentRef)).size !==
      page.items.length ||
    page.items.some(
      (item) =>
        !item.agentRef ||
        !item.name ||
        !Number.isSafeInteger(item.agentVersion) ||
        item.agentVersion < 1 ||
        !states.has(item.state) ||
        typeof item.bound !== "boolean" ||
        typeof item.canBind !== "boolean" ||
        typeof item.canUnbind !== "boolean" ||
        !reasons.has(item.bindReason) ||
        !reasons.has(item.unbindReason) ||
        item.canBind !== (item.bindReason === "AVAILABLE") ||
        item.canUnbind !== (item.unbindReason === "AVAILABLE") ||
        (item.bound && item.canBind) ||
        (!item.bound && item.canUnbind),
    )
  )
    throw new Error("Invalid artifact binding target page");
  if (
    page.artifactVersion !== artifact.version ||
    (digest && page.digest !== digest)
  )
    throw new AppProblem({
      status: 412,
      code: "VERSION_MISMATCH",
      kind: "conflict",
      retryable: true,
    });
  return page;
}
