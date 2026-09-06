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
import { mutateWithRetry, type MutationHeaders } from "@/shared/api/mutation";
import { asProblem, unwrap } from "@/shared/api/problem";

const readRetryDelaysMs = [0, 200, 600] as const;
const runtimeConfigurationReadRetryDelaysMs = [
  0, 200, 600, 1_500, 3_000,
] as const;

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
    runtimeConfigurationReadRetryDelaysMs,
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
    readRetryDelaysMs,
    signal,
  );
}

export async function saveAgentRuntime(
  agentRef: string,
  input: AgentRuntimeConfigurationInput,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutateWithRetry(
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
    await mutateWithRetry(
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

export async function bindRuntimeEnvironment(
  agentRef: string,
  environmentRef: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return (
    await mutateWithRetry(
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
  return readWithRetry(
    async () =>
      (
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
      ).data,
  );
}

async function readWithRetry<T>(
  request: () => Promise<T>,
  retryDelaysMs: readonly number[] = readRetryDelaysMs,
  signal?: AbortSignal,
): Promise<T> {
  let lastProblem = asProblem(new Error("Runtime read did not start"));
  for (const delayMs of retryDelaysMs) {
    signal?.throwIfAborted();
    if (delayMs > 0) {
      await new Promise<void>((resolve, reject) => {
        const timer = globalThis.setTimeout(() => {
          signal?.removeEventListener("abort", abort);
          resolve();
        }, delayMs);
        function abort(): void {
          globalThis.clearTimeout(timer);
          signal?.removeEventListener("abort", abort);
          reject(new DOMException("Runtime read aborted", "AbortError"));
        }
        signal?.addEventListener("abort", abort, { once: true });
      });
    }
    signal?.throwIfAborted();
    try {
      return await request();
    } catch (error) {
      signal?.throwIfAborted();
      lastProblem = asProblem(error);
      if (!lastProblem.retryable || delayMs === retryDelaysMs.at(-1)) {
        throw lastProblem;
      }
    }
  }
  throw lastProblem;
}
