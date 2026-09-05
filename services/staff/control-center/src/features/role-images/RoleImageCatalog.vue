<script setup lang="ts">
import { Box, Layers3, Maximize2, Plus, Search } from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useRoleImagesStore } from "@/features/role-images/store";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import RoleImageLineage from "./RoleImageLineage.vue";

const props = defineProps<{ projectRef: string }>();
const { t } = useI18n();
const store = useRoleImagesStore();
const query = ref("");
const expanded = ref(false);
const state = ref<"ALL" | "ACTIVE" | "ARCHIVED">("ALL");
const items = computed(() => store.catalog(props.projectRef));
function loadFiltered() {
  return store.loadCatalog(props.projectRef, true, {
    ...(query.value.trim() ? { query: query.value.trim() } : {}),
    ...(state.value === "ALL" ? {} : { state: state.value }),
  });
}

async function load(): Promise<void> {
  await Promise.all([
    loadFiltered(),
    store.loadSupportingCatalogs(props.projectRef),
  ]);
}

function onScroll(event: Event): void {
  const element = event.currentTarget as HTMLElement;
  if (
    element.scrollTop + element.clientHeight >= element.scrollHeight - 100 &&
    store.projectNextPageToken[props.projectRef]
  )
    void store.loadCatalog(props.projectRef, false);
}

watch(
  () => props.projectRef,
  () => void load(),
);
watch([query, state], () => void loadFiltered());
onMounted(() => void load());
onBeforeUnmount(() => store.dispose());
</script>

<template>
  <component
    :is="expanded ? ModalDialog : 'section'"
    class="role-image-catalog"
    :class="{ 'role-image-catalog--expanded': expanded }"
    :title="expanded ? t('roleImages.title') : undefined"
    size="full"
    @close="expanded = false"
  >
    <header class="role-image-catalog__toolbar">
      <label class="catalog-search">
        <Search :size="16" aria-hidden="true" />
        <span class="sr-only">{{ t("roleImages.search") }}</span>
        <input
          v-model="query"
          type="search"
          maxlength="128"
          :placeholder="t('roleImages.search')"
        />
      </label>
      <label class="catalog-filter">
        <span>{{ t("common.status") }}</span>
        <select v-model="state">
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
      <button
        v-if="!expanded"
        type="button"
        class="icon-button"
        :title="t('catalog.expand')"
        :aria-label="t('catalog.expand')"
        @click="expanded = true"
      >
        <Maximize2 :size="18" />
      </button>
      <RouterLink
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
      class="role-image-catalog__scroll"
      :aria-busy="store.loadingCatalog || store.loadingMore"
      @scroll="onScroll"
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
      <div v-else class="role-image-grid">
        <article v-for="recipe in items" :key="recipe.ref" class="image-card">
          <header>
            <span class="image-card__icon"><Box :size="20" /></span>
            <div>
              <h2>{{ recipe.name }}</h2>
              <p>
                {{
                  store.roleDefinitionByRef.get(recipe.roleDefinitionRef)
                    ?.label ?? t("roleImages.unknownRole")
                }}
              </p>
            </div>
            <StatusBadge :state="recipe.state" />
          </header>
          <dl>
            <div>
              <dt>{{ t("roleImages.generation") }}</dt>
              <dd>{{ recipe.generation }}</dd>
            </div>
            <div>
              <dt>{{ t("roleImages.environment") }}</dt>
              <dd>
                {{
                  store.environmentByKey.get(recipe.environment.environmentKey)
                    ? t(
                        store.environmentByKey.get(
                          recipe.environment.environmentKey,
                        )!.nameMessageKey,
                      )
                    : recipe.environment.environmentKey
                }}
              </dd>
            </div>
            <div>
              <dt>{{ t("roleImages.promotion") }}</dt>
              <dd>
                <StatusBadge
                  :state="recipe.promotedImageReady ? 'PROMOTED' : 'PENDING'"
                  :label="
                    recipe.promotedImageReady
                      ? t('roleImages.promoted')
                      : t('roleImages.notPromoted')
                  "
                />
              </dd>
            </div>
          </dl>
          <RoleImageLineage :lineage="recipe.managedLineage" />
          <footer>
            <span>
              {{
                t("roleImages.updated", {
                  date: new Date(recipe.updatedAt).toLocaleString(),
                })
              }}
            </span>
            <RouterLink
              class="button"
              :to="`/projects/${encodeURIComponent(projectRef)}/role-images/${encodeURIComponent(recipe.ref)}`"
            >
              {{ t("common.open") }}
            </RouterLink>
          </footer>
        </article>
      </div>
      <p v-if="store.loadingMore" class="catalog-loading" role="status">
        {{ t("common.loading") }}
      </p>
      <button
        v-else-if="store.projectNextPageToken[projectRef]"
        class="button catalog-more"
        type="button"
        @click="store.loadCatalog(projectRef, false)"
      >
        <Layers3 :size="16" aria-hidden="true" />
        {{ t("roleImages.loadMore") }}
      </button>
    </div>
  </component>
</template>

<style scoped>
.role-image-catalog {
  overflow: hidden;
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
  max-height: 1696px;
  padding: 14px;
  overflow: auto;
}
.role-image-catalog--expanded .role-image-catalog__scroll {
  max-height: calc(100dvh - 240px);
}
.role-image-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 360px), 1fr));
  grid-auto-rows: minmax(340px, auto);
  gap: 12px;
}
.image-card {
  display: grid;
  gap: 16px;
  padding: 16px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.image-card > header {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
}
.image-card__icon {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  border-radius: 7px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}
.image-card h2,
.image-card p {
  margin: 0;
}
.image-card h2 {
  font-size: 1rem;
  overflow-wrap: anywhere;
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.image-card p,
.image-card footer > span {
  color: var(--text-secondary);
  font-size: 0.8rem;
}
.image-card dl {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
  margin: 0;
}
.image-card dl > div {
  min-width: 0;
}
.image-card dt {
  color: var(--text-secondary);
  font-size: 0.75rem;
}
.image-card dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.image-card footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
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
.catalog-loading,
.catalog-more {
  margin: 16px auto 0;
}
.catalog-more {
  display: flex;
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
@media (max-width: 520px) {
  .role-image-grid {
    grid-template-columns: minmax(0, 1fr);
    grid-auto-rows: minmax(440px, auto);
  }
  .image-card dl {
    grid-template-columns: 1fr;
  }
  .image-card > header {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .image-card > header > :last-child {
    grid-column: 2;
    justify-self: start;
  }
  .image-card footer {
    flex-wrap: wrap;
  }
}
</style>
