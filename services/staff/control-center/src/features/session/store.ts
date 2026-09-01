import { defineStore } from "pinia";
import {
  InMemoryWebStorage,
  UserManager,
  WebStorageStateStore,
} from "oidc-client-ts";
import { computed, ref } from "vue";

import {
  consumeOidcIntent,
  createRuntimeEnvironmentPolicyIntent,
  createRuntimeSecretRevealIntent,
  oidcReauthIntentStorageKey,
  recordRuntimeEnvironmentPolicyReauthCompletion,
  runtimeEnvironmentPolicyReauthCompletionStorageKey,
  type OidcIntent,
  type RuntimeEnvironmentPolicyOperation,
} from "./reauth";

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
const runtimeSecretRevealPendingLifetimeMs = 5 * 60 * 1000;

export interface LoginCompletion {
  readonly kind: "login" | "runtime-secret" | "runtime-environment-policy";
  readonly returnPath?: string;
}

interface PendingRuntimeSecretReveal {
  readonly expiresAt: number;
  readonly projectRef: string;
  readonly secretRef: string;
}

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
  const requestTimeoutInSeconds = Math.max(
    1,
    Math.ceil(runtimeConfig().requestTimeoutMs / 1_000),
  );
  return new UserManager({
    authority: config.authority,
    client_id: config.clientId,
    redirect_uri: config.redirectUri,
    post_logout_redirect_uri: config.postLogoutRedirectUri,
    response_type: "code",
    scope: config.scope,
    requestTimeoutInSeconds,
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
  const pendingRuntimeSecretRevealState = ref<PendingRuntimeSecretReveal>();
  let generation = 0;
  let loginCompletionRequest: Promise<LoginCompletion> | undefined;
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
    window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    pendingRuntimeSecretRevealState.value = undefined;
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
    window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    pendingRuntimeSecretRevealState.value = undefined;
    await oidcManager().signinRedirect();
  }

  async function beginRuntimeSecretRevealReauth(input: {
    projectRef: string;
    secretRef: string;
  }): Promise<void> {
    const intent = createRuntimeSecretRevealIntent(
      input.projectRef,
      input.secretRef,
    );
    pendingRuntimeSecretRevealState.value = undefined;
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    window.sessionStorage.setItem(
      oidcReauthIntentStorageKey,
      JSON.stringify(intent),
    );
    try {
      await oidcManager().signinRedirect({
        max_age: 0,
        prompt: "login",
        state: intent,
      });
    } catch (error) {
      window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
      throw error;
    }
  }

  async function beginRuntimeEnvironmentPolicyReauth(input: {
    environmentRef?: string;
    operation: RuntimeEnvironmentPolicyOperation;
    projectRef: string;
  }): Promise<void> {
    const intent = createRuntimeEnvironmentPolicyIntent(
      input.projectRef,
      input.operation,
      input.environmentRef,
    );
    pendingRuntimeSecretRevealState.value = undefined;
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    window.sessionStorage.setItem(
      oidcReauthIntentStorageKey,
      JSON.stringify(intent),
    );
    try {
      await oidcManager().signinRedirect({
        max_age: 0,
        prompt: "login",
        state: intent,
      });
    } catch (error) {
      window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
      throw error;
    }
  }

  async function performLoginCompletion(): Promise<LoginCompletion> {
    const current = ++generation;
    phase.value = "checking";
    problem.value = undefined;
    const manager = oidcManager();
    let accessToken = "";
    let callbackUser:
      | Awaited<ReturnType<UserManager["signinRedirectCallback"]>>
      | undefined;
    try {
      callbackUser = await manager.signinRedirectCallback();
      if (!callbackUser.access_token)
        throw new Error("OIDC bearer is unavailable");
      const intent: OidcIntent = consumeOidcIntent(
        callbackUser.state,
        window.sessionStorage,
      );
      accessToken = callbackUser.access_token;
      const oneUseClient = createClient(
        createConfig({
          auth: () => accessToken,
          baseUrl: runtimeConfig().apiBaseUrl,
          credentials: "include",
        }),
      );
      const sessionIdempotencyKey = idempotencyKey();
      const response = await withOwnerSessionRetry(() =>
        unwrap(
          createOwnerSession({
            body:
              intent.kind === "runtime-secret"
                ? {
                    purpose: {
                      kind: "RUNTIME_SECRET_REVEAL",
                      projectRef: intent.projectRef,
                      secretRef: intent.secretRef,
                    },
                  }
                : undefined,
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
      if (current !== generation)
        throw new Error("OIDC callback was superseded");
      revision.value = parsedRevision;
      window.sessionStorage.setItem(sessionRevisionKey, String(parsedRevision));
      phase.value = "authenticated";
      startRenewal();
      resetUnauthorizedNotification();
      if (intent.kind === "runtime-secret") {
        pendingRuntimeSecretRevealState.value = {
          expiresAt: Date.now() + runtimeSecretRevealPendingLifetimeMs,
          projectRef: intent.projectRef,
          secretRef: intent.secretRef,
        };
        return { kind: intent.kind, returnPath: intent.returnPath };
      }
      if (intent.kind === "runtime-environment-policy") {
        recordRuntimeEnvironmentPolicyReauthCompletion(
          intent,
          window.sessionStorage,
        );
        return { kind: intent.kind, returnPath: intent.returnPath };
      }
      return { kind: "login" };
    } catch (error) {
      if (current === generation) {
        problem.value = asProblem(error);
        phase.value = "error";
      }
      throw error;
    } finally {
      accessToken = "";
      if (callbackUser) {
        callbackUser.access_token = "";
        callbackUser.id_token = undefined;
        callbackUser.refresh_token = undefined;
      }
      await manager.removeUser();
    }
  }

  async function completeLogin(): Promise<LoginCompletion> {
    if (loginCompletionRequest) return await loginCompletionRequest;
    const pending = performLoginCompletion();
    loginCompletionRequest = pending;
    try {
      return await pending;
    } finally {
      if (loginCompletionRequest === pending)
        loginCompletionRequest = undefined;
    }
  }

  function pendingRuntimeSecretReveal(projectRef: string): string | undefined {
    const pending = pendingRuntimeSecretRevealState.value;
    if (!pending) return undefined;
    if (pending.expiresAt <= Date.now()) {
      pendingRuntimeSecretRevealState.value = undefined;
      return undefined;
    }
    return pending.projectRef === projectRef ? pending.secretRef : undefined;
  }

  function hasPendingRuntimeSecretReveal(
    projectRef: string,
    secretRef: string,
  ): boolean {
    return pendingRuntimeSecretReveal(projectRef) === secretRef;
  }

  function consumePendingRuntimeSecretReveal(
    projectRef: string,
    secretRef: string,
  ): boolean {
    if (!hasPendingRuntimeSecretReveal(projectRef, secretRef)) return false;
    pendingRuntimeSecretRevealState.value = undefined;
    return true;
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
    beginRuntimeSecretRevealReauth,
    beginRuntimeEnvironmentPolicyReauth,
    completeLogin,
    pendingRuntimeSecretReveal,
    hasPendingRuntimeSecretReveal,
    consumePendingRuntimeSecretReveal,
    renew,
    logout,
    invalidate: setUnauthenticated,
  };
});
