<script setup lang="ts">
import { Box, ChevronRight, Plus, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useRoleImagesStore } from "@/features/role-images/store";
import { usePlatformStore } from "@/features/platform/store";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import { useCursorInfiniteScroll } from "@/shared/ui/async-entity-picker";
import { useAdaptiveCursorPageSize } from "@/shared/ui/cursor-list";
import EntityIcon from "@/shared/ui/EntityIcon.vue";
import RoleImageLineage from "./RoleImageLineage.vue";

const props = defineProps<{ projectRef: string }>();
const { t } = useI18n();
const localizeServerMessage = useServerMessage();
const fieldId = useId();
const store = useRoleImagesStore();
const platform = usePlatformStore();
const query = ref("");
const state = ref<"ALL" | "ACTIVE" | "ARCHIVED">("ALL");
const items = computed(() => store.catalog(props.projectRef));
const scrollRoot = ref<HTMLElement>();
const sentinel = ref<HTMLElement>();
const pageSize = useAdaptiveCursorPageSize({
  container: scrollRoot,
  itemSelector: ".role-image-catalog__row",
  itemCount: () => items.value.length,
  estimatedItemHeight: 72,
  estimatedColumns: 1,
});
useCursorInfiniteScroll({
  sentinel,
  enabled: () =>
    Boolean(store.projectNextPageToken[props.projectRef]) &&
    !store.loadingCatalog &&
    !store.loadingMore,
  loadMore: () =>
    store.loadCatalog(props.projectRef, false, undefined, pageSize.value),
});
function applyRealtimeCatalog(): boolean {
  if (query.value.trim() || state.value !== "ALL") return false;
  const snapshot = platform.realtimeSnapshot(
    "ROLE_IMAGE_RECIPE",
    props.projectRef,
  );
  if (!snapshot) return true;
  store.applyCatalogSnapshot(
    props.projectRef,
    Object.values(platform.roleImageRecipes).filter(
      (recipe) => recipe.projectRef === props.projectRef,
    ),
    snapshot.nextPageToken,
    snapshot.total,
  );
  return true;
}
function loadFiltered() {
  if (applyRealtimeCatalog()) return Promise.resolve();
  return store.loadCatalog(
    props.projectRef,
    true,
    {
      ...(query.value.trim() ? { query: query.value.trim() } : {}),
      ...(state.value === "ALL" ? {} : { state: state.value }),
    },
    pageSize.value,
  );
}

const realtimeVersion = computed(() => {
  const snapshot = platform.realtimeSnapshot(
    "ROLE_IMAGE_RECIPE",
    props.projectRef,
  );
  return JSON.stringify([
    snapshot?.nextPageToken,
    snapshot?.total,
    Object.values(platform.roleImageRecipes)
      .filter((recipe) => recipe.projectRef === props.projectRef)
      .map((recipe) => [recipe.ref, recipe.version]),
  ]);
});

const supportingCatalogVersion = computed(() =>
  JSON.stringify([
    props.projectRef,
    Object.values(platform.agents)
      .filter((agent) => agent.projectRef === props.projectRef)
      .map((agent) => [
        agent.ref,
        agent.version,
        agent.roleDefinitionRef,
        agent.roleDefinitionName,
      ]),
    Object.values(platform.roleEnvironments).map((environment) => [
      environment.key,
      environment.nameMessageKey,
      environment.available,
    ]),
  ]),
);

function applyRealtimeSupportingCatalogs(): void {
  store.applySupportingCatalogSnapshot(
    Object.values(platform.agents).filter(
      (agent) => agent.projectRef === props.projectRef,
    ),
    Object.values(platform.roleEnvironments),
  );
}

async function load(): Promise<void> {
  await Promise.all([
    loadFiltered(),
    store.loadSupportingCatalogs(props.projectRef, {
      agents: Object.values(platform.agents).filter(
        (agent) => agent.projectRef === props.projectRef,
      ),
      environments: Object.values(platform.roleEnvironments),
    }),
  ]);
}

watch(
  () => props.projectRef,
  () => void load(),
);
watch([query, state], () => void loadFiltered());
watch(realtimeVersion, applyRealtimeCatalog);
watch(supportingCatalogVersion, applyRealtimeSupportingCatalogs, {
  immediate: true,
});
onMounted(() => void load());
onBeforeUnmount(() => store.dispose());
</script>

