import { defineStore } from "pinia";
import {
  InMemoryWebStorage,
  UserManager,
  WebStorageStateStore,
} from "oidc-client-ts";
import { computed, ref } from "vue";

import { requestSignal } from "@/shared/api/client";
import {
  createClient,
  createConfig,
} from "@/shared/api/generated/openapi/client";
import {
  createOwnerSession,
  deleteOwnerSession,
  getBootstrapState,
  renewOwnerSession,
} from "@/shared/api/generated/openapi/sdk.gen";
import { csrfToken, etag, idempotencyKey } from "@/shared/api/mutation";
import {
  asProblem,
  resetUnauthorizedNotification,
  type AppProblem,
  unwrap,
} from "@/shared/api/problem";
import { runtimeConfig } from "@/shared/config/runtime";

export type SessionPhase =
  | "checking"
  | "authenticated"
  | "unauthenticated"
  | "forbidden"
  | "error";

const sessionRevisionKey = "kodex.session.revision";
const sessionRenewalIntervalMs = 5 * 60 * 1000;
const sessionProbeRetryDelaysMs = [250, 500, 1_000] as const;
const ownerSessionRetryDelaysMs = [250, 500, 1_000] as const;

async function withOwnerSessionRetry<T>(request: () => Promise<T>): Promise<T> {
  for (let attempt = 0; ; attempt += 1) {
    try {
      return await request();
    } catch (error) {
      const normalized = asProblem(error);
      const retryDelay = ownerSessionRetryDelaysMs[attempt];
      if (!normalized.retryable || retryDelay === undefined) throw normalized;
      await new Promise<void>((resolve) =>
        globalThis.setTimeout(resolve, retryDelay),
      );
    }
  }
}

function oidcManager(): UserManager {
  const config = runtimeConfig().oidc;
  return new UserManager({
    authority: config.authority,
    client_id: config.clientId,
    redirect_uri: config.redirectUri,
    post_logout_redirect_uri: config.postLogoutRedirectUri,
    response_type: "code",
    scope: config.scope,
    loadUserInfo: false,
    automaticSilentRenew: false,
    monitorSession: false,
    stateStore: new WebStorageStateStore({ store: window.sessionStorage }),
    userStore: new WebStorageStateStore({ store: new InMemoryWebStorage() }),
  });
}

