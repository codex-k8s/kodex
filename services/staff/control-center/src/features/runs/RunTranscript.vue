<script setup lang="ts">
import {
  Bot,
  CircleDot,
  Download,
  FileText,
  UserRound,
  Wrench,
} from "@lucide/vue";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  executionKey,
  isTranscriptNearBottom,
  type RunActivityItem,
} from "@/features/runs/run-activity";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = withDefaults(
  defineProps<{
    items: readonly RunActivityItem[];
    embedded?: boolean;
    groupTools?: boolean;
  }>(),
  { embedded: false, groupTools: true },
);
const emit = defineEmits<{ download: [artifact: Artifact] }>();
const { locale, t } = useI18n();
const nativeTools = new Set([
  "CODEX_SHELL",
  "CODEX_FILE_CHANGE",
  "CODEX_WEB_SEARCH",
  "CODEX_DYNAMIC_TOOL",
  "CODEX_IMAGE_VIEW",
  "CODEX_IMAGE_GENERATION",
  "CODEX_SLEEP",
]);

function toolLabel(toolCall: NonNullable<RunActivityItem["toolCall"]>): string {
  if (!nativeTools.has(toolCall.tool)) return toolCall.tool;
  const label = t(`runs.nativeToolNames.${toolCall.tool}`);
  if (toolCall.tool === "CODEX_DYNAMIC_TOOL") {
    const { namespace, tool } = toolCall.safeParameters;
    const safeName = (value: unknown): value is string =>
      typeof value === "string" && /^[A-Za-z0-9_.:/-]{1,128}$/.test(value);
    if (safeName(tool))
      return `${label} · ${safeName(namespace) ? `${namespace}.` : ""}${tool}`;
  }
  if (toolCall.tool === "CODEX_SHELL") {
    const kinds = toolCall.safeParameters.action_kinds;
    if (Array.isArray(kinds)) {
      const labels = ["READ", "LIST_FILES", "SEARCH"]
        .filter((kind) => kinds.includes(kind))
        .map((kind) => t(`runs.nativeShellActions.${kind}`));
      if (labels.length) return `${label} · ${labels.join(", ")}`;
    }
  }
  return label;
}
const log = ref<HTMLElement>();
const expanded = ref<Record<string, boolean>>({});
const unread = ref(false);
const following = ref(true);
const historicalLimit = ref(50);

const sections = computed(() =>
  [false, true].map((historical) => {
    const groups: { id: string; items: RunActivityItem[]; tools: boolean }[] =
      [];
    const entries = props.items.filter(
      (entry) => entry.historical === historical,
    );
    for (const item of historical
      ? entries.slice(-historicalLimit.value)
      : entries) {
      const last = groups.at(-1);
      if (
        props.groupTools &&
        item.toolCall &&
        last?.items[0]?.toolCall &&
        executionKey(item.execution) === executionKey(last.items[0].execution)
      ) {
        last.items.push(item);
      } else groups.push({ id: item.id, items: [item], tools: false });
    }
    for (const group of groups) group.tools = group.items.length >= 4;
    return { historical, groups, total: entries.length };
  }),
);

function onScroll(): void {
  const element = log.value;
  if (!element) return;
  following.value = isTranscriptNearBottom(element);
  if (following.value) unread.value = false;
}
function latest(): void {
  following.value = true;
  unread.value = false;
  log.value?.scrollTo({ top: log.value.scrollHeight });
}
watch(
  () =>
    props.items.map((item) => [
      item.id,
      item.revision,
      item.summary,
      item.state,
      item.toolCall?.state,
    ]),
  async () => {
    if (props.embedded) return;
    const follow = following.value;
    if (!follow) unread.value = true;
    await nextTick();
    if (follow && following.value) latest();
  },
);
onMounted(() => {
  if (!props.embedded) latest();
});

