<template>
  <div class="prototype-drawer">
    <template v-if="drawer">
      <div class="prototype-drawer__header">
        <div>
          <div class="prototype-drawer__eyebrow">{{ drawer.initiativeLabel }}</div>
          <div class="prototype-drawer__title">{{ drawer.title }}</div>
          <div class="prototype-drawer__meta">
            <VChip size="small" variant="tonal" :color="kindTone">
              {{ t(`pages.missionControlPrototype.nodeKinds.${drawer.nodeKind}`) }}
            </VChip>
            <VChip size="small" variant="tonal" :color="stateTone">
              {{ t(`pages.missionControlPrototype.nodeStates.${drawer.state}`) }}
            </VChip>
            <VChip size="small" variant="outlined">
              {{ drawer.stageLabel }}
            </VChip>
          </div>
        </div>

        <VBtn icon="mdi-close" variant="text" @click="emit('close')" />
      </div>

      <VTabs :model-value="tab" density="comfortable" color="primary" @update:model-value="emitTab">
        <VTab value="details">{{ t("pages.missionControlPrototype.tabs.details") }}</VTab>
        <VTab value="timeline">{{ t("pages.missionControlPrototype.tabs.timeline") }}</VTab>
        <VTab value="workflow">{{ t("pages.missionControlPrototype.tabs.workflow") }}</VTab>
      </VTabs>

      <VWindow :model-value="tab" class="prototype-drawer__window">
        <VWindowItem value="details">
          <div class="prototype-drawer__section">
            <div class="prototype-drawer__markdown">{{ drawer.overviewMarkdown }}</div>

            <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.drawer.safeActions") }}</div>
            <div class="prototype-drawer__actions">
              <template v-for="action in drawer.safeActions" :key="action.actionId">
                <AdaptiveBtn
                  v-if="action.kind === 'preview'"
                  variant="tonal"
                  :icon="action.icon"
                  :label="action.label"
                  @click="emit('updateTab', 'workflow')"
                />
                <AdaptiveBtn
                  v-else
                  variant="outlined"
                  :icon="action.icon"
                  :label="action.label"
                  :href="action.href"
                  target="_blank"
                  rel="noreferrer"
                />
              </template>
            </div>

            <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.drawer.relatedNodes") }}</div>
            <div class="prototype-drawer__related">
              <VBtn
                v-for="related in drawer.relatedNodes"
                :key="related.nodeId"
                variant="text"
                class="prototype-drawer__related-button"
                @click="emit('selectNode', related.nodeId)"
              >
                <span class="prototype-drawer__related-kind">
                  {{ t(`pages.missionControlPrototype.nodeKinds.${related.nodeKind}`) }}
                </span>
                <span class="prototype-drawer__related-title">{{ related.title }}</span>
              </VBtn>
            </div>

            <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.drawer.sourceRefs") }}</div>
            <div class="prototype-drawer__refs">
              <VChip v-for="sourceRef in mergedSourceRefs" :key="sourceRef" size="small" variant="outlined">
                {{ sourceRef }}
              </VChip>
            </div>
          </div>
        </VWindowItem>

        <VWindowItem value="timeline">
          <div class="prototype-drawer__section">
            <div v-if="drawer.timelineItems.length === 0" class="prototype-drawer__empty">
              {{ t("pages.missionControlPrototype.drawer.emptyTimeline") }}
            </div>
            <div v-for="item in drawer.timelineItems" :key="item.itemId" class="prototype-drawer__timeline-item">
              <div class="prototype-drawer__timeline-head">
                <VChip size="x-small" variant="tonal" :color="timelineTone(item.tone)">
                  {{ formatCompactDateTime(item.happenedAt, locale) }}
                </VChip>
                <span class="prototype-drawer__timeline-title">{{ item.title }}</span>
              </div>
              <div class="prototype-drawer__timeline-summary">{{ item.summary }}</div>
              <a v-if="item.sourceRef" :href="item.sourceRef" target="_blank" rel="noreferrer" class="prototype-drawer__timeline-link">
                {{ t("pages.missionControlPrototype.drawer.openSource") }}
              </a>
            </div>
          </div>
        </VWindowItem>

        <VWindowItem value="workflow">
          <div class="prototype-drawer__section">
            <div class="prototype-drawer__workflow-grid">
              <VSelect
                :model-value="activeWorkflowPresetId"
                density="comfortable"
                variant="outlined"
                hide-details
                :label="t('pages.missionControlPrototype.workflow.preset')"
                :items="presetItems"
                @update:model-value="emitPreset"
              />

              <VSelect
                :model-value="workflowDraft?.stageSequenceVariant"
                density="comfortable"
                variant="outlined"
                hide-details
                :label="t('pages.missionControlPrototype.workflow.stageSequence')"
                :items="stageSequenceItems"
                :disabled="!workflowDraft"
                @update:model-value="emitDraft('stageSequenceVariant', $event)"
              />

              <VSelect
                :model-value="workflowDraft?.autoReviewPolicy"
                density="comfortable"
                variant="outlined"
                hide-details
                :label="t('pages.missionControlPrototype.workflow.autoReview')"
                :items="autoReviewItems"
                :disabled="!workflowDraft"
                @update:model-value="emitDraft('autoReviewPolicy', $event)"
              />

              <VSelect
                :model-value="workflowDraft?.followUpPolicy"
                density="comfortable"
                variant="outlined"
                hide-details
                :label="t('pages.missionControlPrototype.workflow.followUp')"
                :items="followUpItems"
                :disabled="!workflowDraft"
                @update:model-value="emitDraft('followUpPolicy', $event)"
              />

              <VSelect
                :model-value="workflowDraft?.safeActionProfile"
                density="comfortable"
                variant="outlined"
                hide-details
                :label="t('pages.missionControlPrototype.workflow.safeActions')"
                :items="safeActionItems"
                :disabled="!workflowDraft"
                @update:model-value="emitDraft('safeActionProfile', $event)"
              />
            </div>

            <VAlert v-if="workflowLoading" type="info" variant="tonal" class="mt-4">
              {{ t("common.loading") }}
            </VAlert>

            <template v-if="workflowPreview">
              <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.workflow.generatedBlock") }}</div>
              <pre class="prototype-drawer__code">{{ workflowPreview.generatedBlockMarkdown }}</pre>

              <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.workflow.changes") }}</div>
              <div class="prototype-drawer__refs">
                <VChip v-for="change in workflowPreview.changeExplanations" :key="change" size="small" variant="tonal">
                  {{ change }}
                </VChip>
              </div>

              <div v-if="workflowPreview.warnings.length > 0" class="prototype-drawer__subheading">
                {{ t("pages.missionControlPrototype.workflow.warnings") }}
              </div>
              <div class="prototype-drawer__refs">
                <VChip
                  v-for="warning in workflowPreview.warnings"
                  :key="warning"
                  size="small"
                  color="warning"
                  variant="tonal"
                >
                  {{ warning }}
                </VChip>
              </div>

              <div class="prototype-drawer__subheading">{{ t("pages.missionControlPrototype.workflow.promptSources") }}</div>
              <div class="prototype-drawer__refs">
                <VChip v-for="sourceRef in workflowPreview.sourceRefs" :key="sourceRef" size="small" variant="outlined">
                  {{ sourceRef }}
                </VChip>
              </div>
            </template>
          </div>
        </VWindowItem>
      </VWindow>
    </template>

    <div v-else class="prototype-drawer__empty-shell">
      <VIcon icon="mdi-vector-polyline" size="42" class="mb-4 text-medium-emphasis" />
      <div class="text-h6">{{ t("pages.missionControlPrototype.drawer.emptyTitle") }}</div>
      <div class="text-body-2 text-medium-emphasis mt-2">
        {{ t("pages.missionControlPrototype.drawer.emptyText") }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import { formatCompactDateTime } from "../../../shared/lib/datetime";
import AdaptiveBtn from "../../../shared/ui/AdaptiveBtn.vue";
import { missionControlPrototypeStateTone } from "./presenters";
import type {
  MissionCanvasNodeKind,
  MissionControlPrototypeError,
  MissionDrawerTab,
  MissionDrawerView,
  MissionTimelineItemTone,
  MissionWorkflowAutoReviewPolicy,
  MissionWorkflowDraft,
  MissionWorkflowFollowUpPolicy,
  MissionWorkflowPreviewResult,
  MissionWorkflowPresetOption,
  MissionWorkflowSafeActionProfile,
  MissionWorkflowStageSequenceVariant,
} from "./types";

const props = defineProps<{
  drawer: MissionDrawerView | null;
  tab: MissionDrawerTab;
  drawerLoading: boolean;
  workflowLoading: boolean;
  scenarioSourceRefs: string[];
  workflowPresetOptions: MissionWorkflowPresetOption[];
  activeWorkflowPresetId: string | null;
  workflowDraft: MissionWorkflowDraft | null;
  workflowPreview: MissionWorkflowPreviewResult | null;
  error: MissionControlPrototypeError | null;
}>();

const emit = defineEmits<{
  close: [];
  updateTab: [tab: MissionDrawerTab];
  selectNode: [nodeId: string];
  selectWorkflowPreset: [presetId: string];
  patchWorkflowDraft: [patch: Partial<MissionWorkflowDraft>];
}>();

const { t, locale } = useI18n({ useScope: "global" });

const mergedSourceRefs = computed(() =>
  Array.from(new Set([...(props.drawer?.sourceRefs ?? []), ...props.scenarioSourceRefs])),
);

const presetItems = computed(() =>
  props.workflowPresetOptions.map((preset) => ({
    title: `${preset.label} · ${preset.summary}`,
    value: preset.presetId,
  })),
);

const stageSequenceItems = computed(() => [
  { title: t("pages.missionControlPrototype.workflow.sequenceVariants.full_delivery"), value: "full_delivery" },
  { title: t("pages.missionControlPrototype.workflow.sequenceVariants.owner_demo"), value: "owner_demo" },
  { title: t("pages.missionControlPrototype.workflow.sequenceVariants.revise_loop"), value: "revise_loop" },
]);

const autoReviewItems = computed(() => [
  { title: t("pages.missionControlPrototype.workflow.autoReviewOptions.required"), value: "required" },
  { title: t("pages.missionControlPrototype.workflow.autoReviewOptions.owner_only"), value: "owner_only" },
  { title: t("pages.missionControlPrototype.workflow.autoReviewOptions.paired_reviewer"), value: "paired_reviewer" },
]);

const followUpItems = computed(() => [
  { title: t("pages.missionControlPrototype.workflow.followUpOptions.carry_to_next_stage"), value: "carry_to_next_stage" },
  { title: t("pages.missionControlPrototype.workflow.followUpOptions.spawn_issue_on_gap"), value: "spawn_issue_on_gap" },
  { title: t("pages.missionControlPrototype.workflow.followUpOptions.owner_managed"), value: "owner_managed" },
]);

const safeActionItems = computed(() => [
  { title: t("pages.missionControlPrototype.workflow.safeActionProfiles.preview_only"), value: "preview_only" },
  { title: t("pages.missionControlPrototype.workflow.safeActionProfiles.github_links_only"), value: "github_links_only" },
  { title: t("pages.missionControlPrototype.workflow.safeActionProfiles.candidate_readonly"), value: "candidate_readonly" },
]);

const kindTone = computed(() => nodeKindTone(props.drawer?.nodeKind ?? "Issue"));
const stateTone = computed(() => missionControlPrototypeStateTone(props.drawer?.state ?? "working"));

function emitTab(value: string | null): void {
  if (value === "details" || value === "timeline" || value === "workflow") {
    emit("updateTab", value);
  }
}

function emitPreset(value: string | null): void {
  if (typeof value === "string" && value.trim() !== "") {
    emit("selectWorkflowPreset", value);
  }
}

function emitDraft<K extends keyof MissionWorkflowDraft>(key: K, value: unknown): void {
  if (typeof value !== "string" || value.trim() === "") {
    return;
  }

  emit("patchWorkflowDraft", { [key]: value } as Partial<MissionWorkflowDraft>);
}

function nodeKindTone(kind: MissionCanvasNodeKind): string {
  switch (kind) {
    case "Issue":
      return "warning";
    case "PR":
      return "success";
    case "Run":
      return "info";
  }
}

function timelineTone(tone: MissionTimelineItemTone): string {
  switch (tone) {
    case "positive":
      return "success";
    case "warning":
      return "warning";
    case "attention":
      return "error";
    case "neutral":
      return "secondary";
  }
}
</script>

<style scoped>
.prototype-drawer {
  display: flex;
  flex-direction: column;
  min-height: 100%;
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.98), rgba(248, 250, 252, 0.98)),
    radial-gradient(circle at top right, rgba(13, 148, 136, 0.08), transparent 30%);
}

