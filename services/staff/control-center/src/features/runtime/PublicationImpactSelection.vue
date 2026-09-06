<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type {
  RevisionImpactPlan,
  RevisionImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import {
  publicationPlanIdentity,
  publicationSelection,
  readPublicationImpact,
} from "./publication-impact";

const props = defineProps<{ plan: RevisionImpactPlan; busy?: boolean }>();
const emit = defineEmits<{ publish: [selectedItemRefs: string[]] }>();
const page = ref<RevisionImpactPage>();
const selected = ref(new Set<string>());
const query = ref("");
const loading = ref(false);
const problem = ref<AppProblem>();
const now = ref(Date.now());
const clock = setInterval(() => {
  now.value = Date.now();
}, 1000);
let controller: AbortController | undefined;
let generation = 0;
let debounce: ReturnType<typeof setTimeout> | undefined;
const cursors = new Set<string>();
const editable = computed(
  () =>
    !props.busy &&
    !loading.value &&
    !problem.value &&
    page.value?.plan.state === "PREPARED" &&
    Date.parse(page.value.plan.expiresAt) > now.value,
);

async function load(more = false): Promise<void> {
  if (props.busy || (more && (loading.value || !page.value?.nextPageToken)))
    return;
  clearTimeout(debounce);
  const previous = page.value;
  const current = ++generation;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  loading.value = true;
  problem.value = undefined;
  if (!more) {
    page.value = undefined;
    selected.value.clear();
    cursors.clear();
  }
  try {
    const next = await readPublicationImpact(
      props.plan,
      active.signal,
      query.value,
      more ? previous?.nextPageToken : undefined,
    );
    if (current !== generation) return;
    if (more && previous) {
      if (previous.nextPageToken) cursors.add(previous.nextPageToken);
      if (
        (next.nextPageToken && cursors.has(next.nextPageToken)) ||
        next.total !== previous.total ||
        next.plan.state !== previous.plan.state ||
        previous.items.length + next.items.length > next.total ||
        next.items.some((item) =>
          previous.items.some((old) => old.ref === item.ref),
        )
      )
        throw new Error("Publication impact pagination changed");
      page.value = { ...next, items: [...previous.items, ...next.items] };
    } else page.value = next;
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) loading.value = false;
  }
}
function toggle(ref: string): void {
  if (
    !editable.value ||
    !page.value?.items.some(
      (item) => item.ref === ref && item.outcome === "PENDING",
    )
  )
    return;
  if (selected.value.has(ref)) selected.value.delete(ref);
  else if (selected.value.size < 1000) selected.value.add(ref);
}
function publish(): void {
  if (!editable.value || !page.value) return;
  try {
    const input = publicationSelection(page.value.plan, [...selected.value]);
    emit("publish", input.selectedItemRefs);
  } catch (error) {
    problem.value = asProblem(error);
  }
}
watch(
  () => [
    publicationPlanIdentity(props.plan),
    props.plan.state,
    props.plan.version,
  ],
  () => {
    void load();
  },
  { immediate: true },
);
watch(query, () => {
  controller?.abort();
  generation++;
  clearTimeout(debounce);
  page.value = undefined;
  selected.value.clear();
  loading.value = true;
  debounce = setTimeout(() => {
    void load();
  }, 400);
});
watch(
  () => props.busy,
  (busy, previous) => {
    if (!busy && previous) void load();
  },
);
onBeforeUnmount(() => {
  controller?.abort();
  generation++;
  clearTimeout(debounce);
  clearInterval(clock);
});
</script>
<template>
  <section
    class="publication-impact"
    :aria-label="$t('publicationImpact.title')"
    :aria-busy="loading || busy"
  >
    <p>{{ $t("publicationImpact.explanation") }}</p>
    <p>{{ $t("publicationImpact.snapshotTotal", { count: plan.total }) }}</p>
    <label>
      {{ $t("common.search") }}
      <input v-model="query" type="search" maxlength="200" :disabled="busy" />
    </label>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <template v-if="page">
      <p>
        {{
          $t("publicationImpact.visibleTotal", {
            loaded: page.items.length,
            total: page.total,
          })
        }}
      </p>
      <StatusBadge :state="page.plan.state" />
      <div class="publication-impact__items">
        <label
          v-for="item in page.items"
          :key="item.ref"
          class="publication-impact__item"
        >
          <input
            type="checkbox"
            :checked="selected.has(item.ref)"
            :disabled="!editable || item.outcome !== 'PENDING'"
            :aria-label="item.consumerRef"
            @change="toggle(item.ref)"
          />
          <span
            ><span class="mono">{{ item.consumerRef }}</span
            ><br />{{
              $t("impact.bindingVersion", { version: item.bindingVersion })
            }}</span
          >
          <StatusBadge :state="item.outcome" />
        </label>
      </div>
      <button
        v-if="page.nextPageToken"
        type="button"
        class="button"
        :disabled="loading || busy"
        @click="load(true)"
      >
        {{ $t("impact.more") }}
      </button>
      <p
        v-if="
          page.plan.state === 'PREPARED' &&
          Date.parse(page.plan.expiresAt) <= now
        "
        role="status"
      >
        {{ $t("publicationImpact.expired") }}
      </p>
      <button
        v-if="page.plan.state === 'PREPARED'"
        type="button"
        class="button button--primary"
        :disabled="!editable"
        @click="publish"
      >
        {{ $t("publicationImpact.publish", { count: selected.size }) }}
      </button>
    </template>
  </section>
</template>
<style scoped>
.publication-impact {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.publication-impact__items {
  max-height: min(420px, 50dvh);
  overflow: auto;
}
.publication-impact__item {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 64px;
  padding: 8px;
  border-bottom: 1px solid var(--border);
}
.publication-impact__item > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.publication-impact__item input {
  flex: 0 0 auto;
}
</style>
