<script setup lang="ts">
import { CircleAlert, Layers3, Plus, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { useRuntimeStore } from "@/features/runtime/store";
import { compactIdentifier } from "@/features/runtime/environment-capabilities";
import type { RuntimeEnvironmentSet } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const route = useRoute();
const runtime = useRuntimeStore();
const projectRef = computed(() => String(route.params.projectRef));
const query = ref("");
const items = ref<RuntimeEnvironmentSet[]>([]);
const cursor = ref<string>();
const loading = ref(false);
const loadingMore = ref(false);
const problem = ref<AppProblem>();
const selectedRef = ref("");
const selected = computed(
  () =>
    items.value.find((item) => item.ref === selectedRef.value) ??
    items.value[0],
);
let generation = 0;
let debounceTimer: ReturnType<typeof setTimeout> | undefined;

async function load(reset = true): Promise<void> {
  if (!reset && (!cursor.value || loadingMore.value)) return;
  const current = ++generation;
  if (reset) {
    loading.value = true;
    cursor.value = undefined;
  } else {
    loadingMore.value = true;
  }
  problem.value = undefined;
  try {
    const page = await runtime.searchEnvironmentPage(
      projectRef.value,
      query.value,
      reset ? undefined : cursor.value,
    );
    if (generation !== current) return;
    if (reset) items.value = page.items;
    else {
      const merged = new Map(items.value.map((item) => [item.ref, item]));
      for (const item of page.items) merged.set(item.ref, item);
      items.value = [...merged.values()];
    }
    cursor.value = page.nextPageToken;
    if (
      !selectedRef.value ||
      !items.value.some((item) => item.ref === selectedRef.value)
    )
      selectedRef.value = items.value[0]?.ref ?? "";
  } catch (error) {
    if (generation === current) problem.value = asProblem(error);
  } finally {
    if (generation === current) {
      loading.value = false;
      loadingMore.value = false;
    }
  }
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (
    cursor.value &&
    element.scrollTop + element.clientHeight >= element.scrollHeight - 80
  )
    void load(false);
}

watch(query, () => {
  if (debounceTimer) clearTimeout(debounceTimer);
  debounceTimer = setTimeout(() => void load(), 300);
});
watch(projectRef, () => void load());
onMounted(() => void load());
onBeforeUnmount(() => {
  generation += 1;
  if (debounceTimer) clearTimeout(debounceTimer);
});
</script>

<template>
  <PageFrame
    :title="$t('runtime.environmentsTitle')"
    :subtitle="$t('runtime.environmentsSubtitle')"
  >
    <template #actions>
      <RouterLink
        class="button button--primary"
        :to="`/projects/${encodeURIComponent(projectRef)}/environments/new`"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ $t("runtime.newEnvironment") }}
      </RouterLink>
    </template>
    <section class="environment-registry panel">
      <header class="environment-toolbar">
        <label>
          <Search :size="16" aria-hidden="true" />
          <span class="sr-only">{{ $t("runtime.searchEnvironment") }}</span>
          <input
            v-model="query"
            type="search"
            :placeholder="$t('runtime.searchEnvironment')"
          />
        </label>
        <span>{{ $t("runtime.pickerShown", { count: items.length }) }}</span>
      </header>
      <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
      <div v-else class="environment-registry__content">
        <div
          class="environment-table-wrap"
          :aria-busy="loading || loadingMore"
          @scroll="onScroll"
        >
          <div v-if="loading" class="environment-state" role="status">
            {{ $t("common.loading") }}
          </div>
          <div v-else-if="!items.length" class="environment-state">
            <Layers3 :size="28" aria-hidden="true" />
            <strong>{{ $t("runtime.environmentsEmpty") }}</strong>
            <p>{{ $t("runtime.environmentsEmptyHelp") }}</p>
          </div>
          <table v-else class="environment-table">
            <thead>
              <tr>
                <th>{{ $t("common.name") }}</th>
                <th>{{ $t("runtime.revision") }}</th>
                <th>{{ $t("runtime.versionDigest") }}</th>
                <th>{{ $t("runtime.exactImage") }}</th>
                <th>{{ $t("runtime.verifiedTools") }}</th>
                <th>{{ $t("runtime.variables") }}</th>
                <th>{{ $t("runtime.secretDescriptors") }}</th>
                <th>{{ $t("common.status") }}</th>
                <th>
                  <span class="sr-only">{{ $t("common.actions") }}</span>
                </th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="environment in items"
                :key="environment.ref"
                :class="{
                  'environment-table__row--selected':
                    environment.ref === selected?.ref,
                }"
              >
                <td>
                  <button
                    class="environment-name"
                    type="button"
                    @click="selectedRef = environment.ref"
                  >
                    <strong>{{ environment.name }}</strong>
                    <small>{{ environment.description }}</small>
                  </button>
                </td>
                <td>rev {{ environment.currentVersion.revision }}</td>
                <td>
                  <code>{{
                    compactIdentifier(environment.currentVersion.digest)
                  }}</code>
                </td>
                <td>
                  <code>{{
                    compactIdentifier(environment.currentVersion.image.digest)
                  }}</code>
                </td>
                <td>{{ environment.currentVersion.tools.length }}</td>
                <td>{{ environment.currentVersion.values.length }}</td>
                <td>
                  {{ environment.currentVersion.secretDescriptors.length }}
                </td>
                <td><StatusBadge :state="environment.state" /></td>
                <td>
                  <RouterLink
                    class="button"
                    :to="`/projects/${encodeURIComponent(projectRef)}/environments/${encodeURIComponent(environment.ref)}`"
                  >
                    {{ $t("common.open") }}
                  </RouterLink>
                </td>
              </tr>
            </tbody>
          </table>
          <p v-if="loadingMore" class="environment-loading" role="status">
            {{ $t("common.loading") }}
          </p>
          <p
            v-else-if="cursor"
            class="environment-loading environment-loading--hint"
          >
            {{ $t("runtime.pickerScroll") }}
          </p>
        </div>
        <aside v-if="selected" class="environment-inspector">
          <div class="section-header">
            <div>
              <h2>{{ selected.name }}</h2>
              <p>{{ selected.description }}</p>
            </div>
            <StatusBadge :state="selected.state" />
          </div>
          <dl>
            <div>
              <dt>{{ $t("runtime.revision") }}</dt>
              <dd>rev {{ selected.currentVersion.revision }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.versionDigest") }}</dt>
              <dd>
                <code>{{
                  compactIdentifier(selected.currentVersion.digest)
                }}</code>
              </dd>
            </div>
            <div>
              <dt>{{ $t("runtime.variables") }}</dt>
              <dd>{{ selected.currentVersion.values.length }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.secretDescriptors") }}</dt>
              <dd>{{ selected.currentVersion.secretDescriptors.length }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.updatedAt") }}</dt>
              <dd>{{ new Date(selected.updatedAt).toLocaleString() }}</dd>
            </div>
            <div>
              <dt>{{ $t("runtime.exactImage") }}</dt>
              <dd>
                <code>{{
                  compactIdentifier(selected.currentVersion.image.digest)
                }}</code>
              </dd>
            </div>
          </dl>
          <section>
            <h3>{{ $t("runtime.verifiedTools") }}</h3>
            <div v-if="selected.currentVersion.tools.length" class="chip-list">
              <span
                v-for="tool in selected.currentVersion.tools"
                :key="tool.command"
                :title="tool.description"
              >
                {{ tool.name }} · <code>{{ tool.command }}</code>
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <section>
            <h3>{{ $t("runtime.variableNames") }}</h3>
            <div v-if="selected.currentVersion.values.length" class="chip-list">
              <span
                v-for="item in selected.currentVersion.values"
                :key="item.name"
              >
                {{ item.name }}
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <section class="environment-boundary" role="note">
            <CircleAlert :size="17" aria-hidden="true" />
            <div>
              <strong>{{ $t("runtime.effectivePolicyPreview") }}</strong>
              <p>{{ $t("runtime.catalogCapabilityBoundary") }}</p>
            </div>
          </section>
          <section>
            <h3>{{ $t("runtime.secretDescriptorNames") }}</h3>
            <div
              v-if="selected.currentVersion.secretDescriptors.length"
              class="chip-list"
            >
              <span
                v-for="item in selected.currentVersion.secretDescriptors"
                :key="item.name"
              >
                {{ item.name }}
              </span>
            </div>
            <p v-else>{{ $t("common.empty") }}</p>
          </section>
          <RouterLink
            class="button button--primary"
            :to="`/projects/${encodeURIComponent(projectRef)}/environments/${encodeURIComponent(selected.ref)}`"
          >
            {{ $t("runtime.openEditor") }}
          </RouterLink>
        </aside>
      </div>
    </section>
  </PageFrame>
</template>

<style scoped>
.environment-registry {
  padding: 0;
  overflow: hidden;
}
.environment-toolbar {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.environment-toolbar label {
  display: flex;
  width: min(460px, 100%);
  align-items: center;
  gap: 8px;
}
.environment-toolbar input {
  width: 100%;
}
.environment-toolbar > span {
  color: var(--text-secondary);
}
.environment-table-wrap {
  max-height: calc(100vh - 250px);
  min-height: 360px;
  overflow: auto;
}
.environment-registry__content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.38fr);
}
.environment-table {
  width: 100%;
  min-width: 1120px;
  border-collapse: collapse;
}
.environment-table th,
.environment-table td {
  padding: 12px 14px;
  border-bottom: 1px solid var(--hairline);
  text-align: left;
  vertical-align: middle;
}
.environment-table th {
  position: sticky;
  z-index: 1;
  top: 0;
  background: var(--panel);
  color: var(--text-secondary);
  font-size: 0.78rem;
  font-weight: 500;
}
.environment-table td:first-child {
  min-width: 280px;
}
.environment-table td:first-child > * {
  display: block;
}
.environment-table small {
  max-width: 520px;
  margin-top: 3px;
  overflow: hidden;
  color: var(--text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.environment-table code,
.environment-inspector code {
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.environment-name {
  display: grid;
  width: 100%;
  gap: 3px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--text);
  text-align: left;
  cursor: pointer;
}
.environment-table__row--selected {
  box-shadow: inset 3px 0 var(--accent);
  background: var(--accent-soft);
}
.environment-inspector {
  display: grid;
  align-content: start;
  gap: 18px;
  padding: 16px;
  border-left: 1px solid var(--border);
  background: var(--surface);
}
.environment-inspector .section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.environment-inspector .section-header p {
  margin-bottom: 0;
  color: var(--text-secondary);
}
.environment-inspector dl {
  margin: 0;
}
.environment-inspector dl > div {
  display: grid;
  grid-template-columns: minmax(100px, 0.55fr) minmax(0, 1fr);
  gap: 8px;
  padding: 9px 0;
  border-bottom: 1px solid var(--hairline);
}
.environment-inspector dt {
  color: var(--text-secondary);
}
.environment-inspector dd {
  margin: 0;
}
.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip-list span {
  padding: 4px 7px;
  border-radius: 5px;
  background: var(--panel);
  font-family: var(--font-mono);
  font-size: 0.76rem;
}
.environment-state {
  display: grid;
  min-height: 360px;
  place-items: center;
  align-content: center;
  gap: 8px;
  padding: 24px;
  text-align: center;
}
.environment-state p {
  max-width: 500px;
  color: var(--text-secondary);
}
.environment-loading {
  padding: 12px;
  text-align: center;
}
.environment-loading--hint {
  color: var(--text-secondary);
}
.environment-boundary {
  display: flex;
  gap: 9px;
  padding: 11px 0;
  border-top: 1px solid var(--hairline);
  color: var(--text-secondary);
}
.environment-boundary p {
  margin: 4px 0 0;
}
@media (max-width: 780px) {
  .environment-toolbar {
    align-items: stretch;
    flex-direction: column;
  }
  .environment-table-wrap {
    overflow-x: auto;
  }
  .environment-registry__content {
    grid-template-columns: 1fr;
  }
  .environment-inspector {
    border-top: 1px solid var(--border);
    border-left: 0;
  }
  .environment-table {
    min-width: 760px;
  }
}
</style>
