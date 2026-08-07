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
import { asProblem, type AppProblem } from "@/shared/api/problem";
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
  let requestGeneration = 0;
  const canLogout = computed(
    () => phase.value === "authenticated" && sessionEtag.value !== null,
  );

  async function probe(): Promise<void> {
    const generation = ++requestGeneration;
    phase.value = "checking";
    problem.value = null;
    try {
      await probeOwnerSession();
      if (generation !== requestGeneration) return;
      phase.value = "authenticated";
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

  async function beginLogin(): Promise<void> {
    requestGeneration += 1;
    problem.value = null;
    await oidcManager().signinRedirect();
  }

  async function completeLogin(): Promise<void> {
    const generation = ++requestGeneration;
    phase.value = "checking";
    problem.value = null;
    const manager = oidcManager();
    try {
      const user = await manager.signinRedirectCallback();
      const readback = await admitOwnerSession(user.access_token);
      await manager.removeUser();
      if (generation !== requestGeneration) return;
      sessionEtag.value = readback.etag ?? null;
      persistSessionEtag(sessionEtag.value);
      phase.value = "authenticated";
    } catch (error) {
      if (generation !== requestGeneration) return;
      problem.value = asProblem(error);
      phase.value = "error";
      await manager.removeUser().catch(() => undefined);
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
    probe,
    beginLogin,
    completeLogin,
    logout,
  };
});
