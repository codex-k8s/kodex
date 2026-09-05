<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { idempotencyKey } from "@/shared/api/mutation";
import type { RuntimeSecret } from "./model";
import { readRuntimeSecret } from "./api";
import type { AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  readSecretDraft,
  safeDraftProblem,
  type RuntimeSecretDraft,
} from "./draft-api";
import {
  prepareDraftImpact,
  readDraftImpact,
  restoreDraftImpact,
  publishSecretDraft,
  checkedPlan,
  type RuntimeSecretDraftImpactPlan,
  type RuntimeSecretDraftImpactPage,
} from "./draft-impact";

const props = defineProps<{
  draft: RuntimeSecretDraft;
  initialPlanRef?: string;
}>();
const emit = defineEmits<{
  published: [draft: RuntimeSecretDraft, secret: RuntimeSecret];
  working: [busy: boolean];
  uncertain: [value: boolean];
  prepared: [planRef: string];
}>();
const { t } = useI18n();
const plan = shallowRef<RuntimeSecretDraftImpactPlan>();
const page = shallowRef<RuntimeSecretDraftImpactPage>();
const busy = ref(false);
const problem = shallowRef<AppProblem>();
const query = ref("");
const selected = ref<string[]>([]);
const selectionReady = ref(false);
const pending = ref(false);
let prepareAttempt: { draft: RuntimeSecretDraft; key: string } | undefined;
let publishAttempt:
  | {
      draft: RuntimeSecretDraft;
      plan: RuntimeSecretDraftImpactPlan;
      selected: string[];
      key: string;
    }
  | undefined;
let controller: AbortController | undefined;
let disposed = false;
const isActive = () => !disposed;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
const cursors = new Set<string>();
const canPublish = computed(
  () =>
    !busy.value &&
    (pending.value ||
      (plan.value?.state === "PREPARED" &&
        selectionReady.value &&
        Boolean(page.value) &&
        !problem.value)),
);
function working(value: boolean): void {
  busy.value = value;
  emit("working", value);
}

async function selectAvailable(): Promise<void> {
  if (!plan.value || !page.value || plan.value.state !== "PREPARED") return;
  const signal = AbortSignal.any([
    controller?.signal ?? new AbortController().signal,
    AbortSignal.timeout(15_000),
  ]);
  let current = query.value.trim()
    ? await readDraftImpact(plan.value, signal)
    : page.value;
  const expectedTotal = current.total;
  const refs = new Set<string>();
  const seen = new Set<string>();
  for (let index = 0; index < 26; index += 1) {
    if (current.plan.state !== "PREPARED" || current.total !== expectedTotal)
      throw new Error("Secret draft default selection snapshot changed");
    for (const item of current.items) {
      if (refs.has(item.ref))
        throw new Error("Secret draft default selection repeated");
      refs.add(item.ref);
    }
    if (refs.size > 1000 || refs.size > expectedTotal)
      throw new Error("Secret draft default selection exceeds limit");
    if (!current.nextPageToken) {
      if (refs.size !== expectedTotal)
        throw new Error("Secret draft default selection is incomplete");
      if (!disposed) {
        selected.value = [...refs];
        selectionReady.value = true;
      }
      return;
    }
    if (seen.has(current.nextPageToken))
      throw new Error("Secret draft default selection cursor repeated");
    seen.add(current.nextPageToken);
    current = await readDraftImpact(
      plan.value,
      signal,
      "",
      current.nextPageToken,
    );
  }
  throw new Error("Secret draft default selection page limit exceeded");
}

async function load(more = false): Promise<void> {
  if (!plan.value || (more && !page.value?.nextPageToken)) return;
  const before = page.value;
  controller?.abort();
  controller = new AbortController();
  const result = await readDraftImpact(
    plan.value,
    controller.signal,
    query.value,
    more ? before?.nextPageToken : undefined,
  );
  if (disposed) return;
  if (more && before) {
    if (
      result.plan.state !== before.plan.state ||
      result.total !== before.total ||
      (result.nextPageToken && cursors.has(result.nextPageToken))
    )
      throw new Error("Secret draft impact cursor snapshot changed");
    const items = [...before.items, ...result.items];
    if (
      new Set(items.map((item) => item.ref)).size !== items.length ||
      items.length > result.total
    )
      throw new Error("Secret draft impact items repeated");
    result.items = items;
    cursors.add(before.nextPageToken);
  } else cursors.clear();
  page.value = result;
  plan.value = result.plan;
}

