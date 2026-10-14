<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import type {
  RevisionImpactPlan,
  RevisionImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { usePlatformStore } from "@/features/platform/store";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import {
  publicationPlanIdentity,
  publicationSelection,
  readPublicationImpact,
} from "./publication-impact";

const props = defineProps<{
  plan: RevisionImpactPlan;
  busy?: boolean;
  consumerNames?: Record<string, string>;
}>();
const fieldPrefix = `publication-impact-${useId()}`;
const platform = usePlatformStore();
const emit = defineEmits<{ publish: [selectedItemRefs: string[]] }>();
const page = ref<RevisionImpactPage>();
const itemList = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: itemList,
  itemSelector: ".publication-impact__item",
  itemCount: () => page.value?.items.length ?? 0,
  estimatedViewportHeight: 420,
  estimatedItemHeight: 64,
  minimum: 8,
  maximum: 100,
});
const selected = ref(new Set<string>());
const deselected = ref(new Set<string>());
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
    deselected.value.clear();
    cursors.clear();
  }
  try {
    const next = await readPublicationImpact(
      props.plan,
      active.signal,
      query.value,
      more ? previous?.nextPageToken : undefined,
      pageSize.value,
      platform.bootstrap?.organizationRef,
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
    const defaults = new Set(selected.value);
    for (const item of next.items) {
      if (
        item.outcome === "PENDING" &&
        !deselected.value.has(item.ref) &&
        defaults.size < 1000
      )
        defaults.add(item.ref);
    }
    selected.value = defaults;
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
  if (selected.value.has(ref)) {
    selected.value.delete(ref);
    deselected.value.add(ref);
  } else if (selected.value.size < 1000) {
    selected.value.add(ref);
    deselected.value.delete(ref);
  }
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
  deselected.value.clear();
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
useCursorInfiniteScroll({
  root: itemList,
  sentinel,
  enabled: () =>
    Boolean(page.value?.nextPageToken) && !loading.value && !props.busy,
  loadMore: () => load(true),
});
</script>
<template>
  <section
    class="publication-impact"
    :aria-label="$t('publicationImpact.title')"
    :aria-busy="loading || busy"
  >
    <p class="publication-impact__explanation">
      {{ $t("publicationImpact.explanation") }}
    </p>
    <div class="publication-impact__summary">
      <span>{{
        $t("publicationImpact.snapshotTotal", { count: plan.total })
      }}</span>
      <span v-if="page">
        {{
          $t("publicationImpact.visibleTotal", {
            loaded: page.items.length,
            total: page.total,
          })
        }}
      </span>
      <StatusBadge v-if="page" :state="page.plan.state" />
    </div>
    <label class="publication-impact__search">
      {{ $t("common.search") }}
      <input
        v-model="query"
        name="publication-impact-search"
        type="search"
        maxlength="200"
        :disabled="busy"
      />
    </label>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <template v-if="page">
      <div
        ref="itemList"
        class="publication-impact__items"
        tabindex="0"
        :aria-label="$t('publicationImpact.title')"
      >
        <label
          v-for="(item, index) in page.items"
          :key="item.ref"
          class="publication-impact__item"
        >
          <input
            type="checkbox"
            :id="`${fieldPrefix}-item-${index}`"
            :name="`${fieldPrefix}-item-${index}`"
            :checked="selected.has(item.ref)"
            :disabled="!editable || item.outcome !== 'PENDING'"
            :aria-label="consumerNames?.[item.consumerRef] || item.consumerRef"
            @change="toggle(item.ref)"
          />
          <span class="publication-impact__identity"
            ><span :class="{ mono: !consumerNames?.[item.consumerRef] }">{{
              consumerNames?.[item.consumerRef] || item.consumerRef
            }}</span
            ><br />{{
              $t("impact.bindingVersion", { version: item.bindingVersion })
            }}</span
          >
          <StatusBadge :state="item.outcome" />
        </label>
        <div
          v-if="page.nextPageToken"
          ref="sentinel"
          class="publication-impact__sentinel"
          role="status"
        >
          <span v-if="loading">{{ $t("common.loading") }}</span>
        </div>
      </div>
      <p
        v-if="
          page.plan.state === 'PREPARED' &&
          Date.parse(page.plan.expiresAt) <= now
        "
        role="status"
      >
        {{ $t("publicationImpact.expired") }}
      </p>
      <footer
        v-if="page.plan.state === 'PREPARED'"
        class="publication-impact__actions"
      >
        <button
          type="button"
          class="button button--primary"
          :disabled="!editable"
          @click="publish"
        >
          {{ $t("publicationImpact.publish", { count: selected.size }) }}
        </button>
      </footer>
    </template>
  </section>
</template>
<style scoped>
.publication-impact {
  display: grid;
  gap: 8px;
  min-width: 0;
}
.publication-impact p {
  margin: 0;
}
.publication-impact__explanation,
.publication-impact__summary {
  font-size: 13px;
  line-height: 1.4;
}
.publication-impact__summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 12px;
}
.publication-impact__search {
  display: flex;
  align-items: center;
  gap: 8px;
}
.publication-impact__search input {
  flex: 1 1 auto;
  width: 0;
  min-width: 0;
  height: 32px;
  min-height: 32px;
}
.publication-impact__items {
  max-height: min(280px, 40dvh);
  overflow: auto;
  overscroll-behavior: contain;
}
.publication-impact__item {
  display: flex;
  box-sizing: border-box;
  align-items: center;
  gap: 8px;
  min-height: 56px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  font-size: 14px;
  line-height: 20px;
}
.publication-impact__identity {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
}
.publication-impact__item input {
  flex: 0 0 auto;
}
.publication-impact__item > .status-badge {
  flex: 0 0 auto;
}
.publication-impact__sentinel {
  min-height: 1px;
}
.publication-impact__actions {
  position: sticky;
  bottom: 0;
  z-index: 1;
  display: flex;
  justify-content: flex-end;
  padding: 8px 0;
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.publication-impact__actions .button {
  height: 32px;
  min-height: 32px;
}
@media (max-width: 600px) {
  .publication-impact__actions .button {
    width: 100%;
    height: 44px;
    min-height: 44px;
  }
}
</style>
