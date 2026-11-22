import { computed, onScopeDispose, ref } from "vue";
import * as api from "./personal-email-api";
import {
  mailboxSetupAttempts,
  prepareMailboxSetup,
  rememberMailboxSetup,
  forgetMailboxSetup,
  mailboxSetupRejected,
  type MailboxSetupAttempt,
} from "./personal-email-recovery";
import type { MyEmailMailbox } from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

type PersonalEmailAPI = Pick<
  typeof api,
  "loadMyMailboxes" | "writeMailboxSetup" | "recoverMailboxSetup"
>;
export function usePersonalEmailConnections(
  port: PersonalEmailAPI = api,
  storage: Pick<
    Storage,
    "getItem" | "setItem" | "removeItem"
  > = window.sessionStorage,
) {
  const items = ref<MyEmailMailbox[]>([]),
    query = ref(""),
    nextPageToken = ref("");
  const name = ref(""),
    sender = ref(""),
    busy = ref(false),
    loading = ref(false),
    requiresRefresh = ref(false);
  const problem = ref<AppProblem>(),
    pending = ref<MailboxSetupAttempt>(),
    createdRef = ref("");
  const nameValid = computed(() => api.validPersonalEmailName(name.value));
  const owner = ownerInvalidationSignal();
  let generation = 0,
    disposed = false;
  let controller: AbortController | undefined;
  const cursors = new Set<string>();
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
    busy.value = false;
    loading.value = false;
    requiresRefresh.value = true;
  }
  function reset() {
    disposed = true;
    suspend();
    items.value = [];
    name.value = "";
    sender.value = "";
    pending.value = undefined;
    createdRef.value = "";
    problem.value = undefined;
  }
  owner.addEventListener("abort", reset, { once: true });
  onScopeDispose(() => {
    owner.removeEventListener("abort", reset);
    reset();
  });
  function restore() {
    try {
      pending.value = mailboxSetupAttempts(storage).find(
        (value) => value.operation === "CREATE",
      );
    } catch (error) {
      problem.value = asProblem(error);
      requiresRefresh.value = true;
    }
  }
  async function load(more = false) {
    if (
      disposed ||
      owner.aborted ||
      busy.value ||
      (more && (loading.value || !nextPageToken.value))
    )
      return;
    const token = more ? nextPageToken.value : undefined,
      operation = begin();
    loading.value = true;
    problem.value = undefined;
    try {
      const page = await port.loadMyMailboxes(
        query.value.trim(),
        token,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      const entries = more ? [...items.value, ...page.items] : page.items;
      if (
        new Set(entries.map((item) => item.ownership.connectionRef)).size !==
          entries.length ||
        (page.nextPageToken &&
          (page.nextPageToken === token ||
            (more && cursors.has(page.nextPageToken))))
      )
        throw new Error("Mailbox catalog cursor sequence is invalid");
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
  async function create() {
    if (
      disposed ||
      owner.aborted ||
      busy.value ||
      loading.value ||
      requiresRefresh.value ||
      !nameValid.value ||
      !sender.value.trim()
    )
      return;
    const prior = pending.value;
    const operation = begin(),
      body = { name: name.value.trim(), sender: sender.value.trim() };
    busy.value = true;
    problem.value = undefined;
    createdRef.value = "";
    try {
      const attempt = await prepareMailboxSetup(
        { operation: "CREATE", connectionRef: "", connectionVersion: 0 },
        body,
        pending.value,
      );
      if (!current(operation.epoch)) return;
      rememberMailboxSetup(attempt, storage);
      pending.value = attempt;
      const receipt = await port.writeMailboxSetup(
        attempt,
        body,
        operation.signal,
      );
      if (!current(operation.epoch)) return;
      if (!receipt.mailbox)
        throw new Error("Mailbox create receipt is unavailable");
      forgetMailboxSetup(attempt, storage);
      pending.value = undefined;
      createdRef.value = receipt.mailbox.ownership.connectionRef;
      name.value = "";
      sender.value = "";
      requiresRefresh.value = true;
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
    if (current(operation.epoch) && createdRef.value) await load();
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
    try {
      const receipt = await port.recoverMailboxSetup(attempt, operation.signal);
      if (!current(operation.epoch)) return;
      if (!receipt.mailbox)
        throw new Error("Mailbox create receipt is unavailable");
      forgetMailboxSetup(attempt, storage);
      pending.value = undefined;
      createdRef.value = receipt.mailbox.ownership.connectionRef;
      requiresRefresh.value = true;
    } catch (error) {
      if (current(operation.epoch)) problem.value = asProblem(error);
    } finally {
      if (current(operation.epoch)) busy.value = false;
    }
    if (current(operation.epoch) && !pending.value) await load();
  }
  restore();
  return {
    items,
    query,
    nextPageToken,
    name,
    nameValid,
    sender,
    busy,
    loading,
    requiresRefresh,
    problem,
    pending,
    createdRef,
    load,
    create,
    recover,
    suspend,
  };
}
