<script setup lang="ts">
import { Bot } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import {
  buildRunTranscriptItems,
  type PresentedRunEvent,
} from "@/features/runs/run-activity";
import { indexRunSessionOwnership } from "@/features/runs/run-session-graph";
import { runNodePresentationKey } from "@/features/runs/run-owner";
import type {
  Agent,
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import RuntimeRevisionDiffPanel from "./RuntimeRevisionDiffPanel.vue";
import RunPromptPreview from "./RunPromptPreview.vue";
import RunTranscript from "./RunTranscript.vue";

const props = withDefaults(
  defineProps<{
    run: Run;
    rootRun?: Run;
    node: RunNode;
    nodes: RunNode[];
    events: PresentedRunEvent[];
    artifacts: Artifact[];
    agent?: Agent;
    executionLabel?: "ASSISTANT" | "EMPLOYEE" | "SESSION";
  }>(),
  { rootRun: undefined, agent: undefined },
);
const emit = defineEmits<{ close: []; download: [artifact: Artifact] }>();
const { locale, t } = useI18n();
const roleLabel = computed(() =>
  props.executionLabel === "ASSISTANT" || props.executionLabel === "SESSION"
    ? t(runNodePresentationKey(props.executionLabel, props.node.type))
    : props.node.role || t(`runs.nodeTypes.${props.node.type}`),
);
const revisionDiffOpen = ref(false);
const inputExpanded = ref(false);

const parentNode = computed(() =>
  props.nodes.find((candidate) => candidate.ref === props.node.parentNodeRef),
);
const sessionOwnership = computed(() => indexRunSessionOwnership(props.nodes));
const ownedNodeRefs = computed(
  () =>
    new Set(
      [...sessionOwnership.value.entries()]
        .filter(([, sessionRef]) => sessionRef === props.node.ref)
        .map(([nodeRef]) => nodeRef),
    ),
);
const nodeEvents = computed(() =>
  props.events
    .filter(
      (event) =>
        event.runRef === props.run.ref &&
        (!event.nodeRef ||
          ownedNodeRefs.value.has(event.nodeRef) ||
          !sessionOwnership.value.has(event.nodeRef)),
    )
    .sort((left, right) => left.sequence - right.sequence),
);
const transcriptItems = computed(() =>
  buildRunTranscriptItems(nodeEvents.value, {
    nodes: props.nodes.filter((node) => node.runRef === props.run.ref),
    initiator: props.run.initiator.displayName,
    target: props.node.displayName,
    platform: t("runs.platformActor"),
  }),
);
const resultInTranscript = computed(() =>
  transcriptItems.value.some(
    (item) =>
      item.phase === "FINAL" && item.summary === props.run.resultSummary,
  ),
);
const nodeArtifacts = computed(() => {
  const refs = new Set(
    props.nodes
      .filter((node) => ownedNodeRefs.value.has(node.ref))
      .flatMap((node) => node.artifactRefs),
  );
  return props.artifacts.filter((artifact) => refs.has(artifact.ref));
});
const sessionNode = computed(
  () =>
    props.node.type === "ROOT_PROCESS" || props.node.type === "AGENT_EXECUTION",
);
const usageItems = computed(() => {
  const usage = props.run.usage;
  if (usage.totalTokens === 0) {
    return usage.modelContextWindow > 0
      ? ([["contextWindow", usage.modelContextWindow]] as const)
      : [];
  }
  return [
    ["total", usage.totalTokens],
    ["input", usage.inputTokens],
    ["cached", usage.cachedInputTokens],
    ["output", usage.outputTokens],
    ["reasoning", usage.reasoningOutputTokens],
    ["contextWindow", usage.modelContextWindow],
  ] as const;
});

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString(locale.value) : "";
}

function formatTokenCount(value: number): string {
  return new Intl.NumberFormat(locale.value).format(value);
}
</script>

