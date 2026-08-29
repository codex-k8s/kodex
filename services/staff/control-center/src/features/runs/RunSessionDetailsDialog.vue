<script setup lang="ts">
import { Bot, CircleDot, Wrench } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { PresentedRunEvent } from "@/features/runs/run-activity";
import type {
  Agent,
  Run,
  RunNode,
} from "@/shared/api/generated/openapi/types.gen";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{
    run: Run;
    node: RunNode;
    nodes: RunNode[];
    events: PresentedRunEvent[];
    agent?: Agent;
  }>(),
  { agent: undefined },
);
defineEmits<{ close: [] }>();
const { locale } = useI18n();

const parentNode = computed(() =>
  props.nodes.find((candidate) => candidate.ref === props.node.parentNodeRef),
);
const nodeEvents = computed(() =>
  props.events.filter((event) => event.nodeRef === props.node.ref),
);

function formatDate(value?: string): string {
  return value ? new Date(value).toLocaleString(locale.value) : "";
}
</script>

<template>
  <ModalDialog :title="node.displayName" size="xl" @close="$emit('close')">
    <div class="session-details">
      <header class="session-details__summary">
        <span class="session-details__avatar">
          <Bot :size="22" aria-hidden="true" />
        </span>
        <div>
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

      <div class="session-details__grid">
        <section class="session-details__section">
          <h3>{{ $t("agents.profile") }}</h3>
          <dl>
            <div>
              <dt>{{ $t("common.status") }}</dt>
              <dd><StatusBadge :state="node.state" /></dd>
            </div>
            <div>
              <dt>{{ $t("runs.attempt", { attempt: node.attempt }) }}</dt>
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
              <dd>{{ formatDate(node.finishedAt) || $t("common.noData") }}</dd>
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
          <p class="session-details__unavailable">
            {{ $t("common.unavailable") }}
          </p>
        </section>
      </div>

      <section class="session-details__activity">
        <h3>{{ $t("runs.nodeConversation") }}</h3>
        <ol v-if="nodeEvents.length">
          <li v-for="event in nodeEvents" :key="event.ref">
            <span class="session-details__event-icon" aria-hidden="true">
              <Wrench v-if="event.toolCall" :size="16" />
              <Bot v-else-if="event.actor?.kind === 'AGENT'" :size="16" />
              <CircleDot v-else :size="15" />
            </span>
            <article>
              <header>
                <strong>{{ event.actor?.name || node.displayName }}</strong>
                <time :datetime="event.occurredAt">
                  {{ formatDate(event.occurredAt) }}
                </time>
              </header>
              <SafeMarkdown :content="event.displaySummary" />
              <SafeMarkdown
                v-if="event.displayProgress"
                :content="event.displayProgress"
              />
              <details
                v-if="
                  event.toolCall &&
                  Object.keys(event.toolCall.safeParameters).length
                "
              >
                <summary>{{ $t("runs.toolParameters") }}</summary>
                <SafeStructuredData :value="event.toolCall.safeParameters" />
              </details>
            </article>
          </li>
        </ol>
        <p v-else class="session-details__unavailable">
          {{ $t("runs.noNodeActivity") }}
        </p>
      </section>
    </div>
  </ModalDialog>
</template>

<style scoped>
.session-details {
  display: grid;
  gap: 18px;
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
}
.session-details__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.session-details__section,
.session-details__activity {
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
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
.session-details__activity ol {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.session-details__activity li {
  display: grid;
  grid-template-columns: 30px minmax(0, 1fr);
  gap: 9px;
}
.session-details__event-icon {
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
.session-details__activity header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin-bottom: 6px;
}
.session-details__activity time {
  color: var(--subtle);
  font-size: 0.72rem;
}
.session-details__activity :deep(p) {
  margin: 0 0 5px;
}
.session-details__activity details {
  margin-top: 8px;
}
@media (max-width: 760px) {
  .session-details__grid {
    grid-template-columns: 1fr;
  }
  .session-details dl > div {
    grid-template-columns: 1fr;
    gap: 4px;
  }
}
</style>
