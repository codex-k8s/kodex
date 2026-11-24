import { computed, onScopeDispose, ref, watch } from "vue";
import * as api from "./personal-email-api";
import {
  mailboxSetupAttempts,
  prepareMailboxSetup,
  rememberMailboxSetup,
  forgetMailboxSetup,
  mailboxSetupRejected,
  type MailboxSetupAttempt,
} from "./personal-email-recovery";
import type {
  EmailMailboxOwnership,
  EmailMailboxOwnershipInput,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

type EmailOwnerAPI = Pick<
  typeof api,
  | "readMailboxOwnership"
  | "writeMailboxSetup"
  | "recoverMailboxSetup"
  | "checkMailboxHealth"
>;
export function useEmailMailboxOwnership(
  connection: () => IntegrationConnection,
  onOwnership: (value: EmailMailboxOwnership | undefined) => void,
  onSaved: () => void,
  port: EmailOwnerAPI = api,
  storage: Pick<
    Storage,
    "getItem" | "setItem" | "removeItem"
  > = window.sessionStorage,
) {
  const ownership = ref<EmailMailboxOwnership>(),
    problem = ref<AppProblem>(),
    pending = ref<MailboxSetupAttempt>();
  const policy =
      ref<EmailMailboxOwnershipInput["policy"]>("INITIATOR_REQUIRED"),
    serviceSubjectRef = ref("");
  const busy = ref(false),
    loading = ref(false),
    requiresRefresh = ref(false),
    saved = ref(false);
  const owner = ownerInvalidationSignal();
  let generation = 0,
    disposed = false;
  let controller: AbortController | undefined;
  const matched = computed(() => {
    const current = ownership.value;
    const selected = connection();
    return (
      current !== undefined &&
      current.connectionRef === selected.ref &&
      current.connectionVersion === selected.version
    );
  });
  const canConfigure = computed(
    () =>
      matched.value &&
      connection().state === "DISABLED" &&
      !busy.value &&
      !loading.value &&
      !requiresRefresh.value &&
      (!pending.value || pending.value.operation === "CONFIGURE"),
  );
  const canRevoke = computed(
    () =>
      matched.value &&
      ownership.value?.state === "ACTIVE" &&
      !busy.value &&
      !loading.value &&
      !requiresRefresh.value &&
      (!pending.value || pending.value.operation === "REVOKE"),
  );
  const canTest = computed(
    () =>
      matched.value &&
      !!ownership.value?.ownerEligible &&
      ["NOT_CONNECTED", "CONNECTED", "DEGRADED"].includes(connection().state) &&
      !busy.value &&
      !loading.value &&
      !requiresRefresh.value &&
      !pending.value,
  );
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
  function suspend() {
    generation++;
    controller?.abort();
    ownership.value = undefined;
    onOwnership(undefined);
    busy.value = false;
    loading.value = false;
    requiresRefresh.value = true;
    saved.value = false;
  }
  function reset() {
    disposed = true;
    suspend();
    pending.value = undefined;
    serviceSubjectRef.value = "";
    problem.value = undefined;
  }
  owner.addEventListener("abort", reset, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", reset);
    reset();
  });
  async function load() {
    if (disposed || owner.aborted || busy.value) return;
    const operation = begin(),
      ref = connection().ref;
    ownership.value = undefined;
    onOwnership(undefined);
    loading.value = true;
    try {
      pending.value = mailboxSetupAttempts(storage).find(
        (value) => value.connectionRef === ref,
      );
      const value = await port.readMailboxOwnership(ref, operation.signal);
      if (!current(operation.epoch)) return;
      ownership.value = value;
      requiresRefresh.value = value.connectionVersion !== connection().version;
      if (!pending.value) {
        policy.value = value.policy;
        serviceSubjectRef.value = value.serviceSubjectRef ?? "";
      }
      if (!requiresRefresh.value) onOwnership(value);
    } catch (error) {
      if (current(operation.epoch)) {
        problem.value = asProblem(error);
        requiresRefresh.value = true;
      }
    } finally {
      if (current(operation.epoch)) loading.value = false;
    }
  }
  watch(
    () => [connection().ref, connection().version] as const,
    (value, prior) => {
      if (!prior || value[0] !== prior[0]) problem.value = undefined;
      suspend();
      void load();
    },
    { immediate: true, flush: "sync" },
  );
  async function change(operationKind: "CONFIGURE" | "REVOKE") {
    if (operationKind === "CONFIGURE" ? !canConfigure.value : !canRevoke.value)
      return;
    const operation = begin(),
      selected = connection(),
      prior = pending.value;
    busy.value = true;
    problem.value = undefined;
    saved.value = false;
    onOwnership(undefined);
    const body =
      operationKind === "CONFIGURE"
        ? {
            policy: policy.value,
            ...(policy.value === "PROJECT_SHARED"
              ? { serviceSubjectRef: serviceSubjectRef.value.trim() }
              : {}),
          }
        : undefined;
    try {
      const attempt = await prepareMailboxSetup(
        {
          operation: operationKind,
          connectionRef: selected.ref,
          connectionVersion: prior?.connectionVersion ?? selected.version,
        },
        body,
        prior,
      );
      if (!current(operation.epoch)) return;
      rememberMailboxSetup(attempt, storage);
      pending.value = attempt;
      await port.writeMailboxSetup(attempt, body, operation.signal);
      if (!current(operation.epoch)) return;
      forgetMailboxSetup(attempt, storage);
      pending.value = undefined;
      saved.value = true;
      requiresRefresh.value = true;
      onSaved();
    } catch (error) {
      if (current(operation.epoch)) {
        if (!prior && pending.value && mailboxSetupRejected(error)) {
          forgetMailboxSetup(pending.value, storage);
          pending.value = undefined;
        }
        problem.value = asProblem(error);
        requiresRefresh.value = true;
      }
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
    if (current(operation.epoch)) await load();
  }
  async function recover() {
    if (
      !pending.value ||
      disposed ||
      owner.aborted ||
      busy.value ||
      loading.value
    )
      return;
    const attempt = pending.value,
      operation = begin();
    busy.value = true;
    problem.value = undefined;
    onOwnership(undefined);
    try {
      await port.recoverMailboxSetup(attempt, operation.signal);
      if (!current(operation.epoch)) return;
      forgetMailboxSetup(attempt, storage);
      pending.value = undefined;
      saved.value = true;
      requiresRefresh.value = true;
      onSaved();
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
    if (current(operation.epoch)) await load();
  }
  async function test() {
    if (!canTest.value || !ownership.value) return;
    const operation = begin(),
      selected = connection(),
      view = ownership.value;
    busy.value = true;
    problem.value = undefined;
    onOwnership(undefined);
    try {
      await port.checkMailboxHealth(selected, view, operation.signal);
      if (!current(operation.epoch)) return;
      requiresRefresh.value = true;
      onSaved();
    } catch (error) {
      if (current(operation.epoch)) {
        problem.value = asProblem(error);
        requiresRefresh.value = true;
        onSaved();
      }
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
    if (current(operation.epoch)) await load();
  }
  function refresh() {
    problem.value = undefined;
    onSaved();
    void load();
  }
  return {
    ownership,
    problem,
    pending,
    policy,
    serviceSubjectRef,
    busy,
    loading,
    requiresRefresh,
    saved,
    canConfigure,
    canRevoke,
    canTest,
    load,
    refresh,
    change,
    recover,
    test,
    suspend,
  };
}
