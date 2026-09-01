import { requestSignal } from "@/shared/api/client";
import {
  bindAgentRuntimeEnvironment,
  createConfigOverlayDraft,
  getAgentRuntimeConfiguration,
  listRuntimeEnvironmentSets,
  listRuntimeSelections,
  publishAgentRuntimeConfiguration,
  publishConfigOverlayDraft,
  validateConfigOverlayDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AgentRuntimeConfigurationInput,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentPage,
  RuntimeSelection,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

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
): Promise<AgentRuntimeConfigurationView> {
  return (
    await unwrap(
      getAgentRuntimeConfiguration({
        path: { agentRef },
        signal: requestSignal(),
      }),
    )
  ).data;
}

export async function loadRuntimeCatalog(): Promise<RuntimeSelection[]> {
  return (await unwrap(listRuntimeSelections({ signal: requestSignal() }))).data
    .items;
}

export async function saveAgentRuntime(
  agentRef: string,
  input: AgentRuntimeConfigurationInput,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutate(
      (headers) =>
        publishAgentRuntimeConfiguration({
          path: { agentRef },
          body: input,
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    )
  ).data;
}

export async function saveOverlayDraft(
  agentRef: string,
  content: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutate(
      (headers) =>
        createConfigOverlayDraft({
          path: { agentRef },
          body: { content },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    )
  ).data;
}

export async function changeOverlay(
  agentRef: string,
  action: "VALIDATE" | "PUBLISH",
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  const request =
    action === "VALIDATE"
      ? validateConfigOverlayDraft
      : publishConfigOverlayDraft;
  return (
    await mutate(
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

export async function bindRuntimeEnvironment(
  agentRef: string,
  environmentRef: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutate(
      (headers) =>
        bindAgentRuntimeEnvironment({
          path: { agentRef },
          body: { environmentRef },
          headers: versionHeaders(headers),
          signal: requestSignal(),
        }),
      agentVersion,
    )
  ).data;
}

export async function searchRuntimeEnvironments(
  projectRef: string,
  search: string,
  pageToken?: string,
): Promise<RuntimeEnvironmentPage> {
  return (
    await unwrap(
      listRuntimeEnvironmentSets({
        path: { projectRef },
        query: {
          ...(search.trim() ? { query: search.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
          pageSize: 30,
        },
        signal: requestSignal(),
      }),
    )
  ).data;
}