<template>
  <ModalDialog
    class="session-details-dialog"
    :title="node.displayName"
    size="xl"
    @close="$emit('close')"
  >
    <div class="session-details">
      <header class="session-details__summary">
        <span class="session-details__avatar">
          <img
            v-if="agent?.avatarUrl"
            :src="agent.avatarUrl"
            :alt="agent.name"
          />
          <Bot v-else :size="22" aria-hidden="true" />
        </span>
        <div>
          <small>{{
            $t(sessionNode ? "runs.sessionNode" : "runs.controlNode")
          }}</small>
          <strong>{{ roleLabel }}</strong>
          <p>
            {{
              node.progressSummary ||
              node.inputSummary ||
              $t("runs.waitingForActivity")
            }}
          </p>
        </div>
        <StatusBadge :state="node.state" />
      </header>

      <details
        v-if="sessionNode"
        @toggle="revisionDiffOpen = ($event.target as HTMLDetailsElement).open"
      >
        <summary>{{ $t("runtimeDiff.title") }}</summary>
        <RuntimeRevisionDiffPanel v-if="revisionDiffOpen" :run="run" />
      </details>

      <div class="session-details__workspace">
        <aside class="session-details__sidebar">
          <section class="session-details__section">
            <h3>{{ $t("agents.profile") }}</h3>
            <dl>
              <div>
                <dt>{{ $t("common.status") }}</dt>
                <dd><StatusBadge :state="node.state" /></dd>
              </div>
              <div>
                <dt>{{ $t("runs.attempt", { attempt: node.attempt }) }}</dt>
                <dd>{{ node.attempt }}</dd>
              </div>
              <div class="session-details__long-field">
                <dt>{{ $t("agents.role") }}</dt>
                <dd
                  class="session-details__long-value"
                  tabindex="0"
                  role="region"
                  :aria-label="$t('agents.role')"
                >
                  {{ roleLabel }}
                </dd>
              </div>
              <div v-if="parentNode" class="session-details__long-field">
                <dt>{{ $t("common.source") }}</dt>
                <dd
                  class="session-details__long-value"
                  tabindex="0"
                  role="region"
                  :aria-label="$t('common.source')"
                >
                  {{ parentNode.displayName }}
                </dd>
              </div>
              <div>
                <dt>{{ $t("runs.startedAt") }}</dt>
                <dd>{{ formatDate(node.startedAt || node.createdAt) }}</dd>
              </div>
              <div>
                <dt>{{ $t("runs.finishedAt") }}</dt>
                <dd>
                  {{ formatDate(node.finishedAt) || $t("common.noData") }}
                </dd>
              </div>
            </dl>
          </section>

          <section class="session-details__section">
            <h3>{{ $t("runs.launchSummary") }}</h3>
            <dl>
              <div>
                <dt>{{ $t("common.source") }}</dt>
                <dd>{{ $t(`runs.source.${run.source}`) }}</dd>
              </div>
              <div class="session-details__long-field">
                <dt>{{ $t("runs.runContext") }}</dt>
                <dd>{{ run.title }}</dd>
              </div>
              <div
                v-if="rootRun && rootRun.ref !== run.ref"
                class="session-details__long-field"
              >
                <dt>{{ $t("runs.graph") }}</dt>
                <dd>{{ rootRun.title }}</dd>
              </div>
              <div>
                <dt>{{ $t("decisions.requestedBy") }}</dt>
                <dd>{{ run.initiator.displayName }}</dd>
              </div>
              <div>
                <dt>{{ $t("runs.sessionNode") }}</dt>
                <dd>
                  {{ node.displayName }} ·
                  {{ $t("runs.attempt", { attempt: run.attempt }) }}
                </dd>
              </div>
              <div>
                <dt>{{ $t("files.revision") }}</dt>
                <dd>Run v{{ run.version }} · Graph r{{ run.graphRevision }}</dd>
              </div>
              <div class="session-details__long-field">
                <dt>{{ $t("common.input") }}</dt>
                <dd>
                  <template v-if="node.inputSummary">
                    <div
                      id="run-session-input"
                      class="session-details__input"
                      :class="{
                        'session-details__input--expanded': inputExpanded,
                      }"
                    >
                      <SafeMarkdown :content="node.inputSummary" />
                    </div>
                    <button
                      v-if="node.inputSummary.length > 240"
                      type="button"
                      class="button button--ghost session-details__input-toggle"
                      :aria-expanded="inputExpanded"
                      aria-controls="run-session-input"
                      @click="inputExpanded = !inputExpanded"
                    >
                      {{
                        $t(
                          inputExpanded
                            ? "runs.collapseMessage"
                            : "runs.expandMessage",
                        )
                      }}
                    </button>
                  </template>
                  <template v-else>{{ $t("common.noData") }}</template>
                </dd>
              </div>
              <div v-if="node.integrationNames?.length">
                <dt>{{ $t("agents.integrations") }}</dt>
                <dd>{{ node.integrationNames.join(", ") }}</dd>
              </div>
            </dl>
          </section>

          <section class="session-details__section">
            <h3>{{ $t("agents.runtime") }}</h3>
            <dl v-if="agent">
              <div>
                <dt>{{ $t("agents.provider") }}</dt>
                <dd>{{ agent.runtimeProvider || $t("common.unavailable") }}</dd>
              </div>
              <div>
                <dt>{{ $t("agents.model") }}</dt>
                <dd>{{ agent.runtimeModel || $t("common.unavailable") }}</dd>
              </div>
              <div>
                <dt>{{ $t("agents.runtimeRevision") }}</dt>
                <dd>{{ agent.runtimeRevision || $t("common.unavailable") }}</dd>
              </div>
            </dl>
            <p v-else class="session-details__unavailable">
              {{ $t("common.unavailable") }}
            </p>
          </section>

          <section class="session-details__section">
            <h3>{{ $t("agents.instructions") }}</h3>
            <dl v-if="!sessionNode && agent?.publishedInstructions">
              <div>
                <dt>{{ $t("files.revision") }}</dt>
                <dd>
                  v{{ agent.publishedInstructions.version }} · r{{
                    agent.publishedInstructions.revision
                  }}
                  <StatusBadge :state="agent.publishedInstructions.state" />
                </dd>
              </div>
            </dl>
            <RunPromptPreview
              v-if="sessionNode && node.runRef === run.ref"
              :run="run"
              :title="$t('promptContext.preview')"
            />
            <p v-else class="session-details__unavailable">
              {{ $t("runs.renderedPromptUnavailable") }}
            </p>
          </section>

          <section v-if="usageItems.length" class="session-details__section">
            <h3>{{ $t("runs.usage.title") }}</h3>
            <dl>
              <div v-for="item in usageItems" :key="item[0]">
                <dt>{{ $t(`runs.usage.${item[0]}`) }}</dt>
                <dd>{{ formatTokenCount(item[1]) }}</dd>
              </div>
            </dl>
          </section>

          <section
            v-if="run.resultSummary && !resultInTranscript"
            class="session-details__section"
          >
            <h3>{{ $t("common.result") }}</h3>
            <SafeMarkdown :content="run.resultSummary" />
          </section>

          <section
            v-if="run.incidents?.length"
            class="session-details__section"
          >
            <h3>{{ $t("runs.incidents") }}</h3>
            <div class="session-details__incidents">
              <article v-for="incident in run.incidents" :key="incident.ref">
                <div>
                  <strong>{{ incident.safeSummary }}</strong>
                  <p>{{ incident.safeNextStep }}</p>
                </div>
                <StatusBadge :state="incident.severity" />
              </article>
            </div>
          </section>

          <section class="session-details__section">
            <h3>{{ $t("runs.artifacts") }}</h3>
            <div v-if="nodeArtifacts.length" class="session-details__files">
              <button
                v-for="artifact in nodeArtifacts"
                :key="artifact.ref"
                type="button"
                :disabled="!artifact.nextActions.includes('DOWNLOAD')"
                @click="emit('download', artifact)"
              >
                <span>{{ artifact.fileName }}</span>
                <small
                  >{{ artifact.mediaType }} · v{{ artifact.revision }}</small
                >
                <StatusBadge :state="artifact.scanState" />
              </button>
            </div>
            <p v-else class="session-details__unavailable">
              {{ $t("common.empty") }}
            </p>
          </section>
        </aside>

        <section class="session-details__activity">
          <header class="session-details__activity-heading">
            <div>
              <h3>{{ $t("runs.nodeConversation") }}</h3>
              <small>{{ node.displayName }}</small>
            </div>
            <StatusBadge :state="node.state" />
          </header>
          <RunTranscript
            v-if="transcriptItems.length"
            class="session-details__transcript"
            :items="transcriptItems"
            @download="emit('download', $event)"
          />
          <p v-else class="session-details__unavailable">
            {{ $t("runs.noNodeActivity") }}
          </p>
        </section>
      </div>
    </div>
  </ModalDialog>
