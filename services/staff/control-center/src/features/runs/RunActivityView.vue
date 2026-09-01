<script setup lang="ts">
import { Bot, CircleDot, UserRound, Wrench } from "@lucide/vue";
import { computed } from "vue";

import type { RunEvent } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{ events: readonly RunEvent[] }>();
const items = computed(() =>
  [...props.events].sort((a, b) => a.sequence - b.sequence),
);

function messageClass(event: RunEvent): string {
  if (event.actor?.kind === "USER") return "run-activity-item--user";
  if (event.actor?.kind === "AGENT" || event.actor?.kind === "SYSTEM_ASSISTANT")
    return "run-activity-item--assistant";
  return "run-activity-item--system";
}

function isMessage(event: RunEvent): boolean {
  return [
    "USER_MESSAGE",
    "ASSISTANT_MESSAGE",
    "INTERMEDIATE_MESSAGE",
    "FINAL_MESSAGE",
  ].includes(event.messageKind ?? "");
}
</script>

<template>
  <section class="run-activity" aria-live="polite">
    <ol v-if="items.length" class="run-activity-list">
      <li
        v-for="event in items"
        :key="event.ref"
        class="run-activity-item"
        :class="messageClass(event)"
      >
        <span class="run-activity-item__icon" aria-hidden="true">
          <Wrench v-if="event.toolCall" :size="17" />
          <UserRound v-else-if="event.actor?.kind === 'USER'" :size="17" />
          <Bot
            v-else-if="
              event.actor?.kind === 'AGENT' ||
              event.actor?.kind === 'SYSTEM_ASSISTANT'
            "
            :size="17"
          />
          <CircleDot v-else :size="16" />
        </span>
        <article>
          <header>
            <strong>{{ event.actor?.name ?? $t("runs.platformActor") }}</strong>
            <time :datetime="event.occurredAt">
              {{ new Date(event.occurredAt).toLocaleTimeString() }}
            </time>
          </header>
          <SafeMarkdown :content="event.summary" />
          <SafeMarkdown v-if="event.progress" :content="event.progress" />
          <section v-if="event.toolCall" class="run-tool-call">
            <header>
              <strong>{{ event.toolCall.tool }}</strong>
              <StatusBadge :state="event.toolCall.state" />
            </header>
            <details v-if="Object.keys(event.toolCall.safeParameters).length">
              <summary>{{ $t("runs.toolParameters") }}</summary>
              <SafeStructuredData :value="event.toolCall.safeParameters" />
            </details>
            <p v-if="event.toolCall.safeResult">
              {{ event.toolCall.safeResult }}
            </p>
            <small>
              {{
                $t("runs.toolDuration", {
                  duration: event.toolCall.durationMs,
                })
              }}
            </small>
          </section>
          <StatusBadge
            v-else-if="!isMessage(event) && (event.nodeState || event.runState)"
            :state="event.nodeState ?? event.runState ?? ''"
          />
        </article>
      </li>
    </ol>
    <div v-else class="assistant-empty-state">
      <CircleDot :size="24" aria-hidden="true" />
      <p>{{ $t("runs.noEvents") }}</p>
    </div>
  </section>
</template>

<style scoped>
.run-activity {
  min-height: 0;
  overflow: auto;
  padding: 16px;
}
.run-activity-list {
  display: grid;
  gap: 14px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.run-activity-item {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr);
  gap: 8px;
}
.run-activity-item__icon {
  display: grid;
  width: 28px;
  height: 28px;
  place-items: center;
  border-radius: 50%;
  background: var(--panel);
  color: var(--muted);
}
.run-activity-item > article {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.run-activity-item--user > article {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.run-activity-item--assistant > article {
  border-left: 3px solid var(--success);
}
.run-activity-item header,
.run-tool-call header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.run-activity-item time,
.run-tool-call small {
  color: var(--subtle);
  font-size: 0.75rem;
}
.run-tool-call {
  display: grid;
  gap: 8px;
  margin-top: 10px;
  padding: 10px;
  border-radius: 6px;
  background: var(--panel);
}
.run-tool-call p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-empty-state {
  display: grid;
  min-height: 220px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--muted);
  text-align: center;
}
</style>
