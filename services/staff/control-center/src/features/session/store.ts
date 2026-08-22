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

const sessionRevisionKey = "mattercodex.session.revision";

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

  const canLogout = computed(
    () => phase.value === "authenticated" && revision.value > 0,
  );

  function setUnauthenticated(): void {
    generation += 1;
    revision.value = 0;
    window.sessionStorage.removeItem(sessionRevisionKey);
    phase.value = "unauthenticated";
  }

  async function probe(): Promise<void> {
    const current = ++generation;
    phase.value = "checking";
    problem.value = undefined;
    try {
      await unwrap(getBootstrapState({ signal: requestSignal() }));
      if (current !== generation) return;
      phase.value = "authenticated";
      resetUnauthorizedNotification();
    } catch (error) {
      if (current !== generation) return;
      const normalized = asProblem(error);
      problem.value = normalized;
      phase.value =
        normalized.kind === "unauthorized"
          ? "unauthenticated"
          : normalized.kind === "forbidden"
            ? "forbidden"
            : "error";
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
      const response = await unwrap(
        createOwnerSession({
          client: oneUseClient,
          headers: { "Idempotency-Key": idempotencyKey() },
          signal: requestSignal(),
        }),
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
  }

  return {
    phase,
    problem,
    canLogout,
    probe,
    beginLogin,
    completeLogin,
    logout,
    invalidate: setUnauthenticated,
  };
});
