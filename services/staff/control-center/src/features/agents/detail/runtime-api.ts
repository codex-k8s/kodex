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
import { asProblem, unwrap } from "@/shared/api/problem";

const readRetryDelaysMs = [0, 200, 600] as const;

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
  return readWithRetry(
    async () =>
      (
        await unwrap(
          getAgentRuntimeConfiguration({
            path: { agentRef },
            signal: requestSignal(),
          }),
        )
      ).data,
  );
}

export async function loadRuntimeCatalog(): Promise<RuntimeSelection[]> {
  return readWithRetry(
    async () =>
      (await unwrap(listRuntimeSelections({ signal: requestSignal() }))).data
        .items,
  );
}

export async function saveAgentRuntime(
  agentRef: string,
  input: AgentRuntimeConfigurationInput,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return reconcileRuntimeMutation(
    agentRef,
    async () =>
      (
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
      ).data,
    (view) => runtimeConfigurationMatches(view, input),
  );
}

export async function saveOverlayDraft(
  agentRef: string,
  content: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return reconcileRuntimeMutation(
    agentRef,
    async () =>
      (
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
      ).data,
    (view) => view.draftOverlay?.content === content,
  );
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
  return reconcileRuntimeMutation(
    agentRef,
    async () =>
      (
        await mutate(
          (headers) =>
            request({
              path: { agentRef },
              headers: versionHeaders(headers),
              signal: requestSignal(),
            }),
          agentVersion,
        )
      ).data,
    action === "VALIDATE"
      ? (view) => view.draftOverlay?.state === "VALID"
      : (view) =>
          view.draftOverlay === undefined &&
          view.publishedOverlay.state === "PUBLISHED",
  );
}

export async function bindRuntimeEnvironment(
  agentRef: string,
  environmentRef: string,
  agentVersion: number,
): Promise<AgentRuntimeConfigurationView> {
  return reconcileRuntimeMutation(
    agentRef,
    async () =>
      (
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
      ).data,
    (view) => view.environment.ref === environmentRef,
  );
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

async function readWithRetry<T>(request: () => Promise<T>): Promise<T> {
  let lastProblem = asProblem(new Error("Runtime read did not start"));
  for (const delayMs of readRetryDelaysMs) {
    if (delayMs > 0) {
      await new Promise<void>((resolve) =>
        globalThis.setTimeout(resolve, delayMs),
      );
    }
    try {
      return await request();
    } catch (error) {
      lastProblem = asProblem(error);
      if (!lastProblem.retryable || delayMs === readRetryDelaysMs.at(-1)) {
        throw lastProblem;
      }
    }
  }
  throw lastProblem;
}

async function reconcileRuntimeMutation(
  agentRef: string,
  mutateRuntime: () => Promise<AgentRuntimeConfigurationView>,
  matchesIntent: (view: AgentRuntimeConfigurationView) => boolean,
): Promise<AgentRuntimeConfigurationView> {
  try {
    return await mutateRuntime();
  } catch (error) {
    const mutationProblem = asProblem(error);
    if (!mutationProblem.retryable) throw mutationProblem;
    try {
      const authoritative = await loadAgentRuntime(agentRef);
      if (matchesIntent(authoritative)) return authoritative;
    } catch {
      // Сохраняем исходную ошибку, если авторитетная сверка недоступна.
    }
    throw mutationProblem;
  }
}

function runtimeConfigurationMatches(
  view: AgentRuntimeConfigurationView,
  input: AgentRuntimeConfigurationInput,
): boolean {
  const current = view.configuration;
  return (
    current.runtimeProfileRef === input.runtimeProfileRef &&
    current.model === input.model &&
    current.providerPolicy.mode === input.providerPolicyMode &&
    providerAccountsMatch(
      current.providerPolicy.accountCandidates,
      input.providerAccounts,
    )
  );
}

function providerAccountsMatch(
  current: ReadonlyArray<{ accountRef: string; weight: number }>,
  requested: ReadonlyArray<{ accountRef: string; weight: number }>,
): boolean {
  if (current.length !== requested.length) return false;
  const byReference = new Map(
    current.map((item) => [item.accountRef, item.weight] as const),
  );
  return requested.every(
    (item) => byReference.get(item.accountRef) === item.weight,
  );
}
