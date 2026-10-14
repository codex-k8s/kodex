<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  RoleImageImpactPlan,
  RoleImageImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { usePlatformStore } from "@/features/platform/store";
import { useRuntimeStore } from "@/features/runtime/store";
import {
  runtimeResourceIdentityKey,
  validRuntimeResourceIdentity,
} from "@/features/runtime/resource-scope";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import { roleImagePlanIdentity, readImageImpact } from "./role-image-impact";

const props = defineProps<{ plan: RoleImageImpactPlan; busy?: boolean }>();
const fieldPrefix = `role-image-impact-${useId()}`;
const platform = usePlatformStore();
const runtime = useRuntimeStore();
const { t } = useI18n();
const emit = defineEmits<{ apply: [selectedItemRefs: string[]] }>();
const page = ref<RoleImageImpactPage>();
const displayItems = computed(() =>
  (page.value?.items ?? []).map((item) => {
    const organizationRef = platform.bootstrap?.organizationRef;
    const scoped = validRuntimeResourceIdentity(item, organizationRef);
    const environment = runtime.environments[item.environmentRef];
    const environmentName =
      scoped &&
      environment?.ref === item.environmentRef &&
      environment.state !== "DELETED" &&
      validRuntimeResourceIdentity(environment, organizationRef) &&
      runtimeResourceIdentityKey(environment) ===
        runtimeResourceIdentityKey(item)
        ? environment.name.trim()
        : "";
    const consumer = item.consumer;
    const agent = consumer ? platform.agents[consumer.agentRef] : undefined;
    const agentName =
      scoped &&
      consumer?.scopeKind === "PROJECT" &&
      validRuntimeResourceIdentity(consumer, organizationRef) &&
      runtimeResourceIdentityKey(consumer) ===
        runtimeResourceIdentityKey(item) &&
      agent?.ref === consumer.agentRef &&
      agent.projectRef === consumer.projectRef
        ? agent.name.trim()
        : "";
    const title =
      (consumer ? agentName : environmentName) ||
      t(consumer ? "nav.agent" : "nav.environment");
    return {
      item,
      title,
      environmentName: consumer ? environmentName : "",
      reference: consumer?.agentRef ?? item.environmentRef,
    };
  }),
);
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
    const next = await readImageImpact(
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
  emit("apply", [...selected.value]);
}
watch(
  () => [
    roleImagePlanIdentity(props.plan),
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
    :aria-label="$t('managed.impact')"
    :aria-busy="loading || busy"
  >
    <p>{{ $t("roleImageImpact.explanation") }}</p>
    <p>{{ $t("publicationImpact.snapshotTotal", { count: plan.total }) }}</p>
    <p v-if="plan.total === 0" role="status">
      {{ $t("roleImageImpact.noConsumers") }}
    </p>
    <label v-else>
      {{ $t("common.search") }}
      <input
        v-model="query"
        :id="`${fieldPrefix}-search`"
        :name="`${fieldPrefix}-search`"
        type="search"
        maxlength="200"
        :disabled="busy"
      />
    </label>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <template v-if="page">
      <p v-if="plan.total > 0">
        {{
          $t("publicationImpact.visibleTotal", {
            loaded: page.items.length,
            total: page.total,
          })
        }}
      </p>
      <StatusBadge :state="page.plan.state" />
      <div ref="itemList" class="publication-impact__items">
        <div
          v-for="(
            { item, title, environmentName, reference }, index
          ) in displayItems"
          :key="item.ref"
          class="publication-impact__item"
        >
          <label class="publication-impact__choice">
            <input
              type="checkbox"
              :id="`${fieldPrefix}-item-${index}`"
              :name="`${fieldPrefix}-item-${index}`"
              :checked="selected.has(item.ref)"
              :disabled="!editable || item.outcome !== 'PENDING'"
              :aria-label="title"
              @change="toggle(item.ref)"
            />
            <span class="publication-impact__identity">
              <strong>{{ title }}</strong>
              <small v-if="environmentName">{{ environmentName }}</small>
              <small>{{
                $t("roleImageImpact.version", {
                  version:
                    item.consumer?.bindingVersion ?? item.environmentVersion,
                })
              }}</small>
            </span>
            <StatusBadge :state="item.outcome" />
          </label>
          <details class="publication-impact__details">
            <summary>{{ $t("common.details") }}</summary>
            <small class="mono">{{ reference }}</small>
          </details>
        </div>
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
      <button
        v-if="page.plan.state === 'PREPARED' && page.plan.total > 0"
        type="button"
        class="button button--primary"
        :disabled="!editable"
        @click="publish"
      >
        {{ $t("roleImageImpact.apply", { count: selected.size }) }}
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
  display: grid;
  gap: 4px;
  min-height: 64px;
  padding: 8px;
  border-bottom: 1px solid var(--border);
}
.publication-impact__choice {
  display: flex;
  align-items: center;
  gap: 12px;
}
.publication-impact__choice > span {
  min-width: 0;
  overflow-wrap: anywhere;
}
.publication-impact__identity {
  display: grid;
  flex: 1;
  gap: 2px;
}
.publication-impact__details {
  padding-left: 28px;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.publication-impact__item input {
  flex: 0 0 auto;
}
.publication-impact__sentinel {
  min-height: 1px;
}
</style>
