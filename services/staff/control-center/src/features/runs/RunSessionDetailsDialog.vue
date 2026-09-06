<script setup lang="ts">
import { Bot, CircleDot, UserRound, Wrench } from "@lucide/vue";
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import type { PresentedRunEvent } from "@/features/runs/run-activity";
import { indexRunSessionOwnership } from "@/features/runs/run-session-graph";
import type {
  Agent,
  Artifact,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import RuntimeRevisionDiffPanel from "./RuntimeRevisionDiffPanel.vue";

const props = withDefaults(
  defineProps<{
    run: Run;
    rootRun?: Run;
    node: RunNode;
    nodes: RunNode[];
    events: PresentedRunEvent[];
    artifacts: Artifact[];
    agent?: Agent;
  }>(),
  { rootRun: undefined, agent: undefined },
);
const emit = defineEmits<{ close: []; download: [artifact: Artifact] }>();
const { locale } = useI18n();
const revisionDiffOpen = ref(false);

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
    .filter((event) => event.nodeRef && ownedNodeRefs.value.has(event.nodeRef))
    .sort((left, right) => left.sequence - right.sequence),
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
  if (usage.totalTokens === 0 && usage.modelContextWindow === 0) return [];
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

function eventKind(
  event: PresentedRunEvent,
): "user" | "agent" | "tool" | "system" {
  if (event.toolCall) return "tool";
  if (event.actor?.kind === "USER" || event.messageKind === "USER_MESSAGE")
    return "user";
  if (
    event.actor?.kind === "AGENT" ||
    event.actor?.kind === "SYSTEM_ASSISTANT" ||
    event.messageKind === "ASSISTANT_MESSAGE" ||
    event.messageKind === "INTERMEDIATE_MESSAGE" ||
    event.messageKind === "FINAL_MESSAGE"
  ) {
    return "agent";
  }
  return "system";
}
</script>

<template>
  <ModalDialog :title="node.displayName" size="xl" @close="$emit('close')">
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
          <strong>{{ node.role || $t(`runs.nodeTypes.${node.type}`) }}</strong>
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
              <div>
                <dt>{{ $t("agents.role") }}</dt>
                <dd>{{ node.role || $t(`runs.nodeTypes.${node.type}`) }}</dd>
              </div>
              <div v-if="parentNode">
                <dt>{{ $t("common.source") }}</dt>
                <dd>{{ parentNode.displayName }}</dd>
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
              <div>
                <dt>{{ $t("runs.runContext") }}</dt>
                <dd>{{ run.title }}</dd>
              </div>
              <div v-if="rootRun && rootRun.ref !== run.ref">
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
              <div>
                <dt>{{ $t("common.input") }}</dt>
                <dd>
                  <SafeMarkdown
                    v-if="node.inputSummary"
                    :content="node.inputSummary"
                  />
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
            <dl v-if="agent?.publishedInstructions">
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
            <p class="session-details__unavailable">
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

          <section v-if="run.resultSummary" class="session-details__section">
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
              <small>{{ node.displayName }} · {{ nodeEvents.length }}</small>
            </div>
            <StatusBadge :state="node.state" />
          </header>
          <ol v-if="nodeEvents.length">
            <li
              v-for="event in nodeEvents"
              :key="event.ref"
              :class="`session-details__event--${eventKind(event)}`"
              :data-message-kind="event.messageKind"
            >
              <span class="session-details__event-icon" aria-hidden="true">
                <Wrench v-if="event.toolCall" :size="16" />
                <UserRound
                  v-else-if="event.actor?.kind === 'USER'"
                  :size="16"
                />
                <Bot
                  v-else-if="
                    event.actor?.kind === 'AGENT' ||
                    event.actor?.kind === 'SYSTEM_ASSISTANT'
                  "
                  :size="16"
                />
                <CircleDot v-else :size="15" />
              </span>
              <article>
                <header>
                  <strong>{{ event.actor?.name || node.displayName }}</strong>
                  <StatusBadge
                    v-if="
                      event.toolCall?.state || event.nodeState || event.runState
                    "
                    :state="
                      event.toolCall?.state ||
                      event.nodeState ||
                      event.runState ||
                      ''
                    "
                  />
                  <time :datetime="event.occurredAt">
                    #{{ event.sequence }} · {{ formatDate(event.occurredAt) }}
                  </time>
                </header>
                <SafeMarkdown :content="event.displaySummary" />
                <SafeMarkdown
                  v-if="event.displayProgress"
                  class="session-details__event-progress"
                  :content="event.displayProgress"
                />
                <section v-if="event.toolCall" class="session-details__tool">
                  <strong>{{ event.toolCall.tool }}</strong>
                  <details>
                    <summary>{{ $t("runs.toolParameters") }}</summary>
                    <SafeStructuredData
                      :value="event.toolCall.safeParameters"
                    />
                  </details>
                  <details>
                    <summary>{{ $t("runs.toolResult") }}</summary>
                    <SafeMarkdown
                      v-if="event.toolCall.safeResult"
                      :content="event.toolCall.safeResult"
                    />
                    <p v-else>{{ $t("common.noData") }}</p>
                  </details>
                  <small class="session-details__tool-duration">
                    {{
                      $t("runs.toolDuration", {
                        duration: event.toolCall.durationMs,
                      })
                    }}
                  </small>
                </section>
              </article>
            </li>
          </ol>
          <p v-else class="session-details__unavailable">
            {{ $t("runs.noNodeActivity") }}
          </p>
        </section>
      </div>
    </div>
  </ModalDialog>
</template>

<style scoped>
.session-details {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.session-details__summary {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.session-details__summary p {
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
.session-details__avatar,
.session-details__event-icon {
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
  min-height: min(680px, calc(100dvh - 210px));
  max-height: min(760px, calc(100dvh - 170px));
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
.session-details__activity ol {
  display: grid;
  gap: 0;
  margin: 0;
  padding: 0;
  list-style: none;
}
.session-details__activity {
  min-height: 0;
  padding: 0;
  overflow: auto;
  background: var(--surface);
}
.session-details__activity-heading {
  position: sticky;
  z-index: 2;
  top: 0;
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
.session-details__activity li {
  position: relative;
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: 10px;
  padding: 12px 14px;
}
.session-details__activity li:not(:last-child)::before {
  position: absolute;
  left: 29px;
  top: 44px;
  bottom: -12px;
  width: 1px;
  background: var(--border);
  content: "";
}
.session-details__event-icon {
  z-index: 1;
  width: 30px;
  height: 30px;
  border-radius: 50%;
}
.session-details__activity article {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.session-details__event--user article {
  border-color: color-mix(in srgb, var(--accent) 48%, var(--border));
  background: var(--accent-soft);
}
.session-details__event--agent article {
  border-left: 3px solid var(--success);
}
.session-details__event--agent[data-message-kind="INTERMEDIATE_MESSAGE"]
  article {
  border-left-color: var(--accent);
}
.session-details__event--agent[data-message-kind="FINAL_MESSAGE"] article {
  background: color-mix(in srgb, var(--success) 5%, var(--surface));
}
.session-details__event--tool article {
  border-left: 3px solid var(--warning);
  background: color-mix(in srgb, var(--warning-soft) 55%, var(--surface));
}
.session-details__activity header {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: 6px;
}
.session-details__activity header strong {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.session-details__activity time {
  margin-left: auto;
  color: var(--subtle);
  font-family: var(--font-mono);
  font-size: 0.72rem;
  white-space: nowrap;
}
.session-details__activity :deep(p) {
  margin: 0 0 5px;
}
.session-details__activity details {
  margin-top: 8px;
}
.session-details__event-progress {
  margin-top: 8px;
  padding: 8px 10px;
  border-left: 2px solid var(--accent);
  background: var(--panel);
  color: var(--muted);
}
.session-details__tool {
  display: grid;
  gap: 6px;
  margin-top: 9px;
  padding-top: 9px;
  border-top: 1px solid var(--border);
}
.session-details__tool-duration {
  color: var(--subtle);
  font-size: 0.72rem;
}
@media (max-width: 760px) {
  .session-details__workspace {
    grid-template-columns: 1fr;
    max-height: none;
    overflow: visible;
    border: 0;
  }
  .session-details__sidebar {
    max-height: none;
    overflow: visible;
    border-right: 0;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .session-details__activity {
    min-height: 420px;
    margin-top: 12px;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .session-details dl > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .session-details__activity time {
    display: none;
  }
}
</style>
