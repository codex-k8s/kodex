<template>
  <VCard class="mission-side-panel" variant="outlined">
    <template v-if="loading">
      <VCardText class="py-8">
        <VSkeletonLoader type="article, paragraph, paragraph" />
      </VCardText>
    </template>

    <template v-else-if="error && !details">
      <VCardText>
        <VAlert type="error" variant="tonal">
          {{ t(error.messageKey) }}
        </VAlert>
      </VCardText>
    </template>

    <template v-else-if="details">
      <VCardItem>
        <template #prepend>
          <VAvatar color="primary" variant="tonal" size="42">
            <VIcon :icon="panelIcon" />
          </VAvatar>
        </template>

        <VCardTitle class="mission-side-panel__title">
          {{ details.entity.title }}
        </VCardTitle>
        <VCardSubtitle class="mission-side-panel__subtitle">
          {{ t(kindLabelKey) }}
        </VCardSubtitle>

        <template #append>
          <VBtn icon="mdi-close" variant="text" @click="$emit('close')" />
        </template>
      </VCardItem>

      <VCardText class="mission-side-panel__content">
        <div class="d-flex flex-wrap ga-2">
          <VChip size="small" variant="tonal" :color="stateColor">
            {{ t(stateLabelKey) }}
          </VChip>
          <VChip size="small" variant="tonal" :color="syncColor">
            {{ t(syncLabelKey) }}
          </VChip>
          <VChip v-if="details.entity.primary_actor?.display_name" size="small" variant="tonal">
            {{ details.entity.primary_actor.display_name }}
          </VChip>
        </div>

        <VCard class="mission-side-panel__section" color="surface" variant="tonal">
          <VCardTitle class="text-subtitle-2">{{ t("pages.missionControl.sidePanel.overview") }}</VCardTitle>
          <VCardText class="mission-side-panel__rows">
            <div v-for="row in infoRows" :key="row.labelKey + ':' + row.value" class="mission-side-panel__row">
              <div class="mission-side-panel__label">{{ t(row.labelKey) }}</div>
              <a
                v-if="row.href"
                class="mission-side-panel__value mission-side-panel__value--link"
                :class="{ mono: row.mono }"
                :href="row.href"
                target="_blank"
                rel="noreferrer"
              >
                {{ row.value }}
              </a>
              <div v-else class="mission-side-panel__value" :class="{ mono: row.mono }">
                {{ row.value }}
              </div>
            </div>

            <div v-if="workItemLabels.length" class="mission-side-panel__row">
              <div class="mission-side-panel__label">{{ t("pages.missionControl.sidePanel.labels") }}</div>
              <div class="d-flex flex-wrap ga-2">
                <VChip v-for="label in workItemLabels" :key="label" size="x-small" variant="outlined">
                  {{ label }}
                </VChip>
              </div>
            </div>

            <div v-if="workItemAssignees.length" class="mission-side-panel__row">
              <div class="mission-side-panel__label">{{ t("pages.missionControl.sidePanel.assignees") }}</div>
              <div class="d-flex flex-wrap ga-2">
                <VChip v-for="assignee in workItemAssignees" :key="assignee" size="x-small" variant="outlined">
                  {{ assignee }}
                </VChip>
              </div>
            </div>

            <div v-if="pullRequestLinkedIssues.length" class="mission-side-panel__row">
              <div class="mission-side-panel__label">{{ t("pages.missionControl.sidePanel.linkedIssues") }}</div>
              <div class="d-flex flex-wrap ga-2">
                <VChip v-for="ref in pullRequestLinkedIssues" :key="ref" size="x-small" variant="outlined">
                  {{ ref }}
                </VChip>
              </div>
            </div>

            <div v-if="discussionLatestComment" class="mission-side-panel__row">
              <div class="mission-side-panel__label">{{ t("pages.missionControl.sidePanel.latestComment") }}</div>
              <div class="mission-side-panel__value">
                {{ discussionLatestComment }}
              </div>
            </div>
          </VCardText>
        </VCard>

        <VCard class="mission-side-panel__section" color="surface" variant="tonal">
          <VCardTitle class="text-subtitle-2">{{ t("pages.missionControl.sidePanel.relations") }}</VCardTitle>
          <VCardText>
            <div v-if="details.relations.length" class="d-flex flex-wrap ga-2">
              <VChip
                v-for="relation in details.relations"
                :key="relation.relation_kind + relation.source_entity_public_id + relation.target_entity_public_id"
                size="small"
                variant="outlined"
                class="mission-side-panel__relation"
                @click="$emit('selectRelation', relation)"
              >
                {{ relation.relation_kind }} · {{ relation.source_entity_public_id === details.entity.entity_public_id ? relation.target_entity_public_id : relation.source_entity_public_id }}
              </VChip>
            </div>
            <div v-else class="text-medium-emphasis">
              {{ t("pages.missionControl.sidePanel.noRelations") }}
            </div>
          </VCardText>
        </VCard>

        <VCard class="mission-side-panel__section" color="surface" variant="tonal">
          <VCardTitle class="text-subtitle-2 d-flex align-center justify-space-between ga-2">
            <span>{{ t("pages.missionControl.sidePanel.timeline") }}</span>
            <VProgressCircular v-if="timelineLoading" indeterminate size="18" width="2" />
          </VCardTitle>
          <VCardText>
            <VAlert v-if="timelineError" type="error" variant="tonal" class="mb-4">
              {{ t(timelineError.messageKey) }}
            </VAlert>
            <div v-if="timeline.length" class="mission-side-panel__timeline">
              <div v-for="entry in timeline" :key="entry.entry_id" class="mission-side-panel__timeline-entry">
                <div class="mission-side-panel__timeline-head">
                  <VChip size="x-small" variant="tonal">
                    {{ entry.source_kind }}
                  </VChip>
                  <span class="mono text-medium-emphasis">{{ formatCompactDateTime(entry.occurred_at, locale) }}</span>
                </div>
                <div class="mission-side-panel__timeline-summary">{{ entry.summary }}</div>
                <div v-if="entry.body_markdown" class="mission-side-panel__timeline-body">
                  {{ entry.body_markdown }}
                </div>
                <a
                  v-if="entry.provider_url"
                  class="mission-side-panel__value--link"
                  :href="entry.provider_url"
                  target="_blank"
                  rel="noreferrer"
                >
                  {{ t("pages.missionControl.sidePanel.openProviderEvent") }}
                </a>
              </div>
            </div>
            <div v-else class="text-medium-emphasis">
              {{ t("pages.missionControl.sidePanel.noTimeline") }}
            </div>
            <div v-if="hasMoreTimeline" class="mt-4">
              <AdaptiveBtn
                variant="text"
                icon="mdi-chevron-down"
                :label="t('pages.missionControl.sidePanel.loadMoreTimeline')"
                :loading="timelineLoading"
                @click="$emit('loadMoreTimeline')"
              />
            </div>
          </VCardText>
        </VCard>

        <VCard class="mission-side-panel__section" color="surface" variant="tonal">
          <VCardTitle class="text-subtitle-2">{{ t("pages.missionControl.sidePanel.allowedActions") }}</VCardTitle>
          <VCardText class="mission-side-panel__actions">
            <div v-if="details.allowed_actions.length" class="d-flex flex-column ga-3">
              <VSheet
                v-for="action in details.allowed_actions"
                :key="action.action_kind"
                class="mission-side-panel__action"
                rounded="xl"
                border
              >
                <div class="d-flex align-start ga-3">
                  <VAvatar :color="actionPresentation(action.action_kind).color" variant="tonal" size="36">
                    <VIcon :icon="actionPresentation(action.action_kind).icon" size="18" />
                  </VAvatar>
                  <div class="flex-grow-1">
                    <div class="font-weight-medium">{{ t(missionControlActionLabelKey(action.action_kind)) }}</div>
                    <div class="text-body-2 text-medium-emphasis mt-1">
                      {{
                        action.approval_requirement === "owner_review"
                          ? t("pages.missionControl.sidePanel.ownerReviewRequired")
                          : t("pages.missionControl.sidePanel.noApprovalRequired")
                      }}
                    </div>
                    <div v-if="action.blocked_reason" class="text-body-2 text-error mt-2">
                      {{ action.blocked_reason }}
                    </div>
                  </div>
                </div>
              </VSheet>
            </div>
            <div v-else class="text-medium-emphasis">
              {{ t("pages.missionControl.sidePanel.noActions") }}
            </div>
          </VCardText>
        </VCard>

        <VCard class="mission-side-panel__section" color="surface" variant="tonal">
          <VCardTitle class="text-subtitle-2">{{ t("pages.missionControl.sidePanel.deepLinks") }}</VCardTitle>
          <VCardText>
            <div v-if="details.provider_deep_links.length" class="d-flex flex-wrap ga-2">
              <VBtn
                v-for="link in details.provider_deep_links"
                :key="link.action_kind + ':' + link.url"
                :color="deepLinkPresentation(link.action_kind).color"
                variant="tonal"
                :href="link.url"
                target="_blank"
                rel="noreferrer"
                :prepend-icon="deepLinkPresentation(link.action_kind).icon"
              >
                {{ t(deepLinkPresentation(link.action_kind).labelKey) }}
              </VBtn>
            </div>
            <div v-else class="text-medium-emphasis">
              {{ t("pages.missionControl.sidePanel.noDeepLinks") }}
            </div>
          </VCardText>
        </VCard>
      </VCardText>
    </template>

    <template v-else>
      <VCardText class="py-10 text-center text-medium-emphasis">
        {{ t("pages.missionControl.sidePanel.placeholder") }}
      </VCardText>
    </template>
  </VCard>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { formatCompactDateTime } from "../../shared/lib/datetime";
