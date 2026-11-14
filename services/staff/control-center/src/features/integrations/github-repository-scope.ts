import { computed, onScopeDispose, ref, watch, type Ref } from "vue";
import {
  getIntegrationConnection,
  getPersonalIntegrationCredential,
  testPersonalIntegrationCredential,
  updateIntegrationConnection,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  GitHubDiscoveredRepository,
  GitHubRepositoryDiscoveryPage,
  IntegrationConnection,
  PersonalIntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap, asProblem, type AppProblem } from "@/shared/api/problem";
import { isRecord } from "@/shared/lib/exact-object";

const namePattern = /^[A-Za-z0-9_.-]{1,100}\/[A-Za-z0-9_.-]{1,100}$/;

// Поля provider response не копируются в UI автоматически; повторные ID и неверная страница закрыто отклоняются.
export function githubDiscoveryView(
  value: unknown,
): GitHubRepositoryDiscoveryPage {
  if (
    !isRecord(value) ||
    typeof value.identityId !== "number" ||
    !Number.isSafeInteger(value.identityId) ||
    value.identityId < 1 ||
    typeof value.login !== "string" ||
    !namePattern.test(`${value.login}/identity`) ||
    typeof value.page !== "number" ||
    !Number.isSafeInteger(value.page) ||
    value.page < 1 ||
    value.page > 10000 ||
    typeof value.nextPage !== "number" ||
    !Number.isSafeInteger(value.nextPage) ||
    (value.nextPage !== 0 &&
      (value.nextPage !== value.page + 1 || value.nextPage > 10000)) ||
    !Array.isArray(value.items) ||
    value.items.length > 100
  )
    throw new Error("GitHub discovery page is invalid");
  const ids = new Set<number>();
  const names = new Set<string>();
  const items = value.items.map((repo: unknown): GitHubDiscoveredRepository => {
    if (
      !isRecord(repo) ||
      typeof repo.id !== "number" ||
      !Number.isSafeInteger(repo.id) ||
      repo.id < 1 ||
      typeof repo.fullName !== "string" ||
      !namePattern.test(repo.fullName) ||
      repo.fullName.split("/").some((part) => part === "." || part === "..") ||
      (repo.ownerKind !== "User" && repo.ownerKind !== "Organization") ||
      typeof repo.private !== "boolean" ||
      typeof repo.archived !== "boolean" ||
      repo.canRead !== true ||
      typeof repo.canPush !== "boolean" ||
      ids.has(repo.id) ||
      names.has(repo.fullName)
    )
      throw new Error("GitHub discovery repository is invalid");
    ids.add(repo.id);
    names.add(repo.fullName);
    return {
      id: repo.id,
      fullName: repo.fullName,
      ownerKind: repo.ownerKind,
      private: repo.private,
      archived: repo.archived,
      canRead: repo.canRead,
      canPush: repo.canPush,
    };
  });
  return {
    identityId: value.identityId,
    login: value.login,
    page: value.page,
    nextPage: value.nextPage,
    items,
  };
}