<template>
  <section class="role-image-catalog">
    <header class="role-image-catalog__toolbar">
      <label class="catalog-search" :for="`${fieldId}-search`">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t("roleImages.search") }}</span>
        <input
          :id="`${fieldId}-search`"
          v-model="query"
          :name="`${fieldId}-search`"
          type="search"
          maxlength="128"
          :placeholder="t('roleImages.search')"
        />
      </label>
      <label class="catalog-filter" :for="`${fieldId}-state`">
        <span>{{ t("common.status") }}</span>
        <select
          :id="`${fieldId}-state`"
          v-model="state"
          :name="`${fieldId}-state`"
        >
          <option value="ALL">{{ t("common.all") }}</option>
          <option value="ACTIVE">{{ t("common.active") }}</option>
          <option value="ARCHIVED">{{ t("roleImages.archived") }}</option>
        </select>
      </label>
      <span
        v-if="store.projectTotal[projectRef] !== undefined"
        class="catalog-count"
      >
        {{ t("roleImages.total", { count: store.projectTotal[projectRef] }) }}
      </span>
      <RouterLink
        v-if="store.createAllowed[projectRef]"
        class="button button--primary"
        :to="`/projects/${encodeURIComponent(projectRef)}/role-images/new`"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ t("roleImages.new") }}
      </RouterLink>
    </header>

    <ProblemNotice
      v-if="store.problem"
      :problem="store.problem"
      @retry="load"
    />

    <div
      ref="scrollRoot"
      class="role-image-catalog__scroll"
      :aria-busy="store.loadingCatalog || store.loadingMore"
    >
      <div
        v-if="store.loadingCatalog && !items.length"
        class="catalog-state"
        role="status"
      >
        {{ t("common.loading") }}
      </div>
      <div v-else-if="!items.length" class="catalog-state">
        <Box :size="32" aria-hidden="true" />
        <strong>{{ t("roleImages.empty") }}</strong>
        <p>{{ t("roleImages.emptyHelp") }}</p>
      </div>
      <div v-else class="role-image-catalog__table-wrap">
        <table class="role-image-catalog__table">
          <thead>
            <tr>
              <th scope="col">{{ t("catalog.table.name") }}</th>
              <th scope="col">{{ t("roleImages.environment") }}</th>
              <th scope="col">{{ t("common.status") }}</th>
              <th scope="col">{{ t("roleImages.promotion") }}</th>
              <th scope="col">{{ t("roleImages.generation") }}</th>
              <th scope="col">{{ t("roleImages.updatedAt") }}</th>
              <th scope="col" class="role-image-catalog__open">
                {{ t("common.open") }}
              </th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="recipe in items"
              :key="recipe.ref"
              class="role-image-catalog__row"
            >
              <td>
                <div class="role-image-catalog__identity">
                  <EntityIcon kind="ROLE_IMAGE" />
                  <div>
                    <RouterLink
                      :to="`/projects/${encodeURIComponent(projectRef)}/role-images/${encodeURIComponent(recipe.ref)}`"
                      :title="localizeServerMessage(recipe.name)"
                      >{{ localizeServerMessage(recipe.name) }}</RouterLink
                    >
                    <small>{{
                      store.roleDefinitionByRef.get(recipe.roleDefinitionRef)
                        ?.label ?? t("roleImages.unknownRole")
                    }}</small>
                  </div>
                </div>
              </td>
              <td>
                <span>{{
                  store.environmentByKey.get(recipe.environment.environmentKey)
                    ? t(
                        store.environmentByKey.get(
                          recipe.environment.environmentKey,
                        )!.nameMessageKey,
                      )
                    : recipe.environment.environmentKey
                }}</span>
                <RoleImageLineage
                  :lineage="recipe.managedLineage"
                  collapsible
                />
              </td>
              <td><StatusBadge :state="recipe.state" /></td>
              <td>
                <StatusBadge
                  :state="recipe.promotedImageReady ? 'PROMOTED' : 'PENDING'"
                  :label="
                    recipe.promotedImageReady
                      ? t('roleImages.promoted')
                      : t('roleImages.notPromoted')
                  "
                />
              </td>
              <td>{{ recipe.generation }}</td>
              <td>
                <time :datetime="recipe.updatedAt">{{
                  new Date(recipe.updatedAt).toLocaleString()
                }}</time>
              </td>
              <td class="role-image-catalog__open">
                <RouterLink
                  class="icon-button"
                  :to="`/projects/${encodeURIComponent(projectRef)}/role-images/${encodeURIComponent(recipe.ref)}`"
                  :aria-label="t('common.open')"
                  :title="t('common.open')"
                  ><ChevronRight :size="18" aria-hidden="true"
                /></RouterLink>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="store.loadingMore" class="catalog-loading" role="status">
        {{ t("common.loading") }}
      </p>
      <div
        v-if="store.projectNextPageToken[projectRef]"
        ref="sentinel"
        class="cursor-sentinel"
      />
    </div>
  </section>
