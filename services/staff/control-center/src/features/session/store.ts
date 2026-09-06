import { defineStore } from "pinia";
import { computed, onScopeDispose, ref } from "vue";
import { clearProviderLifecycleAttempts } from "@/features/providers/lifecycle-attempt";
import { environmentDraftReauthKey } from "@/features/runtime/environment-draft-reauth";
import { emailAttemptStorageKey } from "@/features/integrations/email-attempt";
import { mailboxCredentialRecoveryKey } from "@/features/integrations/email-credential-recovery";
import { gitSourceRecoveryKey } from "@/features/managed-configurations/git-source";
import { clearWriteBackRecovery } from "@/features/managed-configurations/writeback/model";
import { clearPublicationAttempts } from "@/features/runtime/publication-attempt";

import {
  consumePendingBrowserIntent,
  createEmailReconciliationIntent,
  type EmailReconciliationIntent,
  createRuntimeEnvironmentPolicyIntent,
  createRuntimeSecretRevealIntent,
  oidcReauthIntentStorageKey,
  recordRuntimeEnvironmentPolicyReauthCompletion,
  runtimeEnvironmentPolicyReauthCompletionStorageKey,
  type OidcIntent,
  type RuntimeEnvironmentPolicyOperation,
} from "./reauth";
import {
  SessionRenewalBus,
  SessionRenewalCoordinator,
} from "./renewal-coordinator";

import { requestSignal } from "@/shared/api/client";
import {
  beginOwnerAuthorization,
  completeOwnerAuthorization,
  getOwnerSession,
  deleteOwnerSession,
  getBootstrapState,
  renewOwnerSession,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  OwnerAuthorizationInput,
  OwnerSessionMetadata,
} from "@/shared/api/generated/openapi/types.gen";
import {
  authorizationCallback,
  authorizationRedirect,
  browserSessionIdentity,
  browserSessionTiming,
  type BrowserSessionTiming,
} from "./browser-session";
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
const authorizationStateKey = "kodex.session.authorization-state";
const sessionRenewalRetryDelaysMs = [1_000, 5_000, 15_000, 60_000] as const;
const sessionProbeRetryDelaysMs = [250, 500, 1_000] as const;
const ownerSessionRetryDelaysMs = [250, 500, 1_000] as const;
const runtimeSecretRevealPendingLifetimeMs = 5 * 60 * 1000;
const renewalChannelName = "kodex.session";