async function prepare(): Promise<void> {
  if (busy.value || publishAttempt) return;
  working(true);
  problem.value = undefined;
  try {
    if (!prepareAttempt) {
      controller?.abort();
      controller = new AbortController();
      const current = await readSecretDraft(
        props.draft.projectRef,
        props.draft.ref,
        controller.signal,
      );
      if (disposed) return;
      prepareAttempt = { draft: current, key: idempotencyKey() };
    }
    const result = await prepareDraftImpact(
      prepareAttempt.draft,
      prepareAttempt.key,
    );
    if (disposed) return;
    prepareAttempt = undefined;
    plan.value = result;
    selected.value = [];
    selectionReady.value = false;
    page.value = undefined;
    await load();
    await selectAvailable();
  } catch (error) {
    if (!disposed) problem.value = safeDraftProblem(error);
  } finally {
    if (!disposed) {
      working(false);
      if (plan.value) emit("prepared", plan.value.ref);
    }
  }
}

async function refresh(more = false): Promise<void> {
  if (busy.value || !plan.value) return;
  working(true);
  problem.value = undefined;
  try {
    await load(more);
    if (!selectionReady.value && plan.value.state === "PREPARED")
      await selectAvailable();
    if (publishAttempt && plan.value.state === "APPLIED") {
      controller?.abort();
      controller = new AbortController();
      const current = await readSecretDraft(
        props.draft.projectRef,
        props.draft.ref,
        controller.signal,
      );
      if (disposed) return;
      if (current.state !== "PUBLISHED")
        throw new Error("Secret draft publication recovery is not terminal");
      const secret = await readRuntimeSecret(
        current.secretRef,
        current.projectRef,
        controller.signal,
      );
      if (isActive()) {
        publishAttempt = undefined;
        pending.value = false;
        emit("uncertain", false);
        emit("published", current, secret);
      }
    } else if (["EXPIRED", "CANCELLED"].includes(plan.value.state)) {
      publishAttempt = undefined;
      pending.value = false;
      emit("uncertain", false);
    }
  } catch (error) {
    if (!disposed) problem.value = safeDraftProblem(error);
  } finally {
    if (!disposed) working(false);
  }
}

async function publish(replace = true): Promise<void> {
  if (!canPublish.value || !plan.value) return;
  const retrying = Boolean(publishAttempt);
  working(true);
  problem.value = undefined;
  try {
    if (!publishAttempt) {
      controller?.abort();
      controller = new AbortController();
      const current = await readSecretDraft(
        props.draft.projectRef,
        props.draft.ref,
        controller.signal,
      );
      if (disposed) return;
      if (
        plan.value.draftVersion !== current.version ||
        plan.value.secretVersion !== current.secretVersion
      ) {
        plan.value = undefined;
        page.value = undefined;
        selected.value = [];
        throw new Error("Secret draft impact pins changed");
      }
      checkedPlan(plan.value, current);
      publishAttempt = {
        draft: current,
        plan: plan.value,
        selected: replace ? [...selected.value] : [],
        key: idempotencyKey(),
      };
    }
    const request = publishAttempt;
    const result = await publishSecretDraft(
      request.draft,
      request.plan,
      request.selected,
      request.key,
    );
    if (disposed) return;
    publishAttempt = undefined;
    pending.value = false;
    emit("uncertain", false);
    emit("published", result.draft, result.secret);
    // APPLIED меняет привязку cursor; результаты читаются с первой страницы.
    query.value = "";
    await load();
  } catch (error) {
    if (!disposed) {
      problem.value = safeDraftProblem(error);
      if (
        !retrying &&
        [400, 401, 403, 404, 412, 422].includes(problem.value.status)
      ) {
        publishAttempt = undefined;
        plan.value = undefined;
        page.value = undefined;
        selected.value = [];
      }
      pending.value = Boolean(publishAttempt);
      emit("uncertain", pending.value);
    }
  } finally {
    if (!disposed) working(false);
  }
}

