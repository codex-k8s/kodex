import { requestSignal } from "@/shared/api/client";
import {
  bindAgentRuntimeEnvironment,
  createConfigOverlayDraft,
  getAgentRuntimeConfiguration,
  listRuntimeEnvironmentSets,
  listOrganizationRuntimeEnvironmentSets,
  listRuntimeSelections,
  publishAgentRuntimeConfiguration,
  publishConfigOverlayDraft,
  publishRuntimeEnvironmentVersion,
  validateConfigOverlayDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AgentRuntimeConfigurationInput,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentPage,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
  RuntimeSelection,
} from "@/shared/api/generated/openapi/types.gen";
import {
  mutate,
  mutateWithRetry,
  type MutationHeaders,
} from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { readWithRetry } from "@/shared/api/read-retry";

function versionHeaders(headers: MutationHeaders): {
  "If-Match": string;
  "Idempotency-Key": string;
  "X-CSRF-Token": string;
} {
  const version = headers["If-Match"];
  if (!version) throw new Error("Runtime resource version is unavailable");
  return {
    "If-Match": version,
    "Idempotency-Key": headers["Idempotency-Key"],
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

export async function loadAgentRuntime(
  agentRef: string,
  signal?: AbortSignal,
): Promise<AgentRuntimeConfigurationView> {
  return readWithRetry(
    async () =>
      (
        await unwrap(
          getAgentRuntimeConfiguration({
            path: { agentRef },
            signal: requestSignal(signal),
          }),
        )
      ).data,
    undefined,
    signal,
  );
}

export async function loadRuntimeCatalog(
  signal?: AbortSignal,
): Promise<RuntimeSelection[]> {
  return readWithRetry(
    async () =>
      (await unwrap(listRuntimeSelections({ signal: requestSignal(signal) })))
        .data.items,
    undefined,
    signal,
  );
}

type AgentRuntimeRequest = Parameters<
  typeof mutateWithRetry<AgentRuntimeConfigurationView>
>[0];

// Повторяет выбранную команду с прежней версией и возвращает только её ответ.
// Заголовки и signal создаются внутри каждой попытки, payload задаёт вызывающий.
async function mutateAgentRuntime(
  request: (options: {
    path: { agentRef: string };
    headers: ReturnType<typeof versionHeaders>;
    signal: AbortSignal;
  }) => ReturnType<AgentRuntimeRequest>,
  agentRef: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutateWithRetry(
      (headers) =>
        request({
          path: { agentRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    )
  ).data;
}

export function saveAgentRuntime(
  agentRef: string,
  input: AgentRuntimeConfigurationInput,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return mutateAgentRuntime(
    (options) => publishAgentRuntimeConfiguration({ ...options, body: input }),
    agentRef,
    agentVersion,
  );
}

export function saveOverlayDraft(
  agentRef: string,
  content: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return mutateAgentRuntime(
    (options) => createConfigOverlayDraft({ ...options, body: { content } }),
    agentRef,
    agentVersion,
  );
}

export function changeOverlay(
  agentRef: string,
  action: "VALIDATE" | "PUBLISH",
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  const request =
    action === "VALIDATE"
      ? validateConfigOverlayDraft
      : publishConfigOverlayDraft;
  return mutateAgentRuntime(request, agentRef, agentVersion);
}

export function bindRuntimeEnvironment(
  agentRef: string,
  environmentRef: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return mutateAgentRuntime(
    (options) =>
      bindAgentRuntimeEnvironment({ ...options, body: { environmentRef } }),
    agentRef,
    agentVersion,
  );
}

export async function saveRuntimeEnvironment(
  current: RuntimeEnvironmentSet,
  input: RuntimeEnvironmentInput,
): Promise<RuntimeEnvironmentSet> {
  return (
    await mutate(
      (headers) =>
        publishRuntimeEnvironmentVersion({
          path: { environmentRef: current.ref },
          body: input,
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      current.version,
    )
  ).data;
}

export async function searchRuntimeEnvironments(
  projectRef: string | undefined,
  search: string,
  pageToken?: string,
  pageSize = 30,
): Promise<RuntimeEnvironmentPage> {
  if (!projectRef) {
    let cursor = pageToken;
    const items: RuntimeEnvironmentPage["items"] = [];
    for (
      let attempt = 0;
      attempt < 10 && items.length < pageSize;
      attempt += 1
    ) {
      const page = await readWithRetry(
        async () =>
          (
            await unwrap(
              listOrganizationRuntimeEnvironmentSets({
                query: {
                  ...(search.trim() ? { query: search.trim() } : {}),
                  ...(cursor ? { pageToken: cursor } : {}),
                  pageSize,
                },
                signal: requestSignal(),
              }),
            )
          ).data,
      );
      items.push(...page.items.filter((item) => !item.projectRef));
      cursor = page.nextPageToken;
      if (!cursor) break;
    }
    return { items, ...(cursor ? { nextPageToken: cursor } : {}) };
  }
  return readWithRetry(
    async () =>
      (
        await unwrap(
          listRuntimeEnvironmentSets({
            path: { projectRef },
            query: {
              ...(search.trim() ? { query: search.trim() } : {}),
              ...(pageToken ? { pageToken } : {}),
              pageSize,
            },
            signal: requestSignal(),
          }),
        )
      ).data,
  );
}