export interface LoginCompletion {
  readonly kind:
    | "login"
    | "runtime-secret"
    | "runtime-environment-policy"
    | "email-reconciliation";
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

function ownerSessionRevision(etagValue?: string): number {
  const match = /^"([1-9][0-9]*)"$/.exec(etagValue ?? "");
  const parsed = Number(match?.[1] ?? 0);
  if (!Number.isSafeInteger(parsed) || parsed < 1)
    throw new Error("Owner session revision is unavailable");
  return parsed;
}

export const useSessionStore = defineStore("session", () => {
  const phase = ref<SessionPhase>("checking");
  const problem = ref<AppProblem>();
  const metadata = ref<OwnerSessionMetadata>();
  let timing: BrowserSessionTiming | undefined;
  let metadataRequest: Promise<void> | undefined;
  const connectionIdentity = computed(() =>
    phase.value === "authenticated" && metadata.value
      ? browserSessionIdentity(metadata.value)
      : "",
  );
  const revision = ref<number>(
    Number.parseInt(window.sessionStorage.getItem(sessionRevisionKey) ?? "0"),
  );
  const pendingRuntimeSecretRevealState = ref<PendingRuntimeSecretReveal>();
  const pendingEmailConfirmation = ref<{
    intent: EmailReconciliationIntent;
    expiresAt: number;
  }>();
  let generation = 0;
  const loginFailed = ref(false);
  let loginRedirectRequest: Promise<void> | undefined;
  let loginCompletionRequest: Promise<LoginCompletion> | undefined;
  let renewalTimer: number | undefined;
  let renewalRequest: Promise<void> | undefined;
  let renewalController: AbortController | undefined;
  let renewalRetryTimer: number | undefined;
  let renewalFailures = 0;
  let loggingOut = false;
  const tabId = crypto.randomUUID();
  const renewalChannel =
    typeof BroadcastChannel === "undefined"
      ? undefined
      : new BroadcastChannel(renewalChannelName);
  const renewalBus = new SessionRenewalBus(renewalChannel, revision.value);
  const renewalCoordinator = new SessionRenewalCoordinator(
    window.localStorage,
    tabId,
  );

  renewalBus.subscribe(() => {
    void refreshMetadata();
  });
  const handleWake = (): void => {
    if (document.visibilityState === "hidden" || loggingOut || !metadata.value)
      return;
    if (phase.value !== "authenticated" && phase.value !== "error") return;
    if (
      timing &&
      timing.renewAt <= Date.now() &&
      metadata.value.renewalMode === "BACKEND_REFRESH"
    ) {
      phase.value = "authenticated";
      void renew();
    } else void refreshMetadata();
  };
  document.addEventListener("visibilitychange", handleWake);
  window.addEventListener("pageshow", handleWake);
  window.addEventListener("focus", handleWake);
  window.addEventListener("online", handleWake);
  onScopeDispose(() => {
    generation += 1;
    loggingOut = true;
    void cancelRenewal();
    renewalBus.close();
    document.removeEventListener("visibilitychange", handleWake);
    window.removeEventListener("pageshow", handleWake);
    window.removeEventListener("focus", handleWake);
    window.removeEventListener("online", handleWake);
  });
  const canLogout = computed(
    () => phase.value === "authenticated" && revision.value > 0,
  );

  function acceptMetadata(
    value: OwnerSessionMetadata,
    elapsedMs: number,
  ): void {
    const next = browserSessionTiming(value, Date.now(), elapsedMs);
    if (
      metadata.value?.generation === value.generation &&
      value.version < metadata.value.version
    )
      throw new Error("Browser session metadata moved backwards");
    metadata.value = value;
    timing = next;
    revision.value = value.sessionRevision;
    renewalBus.observeRevision(value.sessionRevision);
    window.sessionStorage.setItem(
      sessionRevisionKey,
      String(value.sessionRevision),
    );
    problem.value = undefined;
  }

  async function redirectAuthorization(
    body: OwnerAuthorizationInput,
  ): Promise<void> {
    const current = generation;
    await cancelRenewal();
    const response = await unwrap(
      beginOwnerAuthorization({ body, signal: requestSignal() }),
    );
    if (current !== generation)
      throw new Error("Owner authorization was superseded");
    const target = authorizationRedirect(
      response.data.authorizationUrl,
      runtimeConfig().oidc.authority,
    );
    const state = new URL(target).searchParams.get("state");
    if (!state || !/^[A-Za-z0-9_-]{43}$/.test(state))
      throw new Error("Owner authorization state is unavailable");
    window.sessionStorage.setItem(authorizationStateKey, state);
    window.location.assign(target);
  }

  function canObserveSession(): boolean {
    return (
      !loggingOut &&
      (phase.value === "authenticated" || phase.value === "error")
    );
  }
  function canRenewSession(): boolean {
    return !loggingOut && phase.value === "authenticated";
  }
  function isCurrentSession(current: number): boolean {
    return current === generation && !loggingOut;
  }
  function currentMetadataRequest(): Promise<void> | undefined {
    return metadataRequest;
  }
  async function refreshMetadata(): Promise<void> {
    if (!canObserveSession()) return;
    if (metadataRequest) return await metadataRequest;
    if (renewalRequest) await renewalRequest;
    if (!canObserveSession()) return;
    const existing = currentMetadataRequest();
    if (existing) return await existing;
    const current = generation;
    const started = performance.now();
    const pending = (async () => {
      try {
        const response = await unwrap(
          getOwnerSession({ signal: requestSignal() }),
        );
        if (!isCurrentSession(current)) return;
        acceptMetadata(response.data, performance.now() - started);
        phase.value = "authenticated";
        renewalFailures = 0;
        startRenewal();
      } catch (error) {
        if (isCurrentSession(current)) handleRenewalFailure(error);
      }
    })();
    metadataRequest = pending;
    try {
      await pending;
    } finally {
      if (metadataRequest === pending) metadataRequest = undefined;
    }
  }

  function handleRenewalFailure(error: unknown): void {
    const normalized = asProblem(error);
    if (
      normalized.kind === "unauthorized" ||
      !timing ||
      timing.deadline <= Date.now()
    ) {
      setUnauthenticated();
      return;
    }
    const delay = sessionRenewalRetryDelaysMs[renewalFailures];
    if (normalized.retryable && delay !== undefined) {
      renewalFailures += 1;
      const boundedDelay = Math.min(
        delay + Math.floor(Math.random() * 250),
        Math.max(0, timing.deadline - Date.now()),
      );
      if (renewalRetryTimer !== undefined)
        window.clearTimeout(renewalRetryTimer);
      renewalRetryTimer = window.setTimeout(() => {
        renewalRetryTimer = undefined;
        void renew();
      }, boundedDelay);
    } else {
      problem.value = normalized;
      phase.value = normalized.kind === "forbidden" ? "forbidden" : "error";
    }
  }

  function setUnauthenticated(): void {
    void cancelRenewal();
    generation += 1;
    revision.value = 0;
    metadata.value = undefined;
    timing = undefined;
    window.sessionStorage.removeItem(authorizationStateKey);
    window.sessionStorage.removeItem(sessionRevisionKey);
    window.sessionStorage.removeItem(environmentDraftReauthKey);
    window.sessionStorage.removeItem(emailAttemptStorageKey);
    window.sessionStorage.removeItem(mailboxCredentialRecoveryKey);
    window.sessionStorage.removeItem(gitSourceRecoveryKey);
    clearWriteBackRecovery(window.sessionStorage);
    clearPublicationAttempts(window.sessionStorage);
    clearProviderLifecycleAttempts(window.sessionStorage);
    window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    pendingRuntimeSecretRevealState.value = undefined;
    pendingEmailConfirmation.value = undefined;
    loginFailed.value = false;
    phase.value = "unauthenticated";
  }

  async function probe(): Promise<void> {
    const current = ++generation;
    loginFailed.value = false;
    phase.value = "checking";
    problem.value = undefined;
    for (let attempt = 0; ; attempt += 1) {
      try {
        const started = performance.now();
        const [response, observed] = await Promise.all([
          unwrap(getBootstrapState({ signal: requestSignal() })),
          unwrap(getOwnerSession({ signal: requestSignal() })),
        ]);
        const serverRevision = ownerSessionRevision(response.etag);
        if (current !== generation) return;
        if (observed.data.sessionRevision !== serverRevision)
          throw new Error("Browser session revision does not match bootstrap");
        acceptMetadata(observed.data, performance.now() - started);
        revision.value = serverRevision;
        renewalBus.observeRevision(serverRevision);
        window.sessionStorage.setItem(
          sessionRevisionKey,
          String(serverRevision),
        );
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
    if (loginRedirectRequest) return await loginRedirectRequest;
    const current = ++generation;
    loginFailed.value = false;
    phase.value = "checking";
    problem.value = undefined;
    window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
    window.sessionStorage.removeItem(
      runtimeEnvironmentPolicyReauthCompletionStorageKey,
    );
    pendingRuntimeSecretRevealState.value = undefined;
    pendingEmailConfirmation.value = undefined;
    const pending = (async () => {
      try {
        await redirectAuthorization({});
      } catch (error) {
        const normalized = asProblem(error);
        if (current === generation) {
          problem.value = normalized;
          loginFailed.value = true;
          phase.value = normalized.kind === "forbidden" ? "forbidden" : "error";
        }
        throw normalized;
      }
    })();
    loginRedirectRequest = pending;
    try {
      await pending;
    } finally {
      if (loginRedirectRequest === pending) loginRedirectRequest = undefined;
    }
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
      await redirectAuthorization({
        freshAuthentication: true,
        purpose: {
          kind: "RUNTIME_SECRET_REVEAL",
          projectRef: intent.projectRef,
          secretRef: intent.secretRef,
        },
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
      await redirectAuthorization({ freshAuthentication: true });
    } catch (error) {
      window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
      throw error;
    }
  }

  async function beginEmailReconciliationReauth(
    input: Pick<
      EmailReconciliationIntent,
      | "receiptRef"
      | "receiptVersion"
      | "receiptDigest"
      | "connectionRef"
      | "invocationRef"
    >,
  ): Promise<void> {
    const intent = createEmailReconciliationIntent(input);
    pendingEmailConfirmation.value = undefined;
    await cancelRenewal();
    window.sessionStorage.setItem(
      oidcReauthIntentStorageKey,
      JSON.stringify(intent),
    );
    try {
      await redirectAuthorization({
        freshAuthentication: true,
        purpose: {
          kind: "EMAIL_EFFECT_RECONCILIATION",
          receiptRef: intent.receiptRef,
          receiptVersion: intent.receiptVersion,
          receiptDigest: intent.receiptDigest,
        },
      });
    } catch (error) {
      window.sessionStorage.removeItem(oidcReauthIntentStorageKey);
      startRenewal();
      throw error;
    }
  }
  function hasPendingEmailConfirmation(
    input: Pick<
      EmailReconciliationIntent,
      "receiptRef" | "receiptVersion" | "receiptDigest"
    >,
    now = Date.now(),
  ): boolean {
    const pending = pendingEmailConfirmation.value;
    return (
      !!pending &&
      pending.expiresAt > now &&
      pending.intent.receiptRef === input.receiptRef &&
      pending.intent.receiptVersion === input.receiptVersion &&
      pending.intent.receiptDigest === input.receiptDigest
    );
  }
  function consumePendingEmailConfirmation(
    input: Pick<
      EmailReconciliationIntent,
      "receiptRef" | "receiptVersion" | "receiptDigest"
    >,
  ): boolean {
    if (!hasPendingEmailConfirmation(input)) return false;
    pendingEmailConfirmation.value = undefined;
    return true;
  }
  function finishEmailConfirmation(): void {
    pendingEmailConfirmation.value = undefined;
    void refreshMetadata();
  }
  async function performLoginCompletion(): Promise<LoginCompletion> {
    const current = ++generation;
    pendingEmailConfirmation.value = undefined;
    phase.value = "checking";
    problem.value = undefined;
    try {
      const callbackURL = new URL(window.location.href);
      window.history.replaceState(
        window.history.state,
        "",
        window.location.pathname,
      );
      const input = authorizationCallback(callbackURL);
      if (input.state !== window.sessionStorage.getItem(authorizationStateKey))
        throw new Error(
          "Owner authorization state does not match this browser flow",
        );
      const intent: OidcIntent = consumePendingBrowserIntent(
        window.sessionStorage,
      );
      const started = performance.now();
      const response = await withOwnerSessionRetry(() =>
        unwrap(
          completeOwnerAuthorization({ body: input, signal: requestSignal() }),
        ),
      );
      if (current !== generation)
        throw new Error("OIDC callback was superseded");
      acceptMetadata(response.data, performance.now() - started);
      window.sessionStorage.removeItem(authorizationStateKey);
      const parsedRevision = response.data.sessionRevision;
      revision.value = parsedRevision;
      renewalBus.observeRevision(parsedRevision);
      window.sessionStorage.setItem(sessionRevisionKey, String(parsedRevision));
      phase.value = "authenticated";
      startRenewal();
      resetUnauthorizedNotification();
      if (intent.kind === "email-reconciliation") {
        pendingEmailConfirmation.value = {
          intent,
          expiresAt: Date.now() + 2 * 60_000,
        };
        return { kind: intent.kind, returnPath: intent.returnPath };
      }
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
    if (!canRenewSession()) return;
    if (renewalRequest) return await renewalRequest;
    if (metadataRequest) await metadataRequest;
    if (!canRenewSession()) return;
    if (!metadata.value || !timing) return await refreshMetadata();
    if (timing.deadline <= Date.now()) {
      setUnauthenticated();
      return;
    }
    if (metadata.value.renewalMode === "REAUTHENTICATION") {
      scheduleRenewal(timing.deadline - Date.now());
      return;
    }
    if (timing.renewAt > Date.now() && renewalFailures === 0) {
      scheduleRenewal(timing.renewAt - Date.now());
      return;
    }
    const lease = renewalCoordinator.acquire();
    if (!lease.acquired) {
      if (renewalRetryTimer !== undefined)
        window.clearTimeout(renewalRetryTimer);
      renewalRetryTimer = window.setTimeout(
        () => {
          renewalRetryTimer = undefined;
          void renew();
        },
        Math.min(
          lease.retryAfterMs + 25,
          Math.max(0, timing.deadline - Date.now()),
        ),
      );
      return;
    }
    const controller = new AbortController();
    renewalController = controller;
    const current = generation;
    const started = performance.now();
    const pending = (async () => {
      try {
        const response = await unwrap(
          renewOwnerSession({
            headers: { "X-CSRF-Token": csrfToken() },
            signal: AbortSignal.any([requestSignal(), controller.signal]),
          }),
        );
        if (controller.signal.aborted || !isCurrentSession(current)) return;
        acceptMetadata(response.data, performance.now() - started);
        const completedAt = Date.now();
        renewalFailures = 0;
        const nextRenewalAt = renewalCoordinator.complete(
          Math.max(1_000, timing.renewAt - completedAt),
        );
        renewalBus.publish({
          revision: revision.value,
          completedAt,
          nextRenewalAt,
        });
        scheduleRenewal(Math.max(0, nextRenewalAt - Date.now()));
      } catch (error) {
        if (controller.signal.aborted) return;
        if (isCurrentSession(current)) handleRenewalFailure(error);
      } finally {
        renewalCoordinator.release();
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
    if (loggingOut || !timing) return;
    scheduleRenewal(Math.max(0, timing.renewAt - Date.now()));
  }

  function scheduleRenewal(delayMs: number): void {
    if (renewalRetryTimer !== undefined) {
      window.clearTimeout(renewalRetryTimer);
      renewalRetryTimer = undefined;
    }
    if (renewalTimer !== undefined) window.clearTimeout(renewalTimer);
    renewalTimer = window.setTimeout(
      () => {
        renewalTimer = undefined;
        void renew();
      },
      Math.min(
        2_147_483_647,
        Math.max(0, delayMs),
        Math.max(0, (timing?.deadline ?? Date.now()) - Date.now()),
      ),
    );
  }

  function cancelRenewal(): Promise<void> | undefined {
    renewalFailures = 0;
    if (renewalTimer !== undefined) {
      window.clearTimeout(renewalTimer);
      renewalTimer = undefined;
    }
    renewalController?.abort();
    if (renewalRetryTimer !== undefined) {
      window.clearTimeout(renewalRetryTimer);
      renewalRetryTimer = undefined;
    }
    renewalCoordinator.release();
    return renewalRequest;
  }

  return {
    phase,
    problem,
    loginFailed,
    canLogout,
    connectionIdentity,
    refreshMetadata,
    probe,
    beginLogin,
    beginRuntimeSecretRevealReauth,
    beginRuntimeEnvironmentPolicyReauth,
    beginEmailReconciliationReauth,
    hasPendingEmailConfirmation,
    consumePendingEmailConfirmation,
    finishEmailConfirmation,
    completeLogin,
    pendingRuntimeSecretReveal,
    hasPendingRuntimeSecretReveal,
    consumePendingRuntimeSecretReveal,
    renew,
    logout,
    invalidate: setUnauthenticated,
  };
});