import AdaptiveBtn from "../../shared/ui/AdaptiveBtn.vue";
import { buildMissionControlInfoRows, missionControlActionLabelKey, missionControlActionPresentation, missionControlDeepLinkPresentation, missionControlDiscussionLatestComment, missionControlEntityKindLabelKey, missionControlPullRequestLinkedIssues, missionControlStateColor, missionControlStateLabelKey, missionControlSyncStatusColor, missionControlSyncStatusLabelKey, missionControlWorkItemAssignees, missionControlWorkItemLabels } from "./presenters";
import type { ApiError } from "../../shared/api/errors";
import type { MissionControlEntityDetails, MissionControlRelation, MissionControlTimelineEntry } from "./types";

const props = defineProps<{
  details: MissionControlEntityDetails | null;
  loading: boolean;
  error: ApiError | null;
  timeline: MissionControlTimelineEntry[];
  timelineError: ApiError | null;
  timelineLoading: boolean;
  hasMoreTimeline: boolean;
  locale: string;
}>();

defineEmits<{
  close: [];
  selectRelation: [relation: MissionControlRelation];
  loadMoreTimeline: [];
}>();

const { t } = useI18n({ useScope: "global" });

const panelIcon = computed(() => {
  switch (props.details?.entity.entity_kind) {
    case "work_item":
      return "mdi-briefcase-outline";
    case "discussion":
      return "mdi-message-text-outline";
    case "pull_request":
      return "mdi-source-pull";
    case "agent":
      return "mdi-robot-outline";
    default:
      return "mdi-radar";
  }
});