.prototype-drawer__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 20px 20px 16px;
}

.prototype-drawer__eyebrow {
  font-size: 0.78rem;
  font-weight: 700;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: rgb(100, 116, 139);
}

.prototype-drawer__title {
  margin-top: 6px;
  font-size: 1.15rem;
  font-weight: 800;
  line-height: 1.35;
  color: rgb(15, 23, 42);
}

.prototype-drawer__meta {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-top: 12px;
}

.prototype-drawer__window {
  flex: 1;
}

.prototype-drawer__section {
  padding: 18px 20px 24px;
}

.prototype-drawer__markdown {
  white-space: pre-line;
  font-size: 0.94rem;
  line-height: 1.7;
  color: rgb(30, 41, 59);
}

.prototype-drawer__subheading {
  margin-top: 22px;
  margin-bottom: 10px;
  font-size: 0.78rem;
  font-weight: 800;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: rgb(100, 116, 139);
}

.prototype-drawer__actions,
.prototype-drawer__refs,
.prototype-drawer__related {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.prototype-drawer__related-button {
  justify-content: flex-start;
  max-width: 100%;
  text-transform: none;
}

.prototype-drawer__related-kind {
  margin-right: 8px;
  color: rgb(14, 116, 144);
  font-weight: 700;
}

.prototype-drawer__related-title {
  color: rgb(30, 41, 59);
}

.prototype-drawer__timeline-item {
  position: relative;
  padding-left: 18px;
  margin-bottom: 18px;
  border-left: 2px solid rgba(148, 163, 184, 0.3);
}

.prototype-drawer__timeline-item::before {
  content: "";
  position: absolute;
  left: -6px;
  top: 6px;
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: rgb(14, 116, 144);
}

.prototype-drawer__timeline-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.prototype-drawer__timeline-title {
  font-weight: 700;
  color: rgb(15, 23, 42);
}

.prototype-drawer__timeline-summary {
  margin-top: 8px;
  color: rgb(51, 65, 85);
  line-height: 1.6;
}

.prototype-drawer__timeline-link {
  display: inline-flex;
  margin-top: 8px;
  color: rgb(14, 116, 144);
  text-decoration: none;
  font-weight: 700;
}

.prototype-drawer__workflow-grid {
  display: grid;
  gap: 12px;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
}

.prototype-drawer__code {
  overflow: auto;
  padding: 16px;
  border-radius: 18px;
  background: rgb(15, 23, 42);
  color: rgb(226, 232, 240);
  font-size: 0.84rem;
  line-height: 1.6;
}

.prototype-drawer__empty-shell {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100%;
  padding: 32px;
  text-align: center;
}

.prototype-drawer__empty {
  color: rgb(100, 116, 139);
}
</style>
