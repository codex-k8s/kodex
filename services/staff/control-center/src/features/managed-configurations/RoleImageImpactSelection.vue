<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId } from "vue";
import { useI18n } from "vue-i18n";
import type {
  RoleImageImpactPlan,
  RoleImageImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { type AppProblem } from "@/shared/api/problem";
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
import { useImpactPageLoader } from "@/shared/ui/use-impact-page";
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
const editable = computed(
  () =>
    !props.busy &&
    !loading.value &&
    !problem.value &&
    page.value?.plan.state === "PREPARED" &&
    Date.parse(page.value.plan.expiresAt) > now.value,
);

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
const { load } = useImpactPageLoader<RoleImageImpactPage>({
  page,
  query,
  loading,
  problem,
  busy: () => props.busy,
  identity: () => [
    roleImagePlanIdentity(props.plan),
    props.plan.state,
    props.plan.version,
  ],
  read: (signal, token) =>
    readImageImpact(
      props.plan,
      signal,
      query.value,
      token,
      pageSize.value,
      platform.bootstrap?.organizationRef,
    ),
  clearSelection: () => {
    selected.value.clear();
  },
});
onBeforeUnmount(() => {
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