function toggle(ref: string): void {
  if (busy.value || pending.value || plan.value?.state !== "PREPARED") return;
  if (selected.value.includes(ref))
    selected.value = selected.value.filter((item) => item !== ref);
  else if (selected.value.length < 1000) selected.value.push(ref);
}
watch(query, () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (!busy.value) void refresh();
  }, 500);
});
onBeforeUnmount(() => {
  disposed = true;
  controller?.abort();
  clearTimeout(searchTimer);
});
async function restore(): Promise<void> {
  if (!props.initialPlanRef || busy.value) return;
  working(true);
  problem.value = undefined;
  controller?.abort();
  controller = new AbortController();
  try {
    const result = await restoreDraftImpact(
      props.draft,
      props.initialPlanRef,
      controller.signal,
    );
    if (!disposed) {
      plan.value = result.plan;
      page.value = result;
      await selectAvailable();
    }
  } catch (error) {
    if (!disposed) problem.value = safeDraftProblem(error);
  } finally {
    if (!disposed) working(false);
  }
}
onMounted(() => void restore());
</script>

<template>
  <section class="draft-impact">
    <h3>{{ t("runtimeSecrets.draft.impactTitle") }}</h3>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <button
      v-if="initialPlanRef && !plan"
      class="button"
      :disabled="busy"
      @click="restore"
    >
      {{ t("runtimeSecrets.draft.restorePlan") }}
    </button>
    <p>{{ t("runtimeSecrets.draft.impactHelp") }}</p>
    <button
      v-if="
        draft.state === 'VALID' &&
        (!plan || ['EXPIRED', 'CANCELLED'].includes(plan.state))
      "
      class="button"
      :disabled="busy || pending"
      @click="prepare"
    >
      {{ t("runtimeSecrets.draft.prepare") }}
    </button>
    <template v-if="plan">
      <p>
        {{
          t("runtimeSecrets.draft.planTotal", {
            total: plan.total,
            selected: selected.length,
          })
        }}
        · {{ plan.state }}
      </p>
      <button
        v-if="plan.state === 'PREPARED' && selected.length"
        class="button"
        :disabled="busy || pending"
        @click="selected = []"
      >
        {{ t("runtimeSecrets.draft.clearSelection") }}
      </button>
      <label class="field"
        ><span>{{ t("runtimeSecrets.search") }}</span
        ><input v-model="query" type="search" :disabled="busy || pending"
      /></label>
      <button class="button" :disabled="busy" @click="refresh()">
        {{ t("common.refresh") }}
      </button>
      <p v-if="page">
        {{ t("runtimeSecrets.draft.visibleTotal", { total: page.total }) }}
      </p>
      <ul v-if="page" class="draft-impact__items">
        <li v-for="item in page.items" :key="item.ref">
          <label>
            <input
              v-if="plan.state === 'PREPARED'"
              type="checkbox"
              :checked="selected.includes(item.ref)"
              :disabled="busy || pending"
              @change="toggle(item.ref)"
            />
            <span
              >{{ item.consumer.environmentRef }} ·
              {{ item.consumer.environmentVersionRef }}<br />{{
                item.consumer.consumer?.agentRef ??
                t("runtimeSecrets.draft.environmentOnly")
              }}</span
            >
          </label>
          <span>{{ item.outcome }}</span>
          <small v-if="item.resultEnvironmentVersionRef">{{
            item.resultEnvironmentVersionRef
          }}</small>
          <small v-if="item.resultBindingRef"
            >{{ item.resultBindingRef }} ·
            {{ item.resultBindingVersion }}</small
          >
        </li>
      </ul>
      <button
        v-if="page?.nextPageToken"
        class="button"
        :disabled="busy"
        @click="refresh(true)"
      >
        {{ t("runtimeSecrets.loadMore") }}
      </button>
      <button
        v-if="plan.state === 'PREPARED' || pending"
        class="button button--primary"
        :disabled="!canPublish"
        @click="publish(true)"
      >
        {{
          t(
            pending
              ? "runtimeSecrets.draft.retry"
              : "runtimeSecrets.draft.publish",
          )
        }}
      </button>
      <button
        v-if="plan.state === 'PREPARED' && !pending"
        class="button"
        :disabled="!canPublish"
        @click="publish(false)"
      >
        {{ t("runtimeSecrets.draft.publishOnly") }}
      </button>
    </template>
  </section>
</template>

<style scoped>
.draft-impact {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.draft-impact__items {
  display: grid;
  gap: 8px;
  padding: 0;
  list-style: none;
  max-height: 360px;
  overflow: auto;
}
.draft-impact__items li {
  display: grid;
  gap: 6px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow-wrap: anywhere;
}
.draft-impact__items label {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}
</style>
