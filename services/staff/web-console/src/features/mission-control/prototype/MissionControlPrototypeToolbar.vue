<template>
  <div class="prototype-toolbar">
    <div class="prototype-toolbar__row">
      <VSelect
        :model-value="activeScenarioId"
        class="prototype-toolbar__scenario"
        density="comfortable"
        variant="outlined"
        hide-details
        :label="t('pages.missionControlPrototype.toolbar.scenario')"
        :items="catalogItems"
        @update:model-value="emitScenario"
      />

      <VTextField
        :model-value="search"
        class="prototype-toolbar__search"
        density="comfortable"
        variant="outlined"
        hide-details
        clearable
        prepend-inner-icon="mdi-magnify"
        :label="t('pages.missionControlPrototype.toolbar.search')"
        @update:model-value="emitSearch"
        @click:clear="emit('updateSearch', '')"
      />

      <div class="prototype-toolbar__controls">
        <AdaptiveBtn variant="text" icon="mdi-magnify-minus-outline" :label="t('pages.missionControlPrototype.toolbar.zoomOut')" @click="emit('zoomOut')" />
        <div class="prototype-toolbar__zoom">{{ zoomLabel }}</div>
        <AdaptiveBtn variant="text" icon="mdi-magnify-plus-outline" :label="t('pages.missionControlPrototype.toolbar.zoomIn')" @click="emit('zoomIn')" />
        <AdaptiveBtn variant="text" icon="mdi-fit-to-screen-outline" :label="t('pages.missionControlPrototype.toolbar.fit')" @click="emit('fit')" />
        <AdaptiveBtn variant="text" icon="mdi-refresh" :label="t('common.reset')" @click="emit('reset')" />
      </div>
    </div>

    <div class="prototype-toolbar__row prototype-toolbar__row--secondary">
      <div class="prototype-toolbar__focus">
        <div class="prototype-toolbar__label">{{ t("pages.missionControlPrototype.toolbar.initiatives") }}</div>
        <div class="prototype-toolbar__chips">
          <VChip
            :variant="focusedInitiativeId ? 'outlined' : 'flat'"
            :color="focusedInitiativeId ? undefined : 'primary'"
            size="small"
            @click="emit('clearInitiative')"
          >
            {{ t("pages.missionControlPrototype.toolbar.allInitiatives") }}
          </VChip>
          <VChip
            v-for="initiative in initiatives"
            :key="initiative.initiativeId"
            size="small"
            :variant="initiative.focused ? 'flat' : 'outlined'"
            :color="initiative.focused ? 'primary' : undefined"
            :class="{ 'prototype-toolbar__chip--dimmed': initiative.dimmed }"
            @click="emit('selectInitiative', initiative.initiativeId)"
          >
            {{ initiative.label }} · {{ initiative.nodeCount }}
          </VChip>
        </div>
      </div>

      <VChip v-if="selectedNodeTitle" color="primary" variant="tonal" size="small" class="prototype-toolbar__selection">
        {{ t("pages.missionControlPrototype.toolbar.selectedNode") }}: {{ selectedNodeTitle }}
      </VChip>

      <AdaptiveBtn
        class="prototype-toolbar__workflow"
        variant="tonal"
        icon="mdi-file-tree-outline"
        :disabled="!selectedNodeTitle"
        :label="t('pages.missionControlPrototype.toolbar.workflowCta')"
        @click="emit('openWorkflow')"
      />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { MissionControlPrototypeCatalogItem, MissionInitiativeView } from "./types";
import AdaptiveBtn from "../../../shared/ui/AdaptiveBtn.vue";

const props = defineProps<{
  catalog: MissionControlPrototypeCatalogItem[];
  activeScenarioId: string;
  initiatives: MissionInitiativeView[];
  focusedInitiativeId: string | null;
  search: string;
  selectedNodeTitle: string;
  zoomLabel: string;
}>();

const emit = defineEmits<{
  selectScenario: [scenarioId: string];
  selectInitiative: [initiativeId: string];
  clearInitiative: [];
  updateSearch: [search: string];
  zoomIn: [];
  zoomOut: [];
  fit: [];
  reset: [];
  openWorkflow: [];
}>();

const { t } = useI18n({ useScope: "global" });

const catalogItems = computed(() =>
  props.catalog.map((item) => ({
    title: `${item.title} · ${item.nodeCount}`,
    value: item.scenarioId,
  })),
);

function emitScenario(value: string | null): void {
  if (typeof value === "string" && value.trim() !== "") {
    emit("selectScenario", value);
  }
}

function emitSearch(value: string | null): void {
  emit("updateSearch", typeof value === "string" ? value : "");
}
</script>

<style scoped>
.prototype-toolbar {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 18px 20px 20px;
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
  background:
    linear-gradient(180deg, rgba(255, 251, 235, 0.92), rgba(255, 255, 255, 0.96)),
    radial-gradient(circle at top right, rgba(217, 119, 6, 0.14), transparent 42%);
}

.prototype-toolbar__row {
  display: flex;
  gap: 12px;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
}

.prototype-toolbar__row--secondary {
  align-items: flex-start;
}

.prototype-toolbar__scenario {
  min-width: 320px;
  max-width: 460px;
}

.prototype-toolbar__search {
  flex: 1;
  min-width: 240px;
}

.prototype-toolbar__controls {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.prototype-toolbar__zoom {
  min-width: 62px;
  text-align: center;
  font-size: 0.88rem;
  font-weight: 700;
  color: rgb(71, 85, 105);
}

.prototype-toolbar__focus {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
  min-width: 320px;
}

.prototype-toolbar__label {
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: rgb(100, 116, 139);
}

.prototype-toolbar__chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.prototype-toolbar__chip--dimmed {
  opacity: 0.62;
}

.prototype-toolbar__selection {
  margin-top: 4px;
}

.prototype-toolbar__workflow {
  margin-left: auto;
}

@media (max-width: 960px) {
  .prototype-toolbar__workflow {
    margin-left: 0;
  }

  .prototype-toolbar__scenario {
    min-width: 100%;
  }
}
</style>
