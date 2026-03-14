<template>
  <button class="mission-card" :class="{ 'mission-card--selected': selected }" type="button" @click="$emit('select')">
    <div class="mission-card__topline">
      <VChip size="x-small" variant="tonal" :color="stateColor">
        {{ t(kindLabelKey) }}
      </VChip>
      <VChip size="x-small" variant="tonal" :color="syncColor">
        {{ t(syncLabelKey) }}
      </VChip>
    </div>

    <div class="mission-card__title">
      {{ entity.title }}
    </div>

    <div class="mission-card__meta">
      <div v-if="entity.primary_actor?.display_name" class="mission-card__meta-row">
        <VIcon icon="mdi-account-circle-outline" size="16" />
        <span>{{ entity.primary_actor.display_name }}</span>
      </div>
      <div class="mission-card__meta-row">
        <VIcon icon="mdi-source-repository" size="16" />
        <span class="mono">{{ entity.provider_reference.external_id }}</span>
      </div>
      <div class="mission-card__meta-row">
        <VIcon icon="mdi-link-variant" size="16" />
        <span>{{ t("pages.missionControl.card.relations", { count: entity.relation_count }) }}</span>
      </div>
      <div class="mission-card__meta-row">
        <VIcon icon="mdi-clock-outline" size="16" />
        <span class="mono">{{ formatCompactDateTime(entity.last_timeline_at, locale) }}</span>
      </div>
    </div>

    <div v-if="entity.badges.length" class="mission-card__badges">
      <VChip
        v-for="badge in entity.badges"
        :key="badge"
        size="x-small"
        variant="outlined"
        class="mission-card__badge"
      >
        {{ t(missionControlBadgeLabelKey(badge)) }}
      </VChip>
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { formatCompactDateTime } from "../../shared/lib/datetime";
import { missionControlBadgeLabelKey, missionControlEntityKindLabelKey, missionControlStateColor, missionControlSyncStatusColor, missionControlSyncStatusLabelKey } from "./presenters";
import type { MissionControlEntityCard } from "./types";

const props = defineProps<{
  entity: MissionControlEntityCard;
  selected: boolean;
  locale: string;
}>();

defineEmits<{
  select: [];
}>();

const { t } = useI18n({ useScope: "global" });

const stateColor = computed(() => missionControlStateColor(props.entity.state));
const syncColor = computed(() => missionControlSyncStatusColor(props.entity.sync_status));
const kindLabelKey = computed(() => missionControlEntityKindLabelKey(props.entity.entity_kind));
const syncLabelKey = computed(() => missionControlSyncStatusLabelKey(props.entity.sync_status));
</script>

<style scoped>
.mission-card {
  width: 100%;
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 20px;
  padding: 16px;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(246, 248, 252, 0.96)),
    radial-gradient(220px 120px at 100% 0%, rgba(255, 226, 183, 0.42), transparent 65%);
  text-align: left;
  cursor: pointer;
  transition: transform 0.18s ease, box-shadow 0.18s ease, border-color 0.18s ease;
}

.mission-card:hover {
  transform: translateY(-2px);
  border-color: rgba(15, 23, 42, 0.14);
  box-shadow: 0 18px 30px rgba(15, 23, 42, 0.08);
}

.mission-card--selected {
  border-color: rgba(25, 118, 210, 0.42);
  box-shadow: 0 0 0 2px rgba(25, 118, 210, 0.08), 0 18px 30px rgba(15, 23, 42, 0.08);
}

.mission-card__topline {
  display: flex;
  gap: 8px;
  justify-content: space-between;
  flex-wrap: wrap;
}

.mission-card__title {
  margin-top: 12px;
  font-size: 1rem;
  font-weight: 700;
  line-height: 1.35;
  color: rgb(15, 23, 42);
}

.mission-card__meta {
  display: grid;
  gap: 8px;
  margin-top: 14px;
  color: rgba(15, 23, 42, 0.72);
}

.mission-card__meta-row {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 0.92rem;
}

.mission-card__badges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.mission-card__badge {
  border-color: rgba(15, 23, 42, 0.12);
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
