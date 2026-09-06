<script setup lang="ts">
import { Expand, Plus, Search } from "@lucide/vue";
import { onBeforeUnmount, ref, watch } from "vue";
import type { ManagedConfigurationSummary } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { nearScrollEnd } from "@/shared/ui/async-entity-picker";
import { listConfigurations, type ConfigurationKind } from "./api";
const props = defineProps<{
  kind: ConfigurationKind;
  projectRef?: string;
  expanded?: boolean;
}>();
const query = ref("");
const items = ref<ManagedConfigurationSummary[]>([]);
const nextPageToken = ref<string>();
const total = ref(0);
const loading = ref(false);
const expansionOpen = ref(false);
const problem = ref<AppProblem>();
const cursors = new Set<string>();
let generation = 0;
let controller: AbortController | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
async function load(more = false): Promise<void> {
  if (more && (!nextPageToken.value || loading.value)) return;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const current = ++generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const token = more ? nextPageToken.value : undefined;
    const page = await listConfigurations({
      kind: props.kind,
      projectRef: props.projectRef,
      query: query.value.trim(),
      pageToken: token,
      signal: request.signal,
    });
    if (request.signal.aborted || generation !== current) return;
    const next = more ? [...items.value, ...page.items] : page.items;
    if (
      !Number.isSafeInteger(page.total) ||
      page.total < 0 ||
      next.some(
        (item) =>
          item.kind !== props.kind ||
          (props.projectRef && item.projectRef !== props.projectRef),
      ) ||
      new Set(next.map((item) => item.ref)).size !== next.length ||
      (page.nextPageToken &&
        (page.nextPageToken === token ||
          (more && cursors.has(page.nextPageToken))))
    )
      throw new Error("Invalid managed configuration catalog");
    if (!more) cursors.clear();
    if (token) cursors.add(token);
    items.value = next;
    total.value = page.total;
    nextPageToken.value = page.nextPageToken || undefined;
  } catch (error) {
    if (!request.signal.aborted && generation === current)
      problem.value = asProblem(error);
  } finally {
    if (generation === current) loading.value = false;
  }
}
watch(
  () => [props.kind, props.projectRef, query.value],
  () => {
    controller?.abort();
    generation += 1;
    if (timer) clearTimeout(timer);
    items.value = [];
    total.value = 0;
    nextPageToken.value = undefined;
    problem.value = undefined;
    loading.value = true;
    timer = setTimeout(() => {
      void load();
    }, 500);
  },
  { immediate: true, flush: "sync" },
);
onBeforeUnmount(() => {
  controller?.abort();
  if (timer) clearTimeout(timer);
  generation += 1;
});
function scroll(event: Event): void {
  if (
    event.currentTarget instanceof HTMLElement &&
    nearScrollEnd(event.currentTarget) &&
    !problem.value
  )
    void load(true);
}
</script>
<template>
  <section class="configuration-catalog">
    <header>
      <label
        ><Search :size="18" /><input
          v-model="query"
          type="search"
          :placeholder="$t('common.search')"
          :aria-label="$t('common.search')"
      /></label>
      <RouterLink
        class="button button--primary"
        :to="{
          name: 'configuration',
          params: { kind, configurationRef: 'new' },
          query: projectRef ? { projectRef } : {},
        }"
        ><Plus :size="18" />{{ $t("common.create") }}</RouterLink
      >
      <button
        v-if="!props.expanded && (total > 6 || nextPageToken)"
        class="icon-button"
        :title="$t('catalog.expand')"
        :aria-label="$t('catalog.expand')"
        @click="expansionOpen = true"
      >
        <Expand :size="18" />
      </button>
    </header>
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading && !items.length" role="status">
      {{ $t("common.loading") }}
    </p>
    <p v-else-if="!items.length && !problem">{{ $t("common.empty") }}</p>
    <div
      class="configuration-catalog__list"
      :class="{ 'configuration-catalog__list--expanded': props.expanded }"
      @scroll.passive="scroll"
    >
      <RouterLink
        v-for="item in items"
        :key="item.ref"
        class="configuration-catalog__row"
        :to="{
          name: 'configuration',
          params: { kind: item.kind, configurationRef: item.ref },
        }"
      >
        <div>
          <strong>{{ item.name }}</strong
          ><small
            >{{ item.managedBy
            }}<template v-if="item.source"> · {{ item.source }}</template
            ><template v-if="item.sourceRevision">
              · {{ item.sourceRevision }}</template
            ></small
          >
        </div>
        <StatusBadge :state="item.currentRevision?.state ?? 'DRAFT'" /><span
          >v{{ item.currentRevision?.revision ?? item.version }}</span
        >
      </RouterLink>
      <button
        v-if="nextPageToken"
        class="button"
        :disabled="loading"
        @click="load(true)"
      >
        {{ $t("managed.more") }} ({{ items.length }}/{{ total }})
      </button>
    </div>
    <ModalDialog
      v-if="expansionOpen"
      :title="$t(`managed.kinds.${kind}`)"
      size="xl"
      @close="expansionOpen = false"
      ><ConfigurationCatalog :kind="kind" :project-ref="projectRef" expanded
    /></ModalDialog>
  </section>
</template>
<style scoped>
.configuration-catalog {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.configuration-catalog > header {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
}
.configuration-catalog label {
  display: flex;
  gap: 8px;
  align-items: center;
  min-width: 0;
  flex: 1;
}
.configuration-catalog input {
  width: 100%;
  min-width: 0;
}
.configuration-catalog__list {
  max-height: 576px;
  overflow-y: auto;
}
.configuration-catalog__list--expanded {
  max-height: 65vh;
}
.configuration-catalog__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 12px;
  align-items: center;
  height: 96px;
  padding: 12px 0;
  color: inherit;
  text-decoration: none;
  border-bottom: 1px solid var(--border);
}
.configuration-catalog__row > div {
  min-width: 0;
  overflow-wrap: anywhere;
}
.configuration-catalog__row strong,
.configuration-catalog__row small {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.configuration-catalog__row small {
  margin-top: 4px;
  color: var(--muted);
}
@media (max-width: 600px) {
  .configuration-catalog__row {
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .configuration-catalog__row > span:last-child {
    display: none;
  }
}
</style>
