import { computed, onScopeDispose, ref } from "vue";
import * as api from "./personal-connections-api";
import type {
  PersonalIntegrationConnection,
  PersonalIntegrationCredential,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export type PersonalConnectionAction = "SAVE" | "TEST" | "REVOKE";
export type PersonalConnectionsAPI = Pick<
  typeof api,
  | "loadPersonalConnections"
  | "createPersonalConnection"
  | "savePersonalConnection"
  | "testPersonalConnection"
  | "revokePersonalConnection"
>;

export function personalConnectionAllows(
  connection: PersonalIntegrationConnection,
  action: PersonalConnectionAction,
): boolean {
  const credential = connection.credential;
  if (action === "REVOKE")
    return (
      !!credential &&
      credential.credentialConfigured &&
      credential.state !== "REVOKED"
    );
  if (
    connection.policy !== "INITIATOR_REQUIRED" ||
    !connection.available ||
    credential?.state === "CHECKING"
  )
    return false;
  if (action === "SAVE") return true;
  return (
    connection.enabled &&
    !!credential?.credentialConfigured &&
    credential.state !== "REVOKED" &&
    credential.state !== "EXPIRED"
  );
}

// Локальный сценарий живёт ровно в текущем owner context. Ключ не попадает в
// Pinia, URL, storage либо recovery metadata; после ошибки нужен свежий readback.
export function usePersonalConnections(port: PersonalConnectionsAPI = api) {
  const items = ref<PersonalIntegrationConnection[]>([]);
  const query = ref("");
  const nextPageToken = ref("");
  const loading = ref(false);
  const busyRef = ref("");
  const problem = ref<AppProblem>();
  const requiresRefresh = ref(false);
  const credentialValue = ref("");
  const editingRef = ref("");
  const success = ref<PersonalConnectionAction>();
  const owner = ownerInvalidationSignal();
  let controller: AbortController | undefined;
  let generation = 0;
  let disposed = false;
  const inactive = ref(false);
  const cursors = new Set<string>();

  function closeEditor() {
    credentialValue.value = "";
    editingRef.value = "";
  }
  function suspend() {
    if (busyRef.value || loading.value) requiresRefresh.value = true;
    generation++;
    controller?.abort();
    closeEditor();
    busyRef.value = "";
    loading.value = false;
    success.value = undefined;
  }
  function reset() {
    disposed = true;
    inactive.value = true;
    generation++;
    controller?.abort();
    closeEditor();
    items.value = [];
    nextPageToken.value = "";
    problem.value = undefined;
    busyRef.value = "";
    loading.value = false;
    success.value = undefined;
  }
  owner.addEventListener("abort", reset, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", reset);
    reset();
  });

  function current(epoch: number) {
    return !disposed && !owner.aborted && epoch === generation;
  }
  function begin() {
    controller?.abort();
    controller = new AbortController();
    return {
      epoch: ++generation,
      signal: AbortSignal.any([owner, controller.signal]),
    };
  }
  async function load(more = false) {
    if (
      disposed ||
      owner.aborted ||
      busyRef.value ||
      (more && (loading.value || !nextPageToken.value))
    )
      return;
    closeEditor();
    const token = more ? nextPageToken.value : undefined;
    const operation = begin();
    loading.value = true;
    problem.value = undefined;
    success.value = undefined;
    try {
      const page = await port.loadPersonalConnections(
        query.value.trim(),
        token,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const entries = more ? [...items.value, ...page.items] : page.items;
      if (
        new Set(entries.map((item) => item.connectionRef)).size !==
          entries.length ||
        (page.nextPageToken &&
          (page.nextPageToken === token ||
            (more && cursors.has(page.nextPageToken))))
      )
        throw new Error("Personal connection cursor sequence is invalid");
      if (!more) cursors.clear();
      if (token) cursors.add(token);
      items.value = entries;
      nextPageToken.value = page.nextPageToken;
      requiresRefresh.value = false;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) loading.value = false;
    }
  }
  function edit(connection: PersonalIntegrationConnection) {
    if (
      busyRef.value ||
      loading.value ||
      requiresRefresh.value ||
      !personalConnectionAllows(connection, "SAVE")
    )
      return;
    closeEditor();
    success.value = undefined;
    editingRef.value = connection.connectionRef;
  }
  function apply(
    connection: PersonalIntegrationConnection,
    credential: PersonalIntegrationCredential,
  ) {
    items.value = items.value.map((item) =>
      item.connectionRef === connection.connectionRef
        ? { ...item, credential }
        : item,
    );
  }
  async function execute(
    connectionRef: string,
    action: PersonalConnectionAction,
  ) {
    const connection = items.value.find(
      (item) => item.connectionRef === connectionRef,
    );
    if (
      !connection ||
      disposed ||
      owner.aborted ||
      loading.value ||
      busyRef.value ||
      requiresRefresh.value ||
      !personalConnectionAllows(connection, action)
    )
      return;
    if (
      action === "SAVE" &&
      (editingRef.value !== connectionRef ||
        !credentialValue.value.trim() ||
        new TextEncoder().encode(credentialValue.value.trim()).length > 16384)
    )
      return;
    let value = action === "SAVE" ? credentialValue.value : "";
    closeEditor();
    success.value = undefined;
    const operation = begin();
    busyRef.value = connectionRef;
    problem.value = undefined;
    try {
      let credential = connection.credential;
      if (action === "SAVE") {
        if (!credential) {
          credential = await port.createPersonalConnection(
            connection,
            operation.signal,
          );
          if (!current(operation.epoch)) return;
          apply(connection, credential);
        }
        credential = await port.savePersonalConnection(
          connection,
          credential,
          value,
          operation.signal,
        );
      } else if (credential) {
        credential =
          action === "TEST"
            ? await port.testPersonalConnection(
                connection,
                credential,
                operation.signal,
              )
            : await port.revokePersonalConnection(
                connection,
                credential,
                operation.signal,
              );
      }
      if (current(operation.epoch) && credential) {
        apply(connection, credential);
        success.value = action;
      }
    } catch (error) {
      if (current(operation.epoch)) {
        problem.value = asProblem(error);
        requiresRefresh.value = true;
      }
    } finally {
      value = "";
      if (current(operation.epoch)) busyRef.value = "";
    }
  }
  return {
    items,
    query,
    nextPageToken,
    loading,
    busyRef,
    problem,
    requiresRefresh,
    credentialValue,
    editingRef,
    success,
    unavailable: computed(() => inactive.value),
    load,
    edit,
    closeEditor,
    execute,
    suspend,
  };
}
