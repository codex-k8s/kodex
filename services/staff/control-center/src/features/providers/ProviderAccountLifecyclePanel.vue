<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  ProviderAccount,
  ProviderAccountBlocker,
  ProviderAccountBlockerKind,
  ProviderAccountBlockerPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { KnownMutationRejection } from "@/shared/api/mutation-rejection";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import ProviderLifecycleRecovery from "./ProviderLifecycleRecovery.vue";
import { loadProviderAccount } from "./api";
import {
  checkedProviderBlockerPage,
  loadProviderBlockers,
  providerBlockerKinds,
  providerBlockerRoute,
  startProviderLifecycle,
  type ProviderLifecycleResult,
} from "./lifecycle";
const props = defineProps<{ account: ProviderAccount }>();
const emit = defineEmits<{
  updated: [account: ProviderAccount];
  unavailable: [ref: string];
}>();
const { t } = useI18n();
const currentAccount = ref<ProviderAccount>();
const page = ref<ProviderAccountBlockerPage>();
const items = ref<ProviderAccountBlocker[]>([]);
const selected = ref<string[]>([]);
const outcomes = ref<ProviderLifecycleResult["outcomes"]>();
const kind = ref<ProviderAccountBlockerKind | "">("");
const query = ref("");
const loading = ref(false);
const busy = ref(false);
const recoveryPending = ref(false);
const problem = ref<AppProblem>();
const confirmation = ref<"DELETE" | "CANCEL_QUEUED">();
const blocked = computed(
  () => loading.value || busy.value || recoveryPending.value,
);
let generation = 0;
let controller = new AbortController();
let pollTimer: ReturnType<typeof setTimeout> | undefined;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let pollCount = 0;
const ownerSignal = ownerRequestSignal();
function stopPolling(): void {
  clearTimeout(pollTimer);
  pollTimer = undefined;
}
function applyAccount(value: ProviderAccount): void {
  currentAccount.value = value;
  emit("updated", value);
}
function clearProjection(): void {
  currentAccount.value = undefined;
  page.value = undefined;
  items.value = [];
  selected.value = [];
  outcomes.value = undefined;
  confirmation.value = undefined;
}
function closeOwner(): void {
  generation++;
  controller.abort();
  stopPolling();
  clearTimeout(searchTimer);
  clearProjection();
}
ownerSignal.addEventListener("abort", closeOwner, { once: true });
function scheduleObservation(): void {
  stopPolling();
  if (
    ownerSignal.aborted ||
    currentAccount.value?.state !== "DELETING" ||
    pollCount >= 150
  )
    return;
  pollTimer = setTimeout(() => void observe(), 4000);
}
async function observe(): Promise<void> {
  if (
    !currentAccount.value ||
    busy.value ||
    loading.value ||
    selected.value.length ||
    recoveryPending.value
  ) {
    scheduleObservation();
    return;
  }
  pollCount++;
  const current = generation;
  try {
    const next = await loadProviderAccount(
      currentAccount.value.ref,
      controller.signal,
    );
    if (current !== generation || controller.signal.aborted) return;
    if (next.ref !== props.account.ref)
      throw new Error("Provider observation scope changed");
    if (
      next.version !== currentAccount.value.version ||
      next.deletion?.version !== currentAccount.value.deletion?.version
    ) {
      applyAccount(next);
      await load(true);
    } else scheduleObservation();
  } catch (error) {
    if (current !== generation) return;
    clearProjection();
    problem.value = asProblem(error);
    emit("unavailable", props.account.ref);
  }
}
async function load(reset: boolean): Promise<void> {
  if (
    busy.value ||
    ownerSignal.aborted ||
    (!reset && !page.value?.nextPageToken)
  )
    return;
  const previous = reset ? undefined : page.value;
  const current = ++generation;
  controller.abort();
  controller = new AbortController();
  stopPolling();
  loading.value = true;
  problem.value = undefined;
  confirmation.value = undefined;
  if (reset) {
    items.value = [];
    selected.value = [];
    page.value = undefined;
  }
  try {
    const fresh = reset
      ? await loadProviderAccount(props.account.ref, controller.signal)
      : currentAccount.value;
    if (current !== generation) return;
    if (!fresh || fresh.ref !== props.account.ref)
      throw new Error("Provider blocker account scope changed");
    if (fresh.state === "DELETED") {
      applyAccount(fresh);
      page.value = undefined;
      items.value = [];
      selected.value = [];
      return;
    }
    const next = await loadProviderBlockers(
      fresh.ref,
      {
        ...(kind.value ? { kind: kind.value } : {}),
        ...(query.value.trim() ? { query: query.value.trim() } : {}),
        ...(previous?.nextPageToken
          ? { pageToken: previous.nextPageToken }
          : {}),
      },
      controller.signal,
    );
    if (current !== generation) return;
    checkedProviderBlockerPage(next, fresh.version, previous);
    if (kind.value && next.items.some((item) => item.kind !== kind.value))
      throw new Error("Provider blocker kind changed");
    const combined = [...items.value, ...next.items];
    if (
      combined.length > next.total ||
      new Set(combined.map((item) => `${item.kind}:${item.ref}`)).size !==
        combined.length
    )
      throw new Error("Provider blocker page overlaps");
    applyAccount(fresh);
    items.value = combined;
    page.value = next;
    scheduleObservation();
  } catch (error) {
    if (current !== generation) return;
    clearProjection();
    problem.value = asProblem(error);
    emit("unavailable", props.account.ref);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function search(): void {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    pollCount = 0;
    void load(true);
  }, 300);
}
async function submit(): Promise<void> {
  const current = currentAccount.value;
  const action = confirmation.value;
  if (!current || !page.value || !action || blocked.value) return;
  if (action === "DELETE" && !current.nextActions.includes("DELETE")) return;
  if (
    action === "CANCEL_QUEUED" &&
    (!selected.value.length ||
      selected.value.length > 64 ||
      selected.value.some(
        (ref) =>
          !items.value.some(
            (item) =>
              item.ref === ref && item.kind === "QUEUED_TURN" && item.canCancel,
          ),
      ))
  )
    return;
  busy.value = true;
  problem.value = undefined;
  outcomes.value = undefined;
  stopPolling();
  const currentGeneration = generation;
  try {
    const result = await startProviderLifecycle(
      current,
      action === "DELETE"
        ? { action }
        : {
            action,
            body: {
              selectedRunRefs: [...selected.value],
              blockersDigest: page.value.contextDigest,
            },
          },
      window.sessionStorage,
      controller.signal,
    );
    if (currentGeneration !== generation) return;
    applyAccount(result.account);
    outcomes.value = result.outcomes;
  } catch (error) {
    if (currentGeneration === generation) problem.value = asProblem(error);
  } finally {
    if (currentGeneration === generation) {
      busy.value = false;
      confirmation.value = undefined;
      if (!problem.value || problem.value instanceof KnownMutationRejection)
        await load(true);
    }
  }
}
function recovered(result: ProviderLifecycleResult): void {
  applyAccount(result.account);
  outcomes.value = result.outcomes;
  void load(true);
}
watch(
  () => props.account.ref,
  () => {
    pollCount = 0;
    clearProjection();
    void load(true);
  },
  { immediate: true },
);
watch(kind, () => {
  pollCount = 0;
  void load(true);
});
onBeforeUnmount(() => {
  closeOwner();
  ownerSignal.removeEventListener("abort", closeOwner);
});
</script>
<template>
  <section class="provider-lifecycle">
    <ProblemNotice v-if="problem" :problem="problem" @retry="load(true)" />
    <button
      type="button"
      class="button"
      :disabled="loading || busy"
      @click="
        pollCount = 0;
        load(true);
      "
    >
      {{ t("providerLifecycle.refresh") }}
    </button>
    <template v-if="currentAccount">
      <strong>{{ currentAccount.name }}</strong>
      <section
        v-if="currentAccount.deletion"
        class="provider-lifecycle__state"
        role="status"
      >
        <strong>{{
          t(`providerLifecycle.states.${currentAccount.deletion.state}`)
        }}</strong>
        <p>
          {{
            t(`providerLifecycle.reasons.${currentAccount.deletion.safeReason}`)
          }}
        </p>
        <p>
          {{
            t("providerLifecycle.pendingCleanup", {
              count: currentAccount.deletion.pendingCleanup,
            })
          }}
        </p>
        <dl :aria-label="t('providerLifecycle.counts')">
          <template
            v-for="item in currentAccount.deletion.blockers"
            :key="item.kind"
            ><dt>{{ t(`providerLifecycle.kinds.${item.kind}`) }}</dt>
            <dd>{{ item.total }}</dd></template
          >
        </dl>
      </section>
      <ProviderLifecycleRecovery
        :account="currentAccount"
        :problem="problem"
        @pending="recoveryPending = $event"
        @recovered="recovered"
      />
      <div class="provider-lifecycle__filters">
        <label
          >{{ t("providerLifecycle.kind")
          }}<select v-model="kind" :disabled="busy">
            <option value="">{{ t("providerLifecycle.all") }}</option>
            <option
              v-for="value in providerBlockerKinds"
              :key="value"
              :value="value"
            >
              {{ t(`providerLifecycle.kinds.${value}`) }}
            </option>
          </select></label
        >
        <label
          >{{ t("providerLifecycle.search")
          }}<input
            v-model="query"
            type="search"
            maxlength="128"
            :disabled="busy"
            @input="search"
        /></label>
      </div>
      <template v-if="page">
        <p>{{ t("providerLifecycle.visible", { count: page.total }) }}</p>
        <p v-if="page.hiddenCount" role="status">
          {{ t("providerLifecycle.hidden", { count: page.hiddenCount }) }}
        </p>
        <p v-if="!items.length">{{ t("providerLifecycle.empty") }}</p>
        <ul class="provider-lifecycle__items">
          <li v-for="item in items" :key="`${item.kind}:${item.ref}`">
            <input
              v-if="item.kind === 'QUEUED_TURN'"
              v-model="selected"
              :value="item.ref"
              type="checkbox"
              :aria-label="`${t('providerLifecycle.select')}: ${item.name}`"
              :disabled="
                blocked ||
                !item.canCancel ||
                (selected.length >= 64 && !selected.includes(item.ref))
              "
            />
            <div>
              <RouterLink
                v-if="providerBlockerRoute(item)"
                :to="providerBlockerRoute(item) ?? ''"
                >{{ item.name }}</RouterLink
              ><strong v-else>{{ item.name }}</strong
              ><small>{{ t(`providerLifecycle.kinds.${item.kind}`) }}</small>
              <p v-if="item.kind === 'WARM_RUNTIME'">
                {{ t("providerLifecycle.warmHelp") }}
              </p>
            </div>
          </li>
        </ul>
        <button
          v-if="page.nextPageToken"
          type="button"
          class="button"
          :disabled="blocked"
          @click="load(false)"
        >
          {{ t("providerLifecycle.more") }}
        </button>
        <p>{{ t("providerLifecycle.selected", { count: selected.length }) }}</p>
        <button
          v-if="
            items.some((item) => item.kind === 'QUEUED_TURN' && item.canCancel)
          "
          type="button"
          class="button"
          :disabled="blocked || !selected.length"
          @click="confirmation = 'CANCEL_QUEUED'"
        >
          {{ t("providerLifecycle.cancelQueued", { count: selected.length }) }}
        </button>
        <button
          v-if="currentAccount.nextActions.includes('DELETE')"
          type="button"
          class="button button--danger"
          :disabled="blocked"
          @click="confirmation = 'DELETE'"
        >
          {{ t("providerLifecycle.delete") }}
        </button>
      </template>
      <section v-if="confirmation" class="provider-lifecycle__confirmation">
        <p>
          {{
            t(
              confirmation === "DELETE"
                ? "providerLifecycle.deleteConfirm"
                : "providerLifecycle.cancelConfirm",
            )
          }}
        </p>
        <button
          type="button"
          class="button button--danger"
          :disabled="blocked"
          @click="submit"
        >
          {{ t("providerLifecycle.confirm") }}</button
        ><button
          type="button"
          class="button"
          :disabled="busy"
          @click="confirmation = undefined"
        >
          {{ t("common.cancel") }}
        </button>
      </section>
      <section v-if="outcomes" :aria-label="t('providerLifecycle.result')">
        <p v-for="(outcome, index) in outcomes" :key="index">
          {{ t("providerLifecycle.resultItem", { index: index + 1 }) }}:
          {{ t(`providerLifecycle.outcomes.${outcome.outcome}`) }}
        </p>
      </section>
    </template>
  </section>
</template>
<style scoped>
.provider-lifecycle {
  display: grid;
  gap: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.provider-lifecycle p {
  margin: 0;
}
.provider-lifecycle .button {
  white-space: normal;
}
.provider-lifecycle__filters {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.provider-lifecycle__filters label,
.provider-lifecycle__items li > div {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.provider-lifecycle__filters input,
.provider-lifecycle__filters select {
  width: 100%;
  min-width: 0;
}
.provider-lifecycle__items {
  list-style: none;
  padding: 0;
  display: grid;
  gap: 12px;
}
.provider-lifecycle__items li {
  display: flex;
  gap: 10px;
  align-items: start;
}
.provider-lifecycle__state,
.provider-lifecycle__confirmation {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.provider-lifecycle__state dl {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 6px;
  margin: 0;
}
@media (max-width: 600px) {
  .provider-lifecycle__filters {
    grid-template-columns: 1fr;
  }
}
</style>