export const useSessionStore = defineStore("session", () => {
  const phase = ref<SessionPhase>("checking");
  const problem = ref<AppProblem>();
  const revision = ref<number>(
    Number.parseInt(window.sessionStorage.getItem(sessionRevisionKey) ?? "0"),
  );
  let generation = 0;
  let renewalTimer: number | undefined;
  let renewalRequest: Promise<void> | undefined;
  let renewalController: AbortController | undefined;
  let loggingOut = false;

  const canLogout = computed(
    () => phase.value === "authenticated" && revision.value > 0,
  );

  function setUnauthenticated(): void {
    void cancelRenewal();
    generation += 1;
    revision.value = 0;
    window.sessionStorage.removeItem(sessionRevisionKey);
    phase.value = "unauthenticated";
  }

  async function probe(): Promise<void> {
    const current = ++generation;
    phase.value = "checking";
    problem.value = undefined;
    for (let attempt = 0; ; attempt += 1) {
      try {
        await unwrap(getBootstrapState({ signal: requestSignal() }));
        if (current !== generation) return;
        phase.value = "authenticated";
        startRenewal();
        resetUnauthorizedNotification();
        return;
      } catch (error) {
        if (current !== generation) return;
        const normalized = asProblem(error);
        const retryDelay = sessionProbeRetryDelaysMs[attempt];
        if (normalized.retryable && retryDelay !== undefined) {
          await new Promise<void>((resolve) =>
            globalThis.setTimeout(resolve, retryDelay),
          );
          if (current !== generation) return;
          continue;
        }
        problem.value = normalized;
        phase.value =
          normalized.kind === "unauthorized"
            ? "unauthenticated"
            : normalized.kind === "forbidden"
              ? "forbidden"
              : "error";
        return;
      }
    }
  }

  async function beginLogin(): Promise<void> {
    await oidcManager().signinRedirect();
  }

  async function completeLogin(): Promise<void> {
    const current = ++generation;
    phase.value = "checking";
    problem.value = undefined;
    const manager = oidcManager();
    try {
      const user = await manager.signinRedirectCallback();
      if (!user.access_token) throw new Error("OIDC bearer is unavailable");
      const oneUseClient = createClient(
        createConfig({
          auth: () => user.access_token,
          baseUrl: runtimeConfig().apiBaseUrl,
          credentials: "include",
        }),
      );
      const sessionIdempotencyKey = idempotencyKey();
      const response = await withOwnerSessionRetry(() =>
        unwrap(
          createOwnerSession({
            client: oneUseClient,
            headers: { "Idempotency-Key": sessionIdempotencyKey },
            signal: requestSignal(),
          }),
        ),
      );
      const parsedRevision = Number.parseInt(
        response.etag?.replaceAll('"', "") ?? "0",
      );
      if (!Number.isSafeInteger(parsedRevision) || parsedRevision < 1)
        throw new Error("Owner session revision is unavailable");
      await manager.removeUser();
      if (current !== generation) return;
      revision.value = parsedRevision;
      window.sessionStorage.setItem(sessionRevisionKey, String(parsedRevision));
      phase.value = "authenticated";
      startRenewal();
      resetUnauthorizedNotification();
    } catch (error) {
      if (current !== generation) return;
      problem.value = asProblem(error);
      phase.value = "error";
      throw error;
    }
  }

  async function logout(): Promise<void> {
    if (revision.value < 1) return;
    loggingOut = true;
    const pendingRenewal = cancelRenewal();
    try {
      await pendingRenewal;
      await unwrap(
        deleteOwnerSession({
          headers: {
            "Idempotency-Key": idempotencyKey(),
            "X-CSRF-Token": csrfToken(),
            "If-Match": etag(revision.value),
          },
          signal: requestSignal(),
        }),
      );
      setUnauthenticated();
    } catch (error) {
      loggingOut = false;
      if (phase.value === "authenticated") startRenewal();
      throw error;
    } finally {
      loggingOut = false;
    }
  }

  async function renew(): Promise<void> {
    if (phase.value !== "authenticated" || loggingOut) return;
    if (renewalRequest) return await renewalRequest;
    const controller = new AbortController();
    renewalController = controller;
    const pending = (async () => {
      try {
        await unwrap(
          renewOwnerSession({
            headers: { "X-CSRF-Token": csrfToken() },
            signal: AbortSignal.any([requestSignal(), controller.signal]),
          }),
        );
      } catch (error) {
        if (controller.signal.aborted) return;
        const normalized = asProblem(error);
        if (normalized.kind === "unauthorized") setUnauthenticated();
      }
    })();
    renewalRequest = pending;
    try {
      await pending;
    } finally {
      if (renewalRequest === pending) renewalRequest = undefined;
      if (renewalController === controller) renewalController = undefined;
    }
  }

  function startRenewal(): void {
    if (renewalTimer !== undefined || loggingOut) return;
    void renew();
    renewalTimer = window.setInterval(() => {
      void renew();
    }, sessionRenewalIntervalMs);
  }

  function cancelRenewal(): Promise<void> | undefined {
    if (renewalTimer !== undefined) {
      window.clearInterval(renewalTimer);
      renewalTimer = undefined;
    }
    renewalController?.abort();
    return renewalRequest;
  }

  return {
    phase,
    problem,
    canLogout,
    probe,
    beginLogin,
    completeLogin,
    renew,
    logout,
    invalidate: setUnauthenticated,
  };
});