</template>

<style scoped>
.session-details-dialog > :deep(.modal) {
  height: calc(100dvh - 40px);
}
.session-details-dialog > :deep(.modal > .modal__body) {
  display: flex;
  overflow: hidden;
}
.session-details {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
  min-height: 0;
}
.session-details > details {
  min-height: 0;
  max-height: 35%;
  flex: 0 1 auto;
  overflow: auto;
}
.session-details__summary {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  flex: 0 0 auto;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.session-details__summary strong {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
  line-height: 1.35;
}
.session-details__summary p {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  overflow-wrap: anywhere;
  min-width: 0;
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.84rem;
}
.session-details__summary > div {
  display: grid;
  min-width: 0;
  gap: 2px;
}
.session-details__summary small {
  color: var(--subtle);
  font-size: 0.74rem;
}
.session-details__avatar {
  display: grid;
  place-items: center;
  border: 1px solid var(--border);
  color: var(--accent);
  background: var(--surface);
}
.session-details__avatar {
  width: 42px;
  height: 42px;
  border-radius: 8px;
  overflow: hidden;
}
.session-details__avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.session-details__workspace {
  display: grid;
  grid-template-columns: minmax(280px, 0.34fr) minmax(0, 1fr);
  flex: 1;
  min-height: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.session-details__sidebar {
  display: grid;
  align-content: start;
  min-width: 0;
  min-height: 0;
  overflow: auto;
  border-right: 1px solid var(--border);
  background: var(--panel);
}
.session-details__section,
.session-details__activity {
  min-width: 0;
  padding: 14px;
}
.session-details__section {
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}
.session-details__section:last-child {
  border-bottom: 0;
}
.session-details h3 {
  margin: 0 0 10px;
  font-size: 0.92rem;
}
.session-details dl {
  display: grid;
  margin: 0;
}
.session-details dl > div {
  display: grid;
  grid-template-columns: minmax(120px, 0.42fr) minmax(0, 1fr);
  gap: 10px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.session-details dl > div:last-child {
  border-bottom: 0;
}
.session-details dl > .session-details__long-field {
  grid-template-columns: minmax(0, 1fr);
  gap: 4px;
}
.session-details__long-value {
  max-height: 150px;
  overflow: auto;
}
.session-details__long-value:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.session-details__input {
  max-height: 150px;
  overflow: auto;
}
.session-details__input--expanded {
  max-height: 320px;
  overflow: auto;
}
.session-details__input-toggle {
  margin-top: 6px;
}
.session-details :deep(.run-prompt-preview > .button) {
  display: block;
  width: 100%;
  height: 32px;
  min-height: 32px;
  padding: 0 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-details dt {
  color: var(--subtle);
  font-size: 0.78rem;
}
.session-details dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
  font-size: 0.84rem;
}
.session-details dd :deep(p) {
  margin: 0;
}
.session-details__unavailable {
  margin: 0;
  padding: 12px;
  border: 1px dashed var(--border-strong);
  border-radius: 8px;
  color: var(--muted);
  background: var(--panel);
}
.session-details__files {
  display: grid;
}
.session-details__incidents {
  display: grid;
  gap: 8px;
}
.session-details__incidents article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  padding: 9px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--warning-soft);
}
.session-details__incidents p {
  margin: 3px 0 0;
  color: var(--muted);
  font-size: 0.78rem;
}
.session-details__files > button {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 3px 10px;
  padding: 8px 0;
  border: 0;
  border-bottom: 1px solid var(--border);
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}
.session-details__files > button:last-child {
  border-bottom: 0;
}
.session-details__files > button:disabled {
  cursor: default;
}
.session-details__files span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-details__files small {
  grid-column: 1;
  color: var(--subtle);
}
.session-details__files :deep(.status-badge) {
  grid-column: 2;
  grid-row: 1 / span 2;
  align-self: center;
}
.session-details__transcript {
  padding: 14px;
}
.session-details__activity {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-height: 0;
  padding: 0;
  overflow: hidden;
  background: var(--surface);
}
.session-details__activity-heading {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
  background: color-mix(in srgb, var(--surface) 96%, transparent);
  backdrop-filter: blur(8px);
}
.session-details__activity-heading h3 {
  margin: 0;
}
.session-details__activity-heading small {
  display: block;
  margin-top: 2px;
  color: var(--subtle);
  font-size: 0.72rem;
}
@media (max-width: 760px) {
  .session-details {
    gap: 10px;
  }
  .session-details__summary {
    gap: 8px;
    padding: 10px;
  }
  .session-details__summary p {
    -webkit-line-clamp: 2;
  }
  .session-details__workspace {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(0, 0.18fr) minmax(0, 0.82fr);
    gap: 12px;
    border: 0;
  }
  .session-details__sidebar {
    border-right: 0;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .session-details__activity {
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .session-details dl > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