function time(value: string): string {
  return new Date(value).toLocaleTimeString(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
function bytes(value: number): string {
  const scale = value >= 1048576 ? 1048576 : value >= 1024 ? 1024 : 1;
  return new Intl.NumberFormat(locale.value, {
    style: "unit",
    unit: scale === 1048576 ? "megabyte" : scale === 1024 ? "kilobyte" : "byte",
    unitDisplay: "short",
    maximumFractionDigits: 1,
  }).format(value / scale);
}
</script>

<template>
  <section
    ref="log"
    class="run-transcript"
    :class="{ 'run-transcript--embedded': embedded }"
    :role="embedded ? undefined : 'log'"
    :aria-live="embedded ? undefined : 'polite'"
    @scroll.passive="onScroll"
  >
    <template v-for="section in sections" :key="String(section.historical)">
      <component
        :is="section.historical ? 'details' : 'section'"
        v-if="section.groups.length"
        :class="{ 'run-transcript__historical': section.historical }"
      >
        <summary v-if="section.historical" class="run-transcript__notice">
          {{ $t("runs.unscopedHistory") }}
          · {{ section.total }}
        </summary>
        <button
          v-if="section.historical && section.total > historicalLimit"
          type="button"
          class="button button--ghost"
          @click="historicalLimit += 50"
        >
          {{ $t("runs.earlierServiceHistory") }}
        </button>
        <ol class="run-activity-list">
          <template v-for="group in section.groups" :key="group.id">
            <li v-if="group.tools" class="run-transcript__tool-group">
              <details>
                <summary>
                  {{ group.items[0]?.actor || $t("runs.platformActor") }} ·
                  {{ $t("runs.toolGroup", { count: group.items.length }) }}
                  <StatusBadge
                    :state="
                      group.items.some(
                        (item) => item.toolCall?.state === 'RUNNING',
                      )
                        ? 'RUNNING'
                        : group.items.some(
                              (item) => item.toolCall?.state === 'FAILED',
                            )
                          ? 'FAILED'
                          : group.items.some(
                                (item) => item.toolCall?.state === 'CANCELLED',
                              )
                            ? 'CANCELLED'
                            : 'SUCCEEDED'
                    "
                  />
                </summary>
                <RunTranscript
                  :items="group.items"
                  embedded
                  :group-tools="false"
                  @download="emit('download', $event)"
                />
              </details>
            </li>
            <template v-else>
              <li
                v-for="item in group.items"
                :key="item.id"
                class="run-activity-item"
                :class="`run-activity-item--${item.kind}`"
                :data-message-kind="item.messageKind"
                :data-phase="item.phase"
                :data-turn-ref="item.execution?.turnRef"
                :data-attempt="item.execution?.attempt"
              >
                <span class="run-activity-item__icon" aria-hidden="true">
                  <Wrench v-if="item.toolCall" :size="17" />
                  <UserRound v-else-if="item.kind === 'initiator'" :size="17" />
                  <Bot v-else-if="item.kind === 'agent'" :size="17" />
                  <FileText v-else-if="item.artifact" :size="17" />
                  <CircleDot v-else :size="16" />
                </span>
                <article class="run-activity-item__content">
                  <header>
                    <strong>{{
                      item.actor || $t("runs.platformActor")
                    }}</strong>
                    <span v-if="item.phase" class="run-transcript__phase">{{
                      $t(`runs.messagePhases.${item.phase}`)
                    }}</span>
                    <StatusBadge
                      v-if="item.state && !item.phase && !item.toolCall"
                      :state="item.state"
                    />
                    <time :datetime="item.occurredAt"
                      >{{ time(item.occurredAt)
                      }}<template v-if="item.sequence">
                        · #{{ item.sequence }}</template
                      ></time
                    >
                  </header>
                  <small
                    v-if="item.execution"
                    class="run-transcript__execution"
                    >{{
                      $t("runs.transcriptTurn", {
                        turn: item.execution.turnNumber,
                        attempt: item.execution.attempt,
                      })
                    }}</small
                  >
                  <section v-if="item.artifact" class="run-file-event">
                    <strong>{{ item.artifact.fileName }}</strong>
                    <small
                      >{{ bytes(item.artifact.sizeBytes) }} ·
                      {{ item.artifact.mediaType }} · v{{
                        item.artifact.revision
                      }}</small
                    >
                    <StatusBadge :state="item.artifact.scanState" />
                    <button
                      v-if="item.artifact.nextActions.includes('DOWNLOAD')"
                      type="button"
                      class="button button--ghost"
                      @click="emit('download', item.artifact)"
                    >
                      <Download :size="16" />{{ $t("common.download") }}
                    </button>
                  </section>
                  <section v-if="item.toolCall" class="run-tool-event">
                    <header>
                      <strong>{{ toolLabel(item.toolCall) }}</strong
                      ><StatusBadge :state="item.toolCall.state" />
                    </header>
                    <details>
                      <summary>{{ $t("runs.toolParameters") }}</summary>
                      <p v-if="nativeTools.has(item.toolCall.tool)">
                        {{ $t("runs.toolTechnicalId") }}:
                        <code>{{ item.toolCall.tool }}</code>
                      </p>
                      <SafeStructuredData
                        :value="item.toolCall.safeParameters"
                      />
                    </details>
                    <details>
                      <summary>{{ $t("runs.toolResult") }}</summary>
                      <SafeMarkdown
                        v-if="item.toolCall.safeResult"
                        :content="item.toolCall.safeResult"
                      />
                      <p v-else>{{ $t("common.noData") }}</p>
                    </details>
                    <small
                      v-if="
                        item.toolCall.durationMs !== undefined &&
                        item.toolCall.state !== 'RUNNING'
                      "
                      >{{
                        $t("runs.toolDuration", {
                          duration: item.toolCall.durationMs,
                        })
                      }}</small
                    >
                  </section>
                  <template v-else>
                    <SafeMarkdown
                      v-if="item.summary"
                      :content="item.summary"
                      class="run-activity-item__message"
                      :class="{
                        'run-activity-item__message--collapsed':
                          item.summary.length > 1200 && !expanded[item.id],
                      }"
                    />
                    <button
                      v-if="item.summary && item.summary.length > 1200"
                      type="button"
                      class="run-activity-item__expand"
                      :aria-expanded="Boolean(expanded[item.id])"
                      @click="expanded[item.id] = !expanded[item.id]"
                    >
                      {{
                        $t(
                          expanded[item.id]
                            ? "runs.collapseMessage"
                            : "runs.expandMessage",
                        )
                      }}
                    </button>
                    <SafeMarkdown
                      v-if="item.progress"
                      :content="item.progress"
                    />
                    <p v-if="item.messageKind === 'ARTIFACT' && !item.artifact">
                      {{ $t("runs.artifactUnavailable") }}
                    </p>
                  </template>
                </article>
              </li>
            </template>
          </template>
        </ol>
      </component>
    </template>
    <p v-if="!items.length" class="run-transcript__empty">
      {{ $t("runs.noNodeActivity") }}
    </p>
    <button
      v-if="unread && !embedded"
      class="button run-transcript__latest"
      type="button"
      @click="latest"
    >
      {{ $t("runs.newMessages") }}
    </button>
  </section>
</template>

<style scoped>
.run-transcript {
  position: relative;
  min-height: 0;
  overflow: auto;
  padding: 16px;
}
.run-transcript--embedded {
  overflow: visible;
  padding: 0;
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
.run-activity-item__content {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  overflow-wrap: anywhere;
}
.run-activity-item--initiator > article {
  border-color: var(--accent);
  background: var(--accent-soft);
}
.run-activity-item--agent > article {
  border-left: 3px solid var(--success);
}
.run-activity-item header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.run-activity-item time {
  margin-left: auto;
}
.run-activity-item time,
.run-transcript__phase,
.run-transcript__execution,
small {
  color: var(--muted);
  font-size: 0.75rem;
}
.run-activity-item__message--collapsed {
  display: -webkit-box;
  -webkit-line-clamp: 8;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.run-activity-item__expand {
  border: 0;
  padding: 4px 0;
  background: none;
  color: var(--accent);
  cursor: pointer;
}
.run-tool-event,
.run-file-event {
  display: grid;
  gap: 8px;
  margin-top: 8px;
}
.run-tool-event details,
.run-transcript__tool-group {
  padding: 8px;
  background: var(--panel);
  border-radius: 6px;
}
summary {
  cursor: pointer;
}
.run-transcript__tool-group > details > summary {
  margin-bottom: 8px;
}
.run-transcript__historical {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.run-transcript__notice,
.run-transcript__empty {
  color: var(--muted);
  font-size: 0.8rem;
}
.run-transcript__latest {
  position: sticky;
  bottom: 8px;
  display: block;
  margin: 12px auto 0;
  background: var(--surface);
}
</style>
