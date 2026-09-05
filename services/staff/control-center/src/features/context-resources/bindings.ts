import * as sdk from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AgentContextBinding,
  AgentRuntimeConfigurationView,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import type { AsyncEntityOptionPage } from "@/shared/ui/async-entity-picker";
import type { ContextKind } from "./api";
export interface ContextBindingSnapshot {
  agentRef: string;
  agentName: string;
  projectRef: string;
  agentVersion: number;
  skillBindings: AgentContextBinding[];
  memoryBindings: AgentContextBinding[];
}
function validBinding(binding: AgentContextBinding, agentRef: string): boolean {
  return (
    binding.agentRef === agentRef &&
    !!binding.ref &&
    !!binding.resourceRef &&
    !!binding.revisionRef &&
    Number.isSafeInteger(binding.version) &&
    binding.version > 0 &&
    /^[a-f0-9]{64}$/.test(binding.digest)
  );
}
export function checkBindingView(
  view: Pick<
    AgentRuntimeConfigurationView,
    "agentVersion" | "skillBindings" | "memoryBindings"
  > & {
    configuration: Pick<
      AgentRuntimeConfigurationView["configuration"],
      "agentRef"
    >;
  },
  agentRef: string,
): void {
  if (
    view.configuration.agentRef !== agentRef ||
    !Number.isSafeInteger(view.agentVersion) ||
    view.agentVersion < 1 ||
    !Array.isArray(view.skillBindings) ||
    !Array.isArray(view.memoryBindings)
  )
    throw new Error("Invalid agent context binding snapshot");
  const all = [...view.skillBindings, ...view.memoryBindings];
  if (
    all.length > 128 ||
    all.some((binding) => !validBinding(binding, agentRef)) ||
    new Set(all.map((binding) => binding.ref)).size !== all.length ||
    [view.skillBindings, view.memoryBindings].some(
      (bindings) =>
        new Set(bindings.map((binding) => binding.resourceRef)).size !==
        bindings.length,
    )
  )
    throw new Error("Invalid agent context binding snapshot");
}
export async function bindingAgents(
  projectRef: string,
  query: string,
  pageToken: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  const page = (
    await unwrap(
      sdk.listOrganizationAgents({
        query: { projectRef, query, pageToken, pageSize: 40 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (page.items.some((item) => item.projectRef !== projectRef))
    throw new Error("Invalid context binding agent scope");
  return {
    items: page.items.map((item) => ({
      ref: item.ref,
      title: item.name,
      description: item.purpose,
      meta: item.state,
      disabled: item.system || item.state === "ARCHIVED",
    })),
    nextPageToken: page.nextPageToken,
  };
}
export async function readBindings(
  projectRef: string,
  agentRef: string,
  signal: AbortSignal,
): Promise<ContextBindingSnapshot> {
  const agent = (
    await unwrap(
      sdk.getAgent({ path: { agentRef }, signal: requestSignal(signal) }),
    )
  ).data;
  if (agent.ref !== agentRef || agent.projectRef !== projectRef || agent.system)
    throw new Error("Invalid context binding agent scope");
  const result = await unwrap(
    sdk.getAgentRuntimeConfiguration({
      path: { agentRef },
      signal: requestSignal(signal),
    }),
  );
  const view = result.data;
  checkBindingView(view, agentRef);
  if (result.etag !== `"${String(view.agentVersion)}"`)
    throw new Error("Invalid agent context binding ETag");
  return {
    agentRef,
    agentName: agent.name,
    projectRef,
    agentVersion: view.agentVersion,
    skillBindings: view.skillBindings,
    memoryBindings: view.memoryBindings,
  };
}
export function currentBinding(
  snapshot: ContextBindingSnapshot,
  kind: ContextKind,
  resourceRef: string,
): AgentContextBinding | undefined {
  return (
    kind === "skills" ? snapshot.skillBindings : snapshot.memoryBindings
  ).find((binding) => binding.resourceRef === resourceRef);
}
export async function changeBinding(
  snapshot: ContextBindingSnapshot,
  kind: ContextKind,
  resourceRef: string,
  target: { ref: string; digest: string },
  action: "bind" | "unbind",
  signal: AbortSignal,
): Promise<AgentContextBinding> {
  const previous = currentBinding(snapshot, kind, resourceRef);
  const revisionRef = action === "unbind" ? previous?.revisionRef : target.ref;
  const digest = action === "unbind" ? previous?.digest : target.digest;
  if (
    !revisionRef ||
    !digest ||
    !Number.isSafeInteger(snapshot.agentVersion) ||
    snapshot.agentVersion < 1
  )
    throw new Error("Context binding revision is unavailable");
  const result = await mutate((headers) => {
    if (!headers["If-Match"]) throw new Error("Agent version is required");
    const request = {
      body: { revisionRef, expectedBindingVersion: previous?.version ?? 0 },
      headers: {
        "If-Match": headers["If-Match"],
        "Idempotency-Key": headers["Idempotency-Key"],
        "X-CSRF-Token": headers["X-CSRF-Token"],
      },
      signal: requestSignal(signal),
    };
    return kind === "skills"
      ? sdk[
          action === "bind" ? "bindAgentSkillBundle" : "unbindAgentSkillBundle"
        ]({
          ...request,
          path: { agentRef: snapshot.agentRef, bundleRef: resourceRef },
        })
      : sdk[
          action === "bind"
            ? "bindAgentMemoryRecord"
            : "unbindAgentMemoryRecord"
        ]({
          ...request,
          path: { agentRef: snapshot.agentRef, recordRef: resourceRef },
        });
  }, snapshot.agentVersion);
  const receipt = result.data;
  if (
    !validBinding(receipt, snapshot.agentRef) ||
    receipt.resourceRef !== resourceRef ||
    receipt.revisionRef !== revisionRef ||
    receipt.digest !== digest ||
    receipt.version <= (previous?.version ?? 0) ||
    (previous && receipt.ref !== previous.ref)
  )
    throw new Error("Invalid context binding receipt");
  return receipt;
}
