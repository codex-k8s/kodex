import { defineStore } from "pinia";
import {
  InMemoryWebStorage,
  UserManager,
  WebStorageStateStore,
} from "oidc-client-ts";
import { computed, ref } from "vue";

import {
  admitOwnerSession,
  probeOwnerSession,
  revokeOwnerSession,
} from "@/shared/api/adapters/session";
import {
  asProblem,
  resetUnauthorizedNotification,
  type AppProblem,
} from "@/shared/api/problem";
import { runtimeConfig } from "@/shared/config/runtime";

export type SessionPhase =
  | "checking"
  | "authenticated"
  | "unauthenticated"
  | "forbidden"
  | "error";

const sessionEtagStorageKey = "mattercodex.owner-session-etag";

function storedSessionEtag(): string | null {
  return window.localStorage.getItem(sessionEtagStorageKey);
}

function persistSessionEtag(value: string | null): void {
  if (value) window.localStorage.setItem(sessionEtagStorageKey, value);
  else window.localStorage.removeItem(sessionEtagStorageKey);
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
  const problem = ref<AppProblem | null>(null);
  const sessionEtag = ref<string | null>(storedSessionEtag());
  const canRetryAdmission = ref(false);
  let requestGeneration = 0;
  let pendingAdmission:
    | { accessToken: string; manager: UserManager }
    | undefined;
  const canLogout = computed(
    () => phase.value === "authenticated" && sessionEtag.value !== null,
  );

  function invalidate(): void {
    requestGeneration += 1;
    pendingAdmission = undefined;
    canRetryAdmission.value = false;
    sessionEtag.value = null;
    persistSessionEtag(null);
    problem.value = null;
    phase.value = "unauthenticated";
  }

  async function probe(): Promise<void> {
    const generation = ++requestGeneration;
    phase.value = "checking";
    problem.value = null;
    try {
      await probeOwnerSession();
      if (generation !== requestGeneration) return;
      phase.value = "authenticated";
      resetUnauthorizedNotification();
    } catch (error) {
      if (generation !== requestGeneration) return;
      const normalized = asProblem(error);
      problem.value = normalized;
      if (normalized.kind === "unauthorized") {
        sessionEtag.value = null;
        persistSessionEtag(null);
      }
      phase.value =
        normalized.kind === "unauthorized"
          ? "unauthenticated"
          : normalized.kind === "forbidden"
            ? "forbidden"
            : "error";
    }
  }

  async function verify(): Promise<void> {
    if (phase.value !== "authenticated") return;
    const generation = ++requestGeneration;
    try {
      await probeOwnerSession();
      if (generation === requestGeneration) resetUnauthorizedNotification();
    } catch (error) {
      if (generation !== requestGeneration) return;
      const normalized = asProblem(error);
      if (normalized.kind === "unauthorized") invalidate();
    }
  }

  async function beginLogin(): Promise<void> {
    requestGeneration += 1;
    pendingAdmission = undefined;
    canRetryAdmission.value = false;
    problem.value = null;
    await oidcManager().signinRedirect();
  }

  async function completeLogin(): Promise<void> {
    const generation = ++requestGeneration;
    phase.value = "checking";
    problem.value = null;
    const manager = pendingAdmission?.manager ?? oidcManager();
    try {
      if (!pendingAdmission) {
        const user = await manager.signinRedirectCallback();
        if (!user.access_token) throw new Error("OIDC bearer is unavailable");
        pendingAdmission = { accessToken: user.access_token, manager };
      }
      const readback = await admitOwnerSession(pendingAdmission.accessToken);
      await manager.removeUser().catch(() => undefined);
      if (generation !== requestGeneration) return;
      pendingAdmission = undefined;
      canRetryAdmission.value = false;
      sessionEtag.value = readback.etag ?? null;
      persistSessionEtag(sessionEtag.value);
      phase.value = "authenticated";
      resetUnauthorizedNotification();
    } catch (error) {
      if (generation !== requestGeneration) return;
      const normalized = asProblem(error);
      problem.value = normalized;
      phase.value = "error";
      canRetryAdmission.value = Boolean(
        pendingAdmission && normalized.retryable,
      );
      if (!canRetryAdmission.value) {
        pendingAdmission = undefined;
        await manager.removeUser().catch(() => undefined);
      }
      throw error;
    }
  }

  async function logout(): Promise<void> {
    if (!sessionEtag.value) return;
    const generation = ++requestGeneration;
    phase.value = "checking";
    problem.value = null;
    try {
      await revokeOwnerSession(sessionEtag.value);
      if (generation !== requestGeneration) return;
      sessionEtag.value = null;
      persistSessionEtag(null);
      phase.value = "unauthenticated";
    } catch (error) {
      if (generation !== requestGeneration) return;
      const normalized = asProblem(error);
      problem.value = normalized;
      phase.value =
        normalized.kind === "unauthorized"
          ? "unauthenticated"
          : normalized.kind === "forbidden"
            ? "forbidden"
            : "error";
      if (normalized.kind === "unauthorized") {
        sessionEtag.value = null;
        persistSessionEtag(null);
      }
    }
  }

  return {
    phase,
    problem,
    canLogout,
    canRetryAdmission,
    probe,
    verify,
    beginLogin,
    completeLogin,
    logout,
    invalidate,
  };
});