</template>

<style scoped>
.role-image-catalog {
  min-width: 0;
}
.role-image-catalog__toolbar {
  display: flex;
  flex-wrap: wrap;
  min-height: 60px;
  align-items: end;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.catalog-search {
  display: flex;
  width: min(520px, 100%);
  align-items: center;
  gap: 8px;
}
.catalog-search input {
  width: 100%;
}
.catalog-filter {
  display: grid;
  min-width: 160px;
  gap: 4px;
}
.catalog-filter span {
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.catalog-count {
  margin-left: auto;
  color: var(--text-secondary);
  white-space: nowrap;
}
.catalog-limit {
  padding: 8px 14px;
  margin: 0;
  border-bottom: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 0.78rem;
}
.role-image-catalog__scroll {
  min-width: 0;
  padding: 14px;
}
.role-image-catalog__table-wrap {
  overflow-x: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.role-image-catalog__table {
  width: 100%;
  min-width: 980px;
  table-layout: fixed;
  border-collapse: collapse;
}
.role-image-catalog__table th,
.role-image-catalog__table td {
  padding: 10px 12px;
  text-align: left;
  vertical-align: middle;
}
.role-image-catalog__table th {
  color: var(--muted);
  font-size: 0.72rem;
  font-weight: 600;
}
.role-image-catalog__table th:nth-child(1) {
  width: 28%;
}
.role-image-catalog__table th:nth-child(2) {
  width: 18%;
}
.role-image-catalog__table th:nth-child(3) {
  width: 12%;
}
.role-image-catalog__table th:nth-child(4) {
  width: 15%;
}
.role-image-catalog__table th:nth-child(5) {
  width: 8%;
}
.role-image-catalog__table th:nth-child(6) {
  width: 14%;
}
.role-image-catalog__table th:nth-child(7) {
  width: 5%;
}
.role-image-catalog__row {
  min-height: 72px;
  border-top: 1px solid var(--border);
}
.role-image-catalog__row:hover {
  background: var(--panel);
}
.role-image-catalog__identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.role-image-catalog__identity > div {
  min-width: 0;
}
.role-image-catalog__identity a,
.role-image-catalog__identity small {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.role-image-catalog__identity a {
  color: var(--text);
  font-weight: 600;
  text-decoration: none;
}
.role-image-catalog__identity a:hover {
  color: var(--accent-strong);
  text-decoration: underline;
}
.role-image-catalog__identity small,
.role-image-catalog__table time {
  color: var(--muted);
  font-size: 0.75rem;
}
.role-image-catalog__table td:nth-child(2) > span {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.role-image-catalog__table :deep(.role-image-lineage summary) {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.role-image-catalog__table :deep(.role-image-lineage summary span) {
  display: none;
}
.role-image-catalog__open {
  text-align: center !important;
}
.catalog-state {
  display: grid;
  min-height: 380px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--text-secondary);
  text-align: center;
}
.catalog-state p {
  max-width: 420px;
  margin: 0;
}
.catalog-loading {
  margin: 16px auto 0;
}
@media (max-width: 820px) {
  .role-image-catalog__toolbar {
    align-items: stretch;
    flex-wrap: wrap;
  }
  .catalog-search {
    flex: 1 1 100%;
  }
  .catalog-count {
    margin-left: 0;
  }
}
</style>
