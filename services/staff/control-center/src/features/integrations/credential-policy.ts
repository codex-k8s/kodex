import { computed, onScopeDispose, ref } from "vue";
import * as api from "./credential-policy-api";
import type {
  IntegrationConnection,
  IntegrationDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export function credentialPolicySupported(
  connection: IntegrationConnection,
  definition?: IntegrationDefinition,
): boolean {
  return (
    connection.nextActions.includes("EDIT") &&
    !!definition?.credentialSecretKey &&
    definition.adapterOwner === "integration-gateway" &&
    definition.executionRoute === "MANAGED_MCP" &&
    definition.adapterReadiness === "READY" &&
    connection.definitionKey === definition.key &&
    connection.definitionVersion === definition.definitionVersion &&
    connection.definitionDigest === definition.digest
  );
}

export type CredentialPolicyAPI = Pick<
  typeof api,
  "readCredentialPolicy" | "saveCredentialPolicy"
>;

// Modal получает свежий owner readback; закрытие/logout делает любой поздний ACK
// недействительным. После неоднозначной ошибки нужен явный read, не write retry.
export function useCredentialPolicy(
  definition: () => IntegrationDefinition | undefined,
  changed: () => void,
  port: CredentialPolicyAPI = api,
) {
  const opened = ref(false);
  const connection = ref<IntegrationConnection>();
  const selection = ref<api.IntegrationCredentialPolicy>("PROJECT_SHARED");
  const busy = ref(false);
  const problem = ref<AppProblem>();
  const requiresRefresh = ref(false);
  const saved = ref(false);
  const owner = ownerInvalidationSignal();
  let inactive = owner.aborted;
  let generation = 0;
  let controller: AbortController | undefined;
  let targetRef = "";
  const allowed = computed(
    () =>
      !inactive &&
      !busy.value &&
      !requiresRefresh.value &&
      !!connection.value &&
      credentialPolicySupported(connection.value, definition()) &&
      connection.value.state === "DISABLED" &&
      !connection.value.grants.some((grant) => grant.enabled),
  );

  function currentRequest(ticket: number, request: AbortController) {
    return (
      ticket === generation &&
      !inactive &&
      !owner.aborted &&
      !request.signal.aborted
    );
  }
  function close() {
    owner.removeEventListener("abort", invalidate);
    generation++;
    controller?.abort();
    opened.value = false;
    connection.value = undefined;
    targetRef = "";
    busy.value = false;
    problem.value = undefined;
    requiresRefresh.value = false;
    saved.value = false;
  }
  function invalidate() {
    inactive = true;
    close();
  }
  onScopeDispose(() => {
    owner.removeEventListener("abort", invalidate);
    invalidate();
  });

  async function refresh() {
    if (inactive || !opened.value || !targetRef || busy.value) return;
    controller?.abort();
    const request = new AbortController();
    controller = request;
    const ticket = ++generation;
    busy.value = true;
    requiresRefresh.value = true;
    problem.value = undefined;
    saved.value = false;
    try {
      const value = await port.readCredentialPolicy(targetRef, request.signal);
      if (!currentRequest(ticket, request)) return;
      if (value.ref !== targetRef)
        throw new Error("Integration credential policy target changed");
      connection.value = value;
      selection.value = value.credentialPolicy;
      requiresRefresh.value = false;
    } catch (error) {
      if (currentRequest(ticket, request)) problem.value = asProblem(error);
    } finally {
      if (ticket === generation) busy.value = false;
    }
  }
  async function open(value: IntegrationConnection) {
    if (
      inactive ||
      owner.aborted ||
      !credentialPolicySupported(value, definition())
    )
      return;
    close();
    owner.addEventListener("abort", invalidate, { once: true });
    targetRef = value.ref;
    opened.value = true;
    await refresh();
  }
  async function save() {
    const current = connection.value;
    if (
      !allowed.value ||
      !current ||
      selection.value === current.credentialPolicy
    )
      return;
    const policy = selection.value;
    const request = new AbortController();
    controller = request;
    const ticket = ++generation;
    busy.value = true;
    requiresRefresh.value = true;
    saved.value = false;
    problem.value = undefined;
    try {
      const value = await port.saveCredentialPolicy(
        current,
        policy,
        request.signal,
      );
      if (!currentRequest(ticket, request)) return;
      connection.value = value;
      selection.value = value.credentialPolicy;
      requiresRefresh.value = false;
      saved.value = true;
      changed();
    } catch (error) {
      if (currentRequest(ticket, request)) problem.value = asProblem(error);
    } finally {
      if (ticket === generation) busy.value = false;
    }
  }
  return {
    opened,
    connection,
    selection,
    busy,
    problem,
    requiresRefresh,
    saved,
    allowed,
    open,
    close,
    refresh,
    save,
  };
}