// Сценарий живёт в одном owner context; сохранение не повторяется после потери ответа, polling ограничен чтением своей привязки.
export function useGitHubRepositoryScope(
  connection: Ref<PersonalIntegrationConnection>,
  changed: () => void,
) {
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const page = ref<GitHubRepositoryDiscoveryPage>();
  const selected = ref<GitHubDiscoveredRepository[]>([]);
  const kind = ref<"ALL_ACCESSIBLE" | "SELECTED">("ALL_ACCESSIBLE");
  const current = ref<IntegrationConnection>();
  const saved = ref(false);
  const owner = ownerInvalidationSignal();
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;
  function invalidate() {
    generation++;
    controller?.abort();
    controller = undefined;
    busy.value = false;
    page.value = undefined;
    selected.value = [];
    current.value = undefined;
    problem.value = undefined;
    saved.value = false;
  }
  function close() {
    disposed = true;
    invalidate();
  }
  owner.addEventListener("abort", close, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", close);
    close();
  });
  watch(
    () =>
      [
        connection.value.connectionRef,
        connection.value.connectionVersion,
        connection.value.credential?.state,
      ] as const,
    (value, previous) => {
      if (previous && (value[0] !== previous[0] || value[1] !== previous[1]))
        invalidate();
      if (!busy.value)
        page.value = connection.value.credential?.githubDiscovery
          ? githubDiscoveryView(connection.value.credential.githubDiscovery)
          : undefined;
    },
    { immediate: true },
  );
  function start() {
    controller?.abort();
    controller = new AbortController();
    const epoch = ++generation;
    const signal = AbortSignal.any([owner, controller.signal]);
    return { epoch, signal };
  }
  function active(epoch: number) {
    return !disposed && !owner.aborted && epoch === generation;
  }
  async function discover(cursor = 1) {
    const target = connection.value;
    const credential = target.credential;
    if (
      busy.value ||
      disposed ||
      !credential ||
      credential.state === "CHECKING"
    )
      return;
    const operation = start();
    busy.value = true;
    problem.value = undefined;
    saved.value = false;
    try {
      const receipt = (
        await mutate(
          (headers) =>
            testPersonalIntegrationCredential({
              path: { connectionRef: target.connectionRef },
              body: {
                expectedConnectionVersion: target.connectionVersion,
                githubDiscoveryPage: cursor,
              },
              headers: { ...headers, "If-Match": etag(credential.version) },
              signal: requestSignal(operation.signal),
            }),
          credential.version,
        )
      ).data;
      if (
        receipt.state !== "CHECKING" ||
        receipt.version !== credential.version + 1
      )
        throw new Error("GitHub discovery receipt is invalid");
      for (let attempt = 0; attempt < 35; attempt++) {
        await new Promise<void>((resolve, reject) => {
          const done = () => {
            operation.signal.removeEventListener("abort", aborted);
            resolve();
          };
          const timer = setTimeout(done, 1000);
          const aborted = () => {
            clearTimeout(timer);
            reject(new Error("GitHub discovery aborted"));
          };
          operation.signal.addEventListener("abort", aborted, { once: true });
          if (operation.signal.aborted) aborted();
        });
        const observed = (
          await unwrap(
            getPersonalIntegrationCredential({
              path: { connectionRef: target.connectionRef },
              signal: requestSignal(operation.signal),
            }),
          )
        ).data;
        if (!active(operation.epoch)) return;
        if (
          observed.connectionRef !== target.connectionRef ||
          observed.connectionVersion !== target.connectionVersion
        )
          throw new Error("GitHub discovery context changed");
        if (observed.state === "CHECKING") continue;
        if (
          observed.state !== "READY" ||
          !observed.githubDiscovery ||
          observed.bindingRef !== receipt.bindingRef ||
          observed.version !== receipt.version + 1
        )
          throw new Error("GitHub discovery is unavailable");
        const result = githubDiscoveryView(observed.githubDiscovery);
        if (result.page !== cursor)
          throw new Error("GitHub discovery cursor changed");
        page.value = result;
        changed();
        return;
      }
      throw new Error("GitHub discovery is still pending");
    } catch (error) {
      if (active(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (active(operation.epoch)) busy.value = false;
    }
  }
  function toggle(repo: GitHubDiscoveredRepository) {
    const exists = selected.value.some((item) => item.id === repo.id);
    if (exists)
      selected.value = selected.value.filter((item) => item.id !== repo.id);
    else if (selected.value.length < 100)
      selected.value = [...selected.value, repo];
  }
  async function save() {
    if (
      busy.value ||
      disposed ||
      (kind.value === "SELECTED" && !selected.value.length)
    )
      return;
    const target = connection.value;
    const operation = start();
    busy.value = true;
    problem.value = undefined;
    saved.value = false;
    try {
      const existing = (
        await unwrap(
          getIntegrationConnection({
            path: { connectionRef: target.connectionRef },
            signal: requestSignal(operation.signal),
          }),
        )
      ).data;
      if (
        existing.ref !== target.connectionRef ||
        existing.version !== target.connectionVersion ||
        !existing.nextActions.includes("UPDATE")
      )
        throw new Error("GitHub repository scope management is unavailable");
      current.value = existing;
      const publicConfiguration: Record<string, string> = {
        ...existing.publicConfiguration,
        scope_kind: kind.value,
      };
      delete publicConfiguration.repositories;
      if (kind.value === "SELECTED")
        publicConfiguration.repositories = JSON.stringify(
          [...selected.value]
            .sort((a, b) => a.id - b.id)
            .map((repo) => ({ id: repo.id, full_name: repo.fullName })),
        );
      const updated = (
        await mutate(
          (headers) =>
            updateIntegrationConnection({
              path: { connectionRef: existing.ref },
              body: { name: existing.name, publicConfiguration },
              headers: { ...headers, "If-Match": etag(existing.version) },
              signal: requestSignal(operation.signal),
            }),
          existing.version,
        )
      ).data;
      if (
        updated.ref !== existing.ref ||
        updated.version !== existing.version + 1
      )
        throw new Error("GitHub scope receipt is invalid");
      if (active(operation.epoch)) {
        page.value = undefined;
        selected.value = [];
        saved.value = true;
        changed();
      }
    } catch (error) {
      if (active(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (active(operation.epoch)) busy.value = false;
    }
  }
  return {
    busy,
    problem,
    page,
    selected,
    kind,
    saved,
    discover,
    toggle,
    save,
    canDiscover: computed(
      () =>
        !busy.value &&
        connection.value.enabled &&
        connection.value.credential?.credentialConfigured &&
        connection.value.credential.state !== "REVOKED" &&
        connection.value.credential.state !== "CHECKING",
    ),
  };
}