const kindLabelKey = computed(() => (props.details ? missionControlEntityKindLabelKey(props.details.entity.entity_kind) : ""));
const stateLabelKey = computed(() => (props.details ? missionControlStateLabelKey(props.details.entity.state) : ""));
const syncLabelKey = computed(() => (props.details ? missionControlSyncStatusLabelKey(props.details.entity.sync_status) : ""));
const stateColor = computed(() => (props.details ? missionControlStateColor(props.details.entity.state) : "secondary"));
const syncColor = computed(() => (props.details ? missionControlSyncStatusColor(props.details.entity.sync_status) : "secondary"));
const infoRows = computed(() => (props.details ? buildMissionControlInfoRows(props.details) : []));
const workItemLabels = computed(() => (props.details ? missionControlWorkItemLabels(props.details) : []));
const workItemAssignees = computed(() => (props.details ? missionControlWorkItemAssignees(props.details) : []));
const pullRequestLinkedIssues = computed(() => (props.details ? missionControlPullRequestLinkedIssues(props.details) : []));
const discussionLatestComment = computed(() => (props.details ? missionControlDiscussionLatestComment(props.details) : ""));

function actionPresentation(actionKind: string) {
  return missionControlActionPresentation(actionKind);
}

function deepLinkPresentation(actionKind: string) {
  return missionControlDeepLinkPresentation(actionKind);
}
</script>

<style scoped>
.mission-side-panel {
  border-radius: 28px;
  overflow: hidden;
}

.mission-side-panel__title {
  white-space: normal;
  line-height: 1.35;
}

.mission-side-panel__subtitle {
  margin-top: 2px;
}

.mission-side-panel__content {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mission-side-panel__section {
  border-radius: 22px;
}

.mission-side-panel__rows {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mission-side-panel__row {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.mission-side-panel__label {
  font-size: 0.8rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: rgba(15, 23, 42, 0.56);
}

.mission-side-panel__value {
  color: rgb(15, 23, 42);
  word-break: break-word;
}

.mission-side-panel__value--link {
  color: rgb(25, 118, 210);
  text-decoration: none;
}

.mission-side-panel__relation {
  cursor: pointer;
}

.mission-side-panel__timeline {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.mission-side-panel__timeline-entry {
  border-left: 2px solid rgba(25, 118, 210, 0.16);
  padding-left: 12px;
}

.mission-side-panel__timeline-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 8px;
}

.mission-side-panel__timeline-summary {
  font-weight: 600;
  color: rgb(15, 23, 42);
}

.mission-side-panel__timeline-body {
  margin-top: 6px;
  color: rgba(15, 23, 42, 0.72);
  white-space: pre-wrap;
}

.mission-side-panel__actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mission-side-panel__action {
  padding: 12px;
  background: rgba(255, 255, 255, 0.72);
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}
</style>
