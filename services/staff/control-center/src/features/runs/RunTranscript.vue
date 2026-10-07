<script setup lang="ts">
import {
  Bot,
  Clock,
  CircleDot,
  Download,
  FileText,
  Globe,
  Image as ImageIcon,
  MessageSquare,
  Terminal,
  UserRound,
  Wrench,
} from "@lucide/vue";
import { computed, nextTick, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  executionKey,
  activeTranscriptItemId,
  assistantFailureMessageKey,
  isAssistantPlanToolReceipt,
  integrationToolPresentationState,
  isTranscriptNearBottom,
  isUnboundRunCancellation,
  presentRunTranscriptItems,
  type RunActivityItem,
  type PresentedTranscriptItem,
} from "@/features/runs/run-activity";
import {
  presentRuntimeText,
  runtimeProgressKey,
} from "@/features/runs/runtime-text";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";

const props = withDefaults(
  defineProps<{
    items: readonly RunActivityItem[];
    embedded?: boolean;
    groupTools?: boolean;
    activeItemId?: string | null;
    closedExecutionKeys?: readonly string[];
  }>(),
  { embedded: false, groupTools: true, closedExecutionKeys: () => [] },
);
const emit = defineEmits<{ download: [artifact: Artifact] }>();
const { locale, t } = useI18n();
const serverMessage = useServerMessage();
const activeItemId = computed(() =>
  props.activeItemId === undefined
    ? activeTranscriptItemId(props.items, props.closedExecutionKeys)
    : props.activeItemId,
);
const displayItems = computed(() =>
  presentRunTranscriptItems(
    props.items,
    activeItemId.value,
    props.closedExecutionKeys,
  ).map((item) => {
    const text = (
      value: string | undefined,
      messageKind = item.messageKind,
    ) => {
      const key = runtimeProgressKey(value);
      return key
        ? t(key)
        : presentRuntimeText(value, serverMessage, messageKind);
    };
    const summaryText = (entry: RunActivityItem) => {
      const key =
        entry.kind === "system" && !entry.phase
          ? assistantFailureMessageKey(entry.summary, entry.state)
          : undefined;
      return key ? t(key) : text(entry.summary, entry.messageKind);
    };
    const completedServiceHistory = item.completedServiceHistory?.map(
      (step) => ({
        ...step,
        summary: summaryText(step),
        progress: text(step.progress, step.messageKind),
      }),
    );
    if (item.phase) {
      const key =
        item.phase === "FINAL"
          ? assistantFailureMessageKey(item.summary, item.state)
          : undefined;
      return {
        ...item,
        summary: key ? t(key) : item.summary,
        completedServiceHistory,
        compactIntegrationSuccess: false,
      };
    }
    const summary = summaryText(item);
    const progress = text(item.progress);
    const compactIntegrationSuccess = isStandaloneIntegrationSuccess(item);
    return {
      ...item,
      summary,
      progress,
      compactIntegrationSuccess,
      serviceHistory: compactIntegrationSuccess
        ? [{ ...item, summary, progress }]
        : item.serviceHistory?.map((step) => ({
            ...step,
            summary: summaryText(step),
            progress: text(step.progress, step.messageKind),
          })),
    };
  }),
);

// Самостоятельная квитанция сохраняет свои pins: с соседним tool не объединяется.
function isStandaloneIntegrationSuccess(
  item: PresentedTranscriptItem,
): boolean {
  return Boolean(
    !item.historical &&
    !item.working &&
    executionKey(item.execution) &&
    item.integrationInvocationRef &&
    /^inv_[A-Za-z0-9_-]{8,124}$/.test(item.integrationInvocationRef) &&
    item.kind === "system" &&
    item.eventType === "TURN_PROGRESS" &&
    item.messageKind === "INTERMEDIATE_MESSAGE" &&
    !item.phase &&
    !item.progress?.trim() &&
    !item.toolCall &&
    !item.artifact &&
    !item.artifactRef &&
    !["FAILED", "CANCELLED"].includes(item.state ?? "") &&
    (item.summary === "i18n:INTEGRATION_ACTION_SUCCEEDED" ||
      item.summary === t("serverMessages.INTEGRATION_ACTION_SUCCEEDED")),
  );
}

// Только представление зарезервированной runner-квитанции; доступ не меняется.
function isWorkspaceReceipt(item: RunActivityItem): boolean {
  const artifact = item.artifact;
  return Boolean(
    artifact?.source === "AGENT_RESULT" &&
    artifact.fileName === "workspace-write-result.json" &&
    artifact.mediaType === "application/json" &&
    artifact.scanState === "CLEAN" &&
    artifact.runRef &&
    artifact.sessionRef &&
    artifact.runRef === item.execution?.runRef &&
    artifact.sessionRef === item.execution.sessionRef,
  );
}

function toolIcon(item: RunActivityItem) {
  switch (item.toolCall?.tool) {
    case "CODEX_SHELL":
      return Terminal;
    case "CODEX_FILE_CHANGE":
      return FileText;
    case "CODEX_WEB_SEARCH":
      return Globe;
    case "CODEX_IMAGE_VIEW":
    case "CODEX_IMAGE_GENERATION":
      return ImageIcon;
    case "CODEX_SLEEP":
      return Clock;
    default:
      return Wrench;
  }
}
function visibleState(state: string | undefined, working: boolean): boolean {
  return Boolean(
    state &&
    (working ||
      !["CREATED", "QUEUED", "PENDING", "READY", "CLAIMED", "RUNNING"].includes(
        state,
      )),
  );
}
function toolState(tool: NonNullable<RunActivityItem["toolCall"]>): string {
  return integrationToolPresentationState(tool) ?? tool.state;
}
function groupToolState(items: readonly RunActivityItem[]): string {
  const states = items.map((item) =>
    item.toolCall ? toolState(item.toolCall) : "",
  );
  return (
    [
      "FAILED",
      "REJECTED",
      "CANCELLED",
      "WAITING_HUMAN",
      "NEEDS_ATTENTION",
      "RUNNING",
      "SUCCEEDED",
    ].find((state) => states.includes(state)) ?? ""
  );
}
function expandableMessage(item: RunActivityItem): boolean {
  return Boolean(
    item.summary &&
    item.summary.length > (item.phase === "COMMENTARY" ? 240 : 1200),
  );
}
const nativeTools = new Set([
  "CODEX_SHELL",
  "CODEX_FILE_CHANGE",
  "CODEX_WEB_SEARCH",
  "CODEX_DYNAMIC_TOOL",
  "CODEX_IMAGE_VIEW",
  "CODEX_IMAGE_GENERATION",
  "CODEX_SLEEP",
]);
const managedTools = new Set([
  "get_configuration_catalog",
  "propose_configuration_plan",
  "get_integration_catalog",
  "find_platform_resources",
  "propose_assistant_metadata",
  "propose_run_metadata",
  "delegate_agent",
  "invoke_integration",
  "search_files",
  "get_file_metadata",
  "preview_file",
  "get_file_manifest",
  "context7_resolve_library_id",
  "context7_query_docs",
]);
const nativeShellFields = new Set([
  "action_count",
  "action_kinds",
  "cwd_scope",
  "exit_code",
]);
const nativeDiagnosticFields = new Set(["codex_item_id", "source"]);

function nativeParameters(
  toolCall: NonNullable<RunActivityItem["toolCall"]>,
  diagnostic: boolean,
): [string, unknown][] {
  return Object.entries(toolCall.safeParameters).filter(([key]) =>
    diagnostic
      ? nativeDiagnosticFields.has(key)
      : toolCall.tool === "CODEX_SHELL" && nativeShellFields.has(key),
  );
}
function nativeOtherParameters(
  toolCall: NonNullable<RunActivityItem["toolCall"]>,
): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(toolCall.safeParameters).filter(
      ([key]) =>
        !nativeDiagnosticFields.has(key) &&
        !(toolCall.tool === "CODEX_SHELL" && nativeShellFields.has(key)),
    ),
  );
}
function nativeParameterValue(key: string, value: unknown): unknown {
  if (
    key === "action_kinds" &&
    Array.isArray(value) &&
    value.every(
      (kind) =>
        typeof kind === "string" &&
        ["READ", "LIST_FILES", "SEARCH", "UNKNOWN"].includes(kind),
    )
  )
    return value.map((kind: string) =>
      kind === "UNKNOWN"
        ? t("runs.nativeShellValues.UNKNOWN")
        : t(`runs.nativeShellActions.${kind}`),
    );
  const allowed =
    key === "cwd_scope"
      ? ["WORKSPACE", "OUTSIDE_WORKSPACE"]
      : key === "exit_code"
        ? ["ZERO", "NONZERO", "UNAVAILABLE"]
        : [];
  return typeof value === "string" && allowed.includes(value)
    ? t(`runs.nativeShellValues.${value}`)
    : value;
}
function nativeResultLabel(
  toolCall: NonNullable<RunActivityItem["toolCall"]>,
): string | undefined {
  if (!nativeTools.has(toolCall.tool)) return undefined;
  if (toolCall.safeResult === "COMPLETED" && toolCall.state === "SUCCEEDED")
    return t("states.SUCCEEDED");
  if (toolCall.safeResult === "RUNNING" && toolCall.state === "RUNNING")
    return t("states.RUNNING");
  if (toolCall.safeResult === "CANCELLED" && toolCall.state === "CANCELLED")
    return t("states.CANCELLED");
  if (
    toolCall.state === "FAILED" &&
    ["FAILED", "DECLINED"].includes(toolCall.safeResult)
  )
    return t(`runs.nativeToolResults.${toolCall.safeResult}`);
  return undefined;
}
const configurationCatalogKinds = new Set([
  "ASSISTANTS",
  "RUNTIME_PROFILES",
  "PROVIDER_ACCOUNTS",
  "MODELS",
  "ROLE_IMAGE_RECIPES",
  "IMAGE_ARTIFACTS",
  "ROLE_ENVIRONMENTS",
  "CURRENT_CONFIGURATION",
]);
function toolPreview(
  toolCall: NonNullable<RunActivityItem["toolCall"]>,
  working: boolean,
): string | undefined {
  if (
    nativeTools.has(toolCall.tool) &&
    ((toolCall.state === "SUCCEEDED" && toolCall.safeResult === "COMPLETED") ||
      (toolCall.state === "RUNNING" &&
        working &&
        toolCall.safeResult === "RUNNING"))
  )
    return undefined;
  if (
    toolCall.state === "FAILED" &&
    /^[A-Z][A-Z0-9_]{0,127}$/.test(
      toolCall.safeResult.trim().replace(/^i18n:/, ""),
    )
  )
    return undefined;
  return (toolCall.state === "SUCCEEDED" &&
    managedTools.has(toolCall.tool) &&
    toolCall.safeResult === `${toolCall.tool}:completed`) ||
    isAssistantPlanToolReceipt(toolCall) ||
    Boolean(integrationToolPresentationState(toolCall))
    ? undefined
    : toolCall.safeResult || undefined;
}

function compactServiceRow(item: (typeof displayItems.value)[number]): boolean {
  if (item.compactIntegrationSuccess) return true;
  if (isUnboundRunCancellation(item)) return Boolean(item.serviceHistory);
  const scope = executionKey(item.execution);
  return Boolean(
    scope &&
    item.serviceHistory &&
    !item.working &&
    !["FAILED", "CANCELLED"].includes(item.state ?? "") &&
    (props.closedExecutionKeys.includes(scope) ||
      displayItems.value.some(
        (entry) =>
          !entry.historical &&
          executionKey(entry.execution) === scope &&
          (Boolean(entry.toolCall) ||
            ((entry.phase === "COMMENTARY" || entry.phase === "FINAL") &&
              Boolean(entry.summary?.trim()))),
      )),
  );
}

function toolLabel(toolCall: NonNullable<RunActivityItem["toolCall"]>): string {
  if (toolCall.tool === "invoke_integration") {
    const label = t("runs.managedToolNames.invoke_integration");
    const capability = toolCall.capabilityRef;
    return typeof capability === "string" &&
      /^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$/.test(capability)
      ? `${label} · ${capability}`
      : label;
  }
  if (toolCall.tool === "get_configuration_catalog") {
    const kind = toolCall.safeParameters.catalogKind;
    if (typeof kind === "string" && configurationCatalogKinds.has(kind))
      return t(`runs.configurationCatalogNames.${kind}`);
  }
  if (managedTools.has(toolCall.tool))
    return t(`runs.managedToolNames.${toolCall.tool}`);
  if (!nativeTools.has(toolCall.tool))
    return /^[A-Za-z0-9_.:/-]{1,128}$/.test(toolCall.tool)
      ? toolCall.tool
      : t("runs.nativeToolNames.CODEX_DYNAMIC_TOOL");
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
    const groups: {
      id: string;
      items: (typeof displayItems.value)[number][];
      tools: boolean;
    }[] = [];
    const entries = displayItems.value.filter(
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
        executionKey(item.execution) &&
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
          {{ $t("runs.earlierServiceHistory") }}
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
                  <span
                    v-if="group.items.some((item) => item.working)"
                    class="run-transcript__work"
                    role="status"
                  >
                    {{ $t("runs.workIndicator")
                    }}<span class="run-transcript__dots" aria-hidden="true"
                      ><i /><i /><i
                    /></span>
                  </span>
                  <StatusBadge
                    v-else-if="visibleState(groupToolState(group.items), false)"
                    :state="groupToolState(group.items)"
                  />
                </summary>
                <RunTranscript
                  :items="group.items"
                  embedded
                  :group-tools="false"
                  :active-item-id="null"
                  @download="emit('download', $event)"
                />
              </details>
            </li>
            <template v-else>
              <li
                v-for="item in group.items"
                :key="item.id"
                class="run-activity-item"
                :class="[
                  `run-activity-item--${item.kind}`,
                  {
                    'run-activity-item--compact':
                      item.phase === 'COMMENTARY' ||
                      Boolean(item.artifact) ||
                      Boolean(item.toolCall) ||
                      Boolean(item.serviceHistory),
                    'run-activity-item--file': Boolean(item.artifact),
                    'run-activity-item--service': Boolean(item.serviceHistory),
                  },
                ]"
                :data-message-kind="item.messageKind"
                :data-phase="item.phase"
                :data-turn-ref="item.execution?.turnRef"
                :data-attempt="item.execution?.attempt"
              >
                <span
                  v-if="!item.artifact && !compactServiceRow(item)"
                  class="run-activity-item__icon"
                  aria-hidden="true"
                >
                  <component
                    :is="toolIcon(item)"
                    v-if="item.toolCall"
                    :size="17"
                  />
                  <UserRound v-else-if="item.kind === 'initiator'" :size="17" />
                  <MessageSquare
                    v-else-if="item.phase === 'COMMENTARY'"
                    :size="17"
                  />
                  <Bot v-else-if="item.kind === 'agent'" :size="17" />
                  <FileText v-else-if="item.artifact" :size="17" />
                  <CircleDot v-else :size="16" />
                </span>
                <article class="run-activity-item__content">
                  <header v-if="!item.artifact && !compactServiceRow(item)">
                    <strong>{{
                      item.toolCall
                        ? toolLabel(item.toolCall)
                        : item.actor || $t("runs.platformActor")
                    }}</strong>
                    <span v-if="item.phase" class="run-transcript__phase">{{
                      $t(`runs.messagePhases.${item.phase}`)
                    }}</span>
                    <span
                      v-if="item.working"
                      class="run-transcript__work"
                      role="status"
                    >
                      {{ $t("runs.workIndicator")
                      }}<span class="run-transcript__dots" aria-hidden="true"
                        ><i /><i /><i
                      /></span>
                    </span>
                    <StatusBadge
                      v-if="
                        visibleState(item.state, item.working) &&
                        !item.phase &&
                        !item.toolCall &&
                        !item.working
                      "
                      :state="item.state ?? ''"
                    />
                    <StatusBadge
                      v-if="
                        item.toolCall &&
                        visibleState(toolState(item.toolCall), false)
                      "
                      :state="toolState(item.toolCall)"
                    />
                    <time :datetime="item.occurredAt">{{
                      time(item.occurredAt)
                    }}</time>
                    <details
                      v-if="item.toolCall"
                      class="run-tool-event__details"
                    >
                      <summary
                        :aria-label="
                          $t('runs.toolDetailsFor', {
                            tool: toolLabel(item.toolCall),
                          })
                        "
                      >
                        {{ $t("runs.toolDetails") }}
                      </summary>
                      <div class="run-tool-event__body">
                        <p v-if="managedTools.has(item.toolCall.tool)">
                          {{ $t("runs.toolTechnicalId") }}:
                          <code>{{ item.toolCall.tool }}</code>
                        </p>
                        <section>
                          <strong>{{ $t("runs.toolParameters") }}</strong>
                          <template v-if="nativeTools.has(item.toolCall.tool)">
                            <dl class="run-native-tool__fields">
                              <template
                                v-for="[key, value] in nativeParameters(
                                  item.toolCall,
                                  false,
                                )"
                                :key="key"
                              >
                                <dt>
                                  {{ $t(`runs.nativeToolFields.${key}`) }}
                                </dt>
                                <dd>
                                  <SafeStructuredData
                                    :depth="1"
                                    :value="nativeParameterValue(key, value)"
                                  />
                                </dd>
                              </template>
                            </dl>
                            <SafeStructuredData
                              v-if="
                                Object.keys(
                                  nativeOtherParameters(item.toolCall),
                                ).length
                              "
                              :value="nativeOtherParameters(item.toolCall)"
                            />
                          </template>
                          <SafeStructuredData
                            v-else
                            :value="item.toolCall.safeParameters"
                          />
                        </section>
                        <section>
                          <strong>{{ $t("runs.toolResult") }}</strong>
                          <p v-if="nativeResultLabel(item.toolCall)">
                            {{ nativeResultLabel(item.toolCall) }}
                          </p>
                          <SafeMarkdown
                            v-else-if="item.toolCall.safeResult"
                            :content="item.toolCall.safeResult"
                          />
                          <p v-else>{{ $t("common.noData") }}</p>
                        </section>
                        <details
                          v-if="nativeTools.has(item.toolCall.tool)"
                          class="run-native-tool__diagnostics"
                        >
                          <summary>
                            {{ $t("runs.nativeToolDiagnostics") }}
                          </summary>
                          <dl class="run-native-tool__fields">
                            <dt>{{ $t("runs.toolTechnicalId") }}</dt>
                            <dd>
                              <code>{{ item.toolCall.tool }}</code>
                            </dd>
                            <template
                              v-for="[key, value] in nativeParameters(
                                item.toolCall,
                                true,
                              )"
                              :key="key"
                            >
                              <dt>{{ $t(`runs.nativeToolFields.${key}`) }}</dt>
                              <dd>
                                <SafeStructuredData :value="value" :depth="1" />
                              </dd>
                            </template>
                            <template v-if="nativeResultLabel(item.toolCall)">
                              <dt>{{ $t("runs.nativeToolResultCode") }}</dt>
                              <dd>
                                <code>{{ item.toolCall.safeResult }}</code>
                              </dd>
                            </template>
                          </dl>
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
                      </div>
                    </details>
                  </header>
                  <small
                    v-if="
                      item.execution &&
                      !item.artifact &&
                      !item.phase &&
                      !item.toolCall &&
                      !item.serviceHistory
                    "
                    class="run-transcript__execution"
                    >{{
                      $t("runs.transcriptTurn", {
                        turn: item.execution.turnNumber,
                        attempt: item.execution.attempt,
                      })
                    }}</small
                  >
                  <component
                    :is="isWorkspaceReceipt(item) ? 'details' : 'div'"
                    v-if="item.artifact"
                    :class="{ 'run-file-receipt': isWorkspaceReceipt(item) }"
                  >
                    <summary v-if="isWorkspaceReceipt(item)">
                      {{ $t("runs.workspaceReceipt") }}
                    </summary>
                    <section class="run-file-event">
                      <FileText
                        class="run-file-event__icon"
                        :size="20"
                        aria-hidden="true"
                      />
                      <div class="run-file-event__body">
                        <strong
                          class="run-file-event__name"
                          :title="item.artifact.fileName"
                          >{{ item.artifact.fileName }}</strong
                        >
                        <div class="run-file-event__metadata">
                          <small>{{ bytes(item.artifact.sizeBytes) }}</small>
                          <StatusBadge :state="item.artifact.scanState" />
                        </div>
                      </div>
                      <button
                        v-if="item.artifact.nextActions.includes('DOWNLOAD')"
                        type="button"
                        class="button button--ghost run-file-event__download"
                        :aria-label="`${$t('common.download')}: ${item.artifact.fileName}`"
                        :title="$t('common.download')"
                        @click="emit('download', item.artifact)"
                      >
                        <Download :size="18" aria-hidden="true" />
                      </button>
                    </section>
                    <details
                      v-if="item.artifact"
                      class="run-file-event__details"
                    >
                      <summary>{{ $t("runs.toolDetails") }}</summary>
                      <p>
                        {{ item.actor || $t("runs.platformActor") }} ·
                        <time :datetime="item.occurredAt">{{
                          time(item.occurredAt)
                        }}</time>
                        · {{ item.artifact.mediaType }} · v{{
                          item.artifact.revision
                        }}
                      </p>
                      <small v-if="item.execution">{{
                        $t("runs.transcriptTurn", {
                          turn: item.execution.turnNumber,
                          attempt: item.execution.attempt,
                        })
                      }}</small>
                    </details>
                  </component>
                  <template v-if="item.toolCall">
                    <SafeMarkdown
                      v-if="toolPreview(item.toolCall, item.working)"
                      :content="toolPreview(item.toolCall, item.working) ?? ''"
                      class="run-transcript__preview"
                    />
                  </template>
                  <template v-else>
                    <SafeMarkdown
                      v-if="
                        item.summary &&
                        !item.artifact &&
                        !compactServiceRow(item) &&
                        (!item.serviceHistory ||
                          ['FAILED', 'CANCELLED'].includes(item.state ?? ''))
                      "
                      :content="item.summary"
                      class="run-activity-item__message"
                      :class="{
                        'run-activity-item__message--collapsed':
                          expandableMessage(item) && !expanded[item.id],
                        'run-activity-item__message--commentary':
                          item.phase === 'COMMENTARY' &&
                          expandableMessage(item) &&
                          !expanded[item.id],
                      }"
                    />
                    <button
                      v-if="
                        !item.artifact &&
                        expandableMessage(item) &&
                        !item.serviceHistory
                      "
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
                      v-if="
                        item.progress && !item.artifact && !item.serviceHistory
                      "
                      :content="item.progress"
                    />
                    <details
                      v-if="item.serviceHistory || item.completedServiceHistory"
                      class="run-transcript__service-history"
                    >
                      <summary>
                        {{
                          $t("runs.serviceProgress", {
                            count:
                              (
                                item.serviceHistory ??
                                item.completedServiceHistory
                              )?.length ?? 0,
                          })
                        }}
                      </summary>
                      <ol>
                        <li
                          v-for="step in item.serviceHistory ??
                          item.completedServiceHistory"
                          :key="step.id"
                        >
                          <time :datetime="step.occurredAt"
                            >{{ time(step.occurredAt)
                            }}<template v-if="step.sequence">
                              · #{{ step.sequence }}</template
                            ></time
                          >
                          <SafeMarkdown
                            v-if="step.summary"
                            :content="step.summary"
                          />
                          <SafeMarkdown
                            v-if="step.progress"
                            :content="step.progress"
                          />
                        </li>
                      </ol>
                      <small v-if="item.execution">{{
                        $t("runs.transcriptTurn", {
                          turn: item.execution.turnNumber,
                          attempt: item.execution.attempt,
                        })
                      }}</small>
                    </details>
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
  width: min(86%, 760px);
  min-width: 0;
  justify-self: start;
}
.run-activity-item--initiator {
  grid-template-columns: minmax(0, 1fr) 28px;
  justify-self: end;
}
.run-activity-item--initiator > .run-activity-item__icon {
  grid-column: 2;
  grid-row: 1;
}
.run-activity-item--initiator > .run-activity-item__content {
  grid-column: 1;
  grid-row: 1;
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
.run-activity-item--compact > article {
  padding: 6px 10px;
  font-size: 0.85rem;
}
.run-activity-item--file {
  grid-template-columns: minmax(0, 1fr);
}
.run-activity-item--service > article {
  grid-column: 2;
  border: 0;
  background: transparent;
}
.run-activity-item--compact :deep(p) {
  margin-block: 4px;
}
.run-transcript__work {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--muted);
  font-size: 0.8rem;
}
.run-transcript__dots {
  display: inline-flex;
  gap: 3px;
}
.run-transcript__dots i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: currentColor;
  animation: transcript-working 1.2s ease-in-out infinite;
}
.run-transcript__dots i:nth-child(2) {
  animation-delay: 0.15s;
}
.run-transcript__dots i:nth-child(3) {
  animation-delay: 0.3s;
}
@keyframes transcript-working {
  0%,
  80%,
  100% {
    opacity: 0.35;
  }
  40% {
    opacity: 1;
  }
}
@media (prefers-reduced-motion: reduce) {
  .run-transcript__dots i {
    animation: none;
    opacity: 0.7;
  }
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
.run-activity-item__message--commentary {
  -webkit-line-clamp: 3;
}
.run-transcript__preview {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  margin-top: 4px;
}
.run-activity-item__expand {
  border: 0;
  padding: 4px 0;
  background: none;
  color: var(--accent);
  cursor: pointer;
}
.run-file-event {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
}
.run-file-event__icon {
  color: var(--muted);
}
.run-file-event__body {
  min-width: 0;
}
.run-file-event__name {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.run-file-event__metadata {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-top: 2px;
}
.run-file-event__download {
  width: 34px;
  height: 34px;
  min-height: 34px;
  padding: 0;
  justify-self: end;
}
.run-file-event__details {
  margin-top: 2px;
  color: var(--muted);
  font-size: 0.75rem;
}
.run-file-receipt > summary {
  color: var(--muted);
  cursor: pointer;
  font-size: 0.8rem;
}
.run-file-receipt[open] > summary {
  margin-bottom: 6px;
}
.run-file-receipt > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.run-file-event__details > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.run-transcript__tool-group {
  padding: 4px 8px;
  background: var(--panel);
  border-radius: 6px;
}
.run-tool-event__details {
  min-width: 0;
  font-size: 0.75rem;
  color: var(--muted);
}
.run-tool-event__details[open] {
  flex: 1 0 100%;
}
.run-tool-event__details > summary:focus-visible,
.run-native-tool__diagnostics > summary:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.run-tool-event__body {
  display: grid;
  gap: 8px;
  padding: 8px;
  margin-top: 4px;
  border-radius: 6px;
  background: var(--panel);
  color: var(--text);
}
.run-native-tool__fields {
  display: grid;
  grid-template-columns: minmax(0, 0.45fr) minmax(0, 1fr);
  gap: 4px 12px;
  margin: 6px 0 0;
}
.run-native-tool__fields dt {
  color: var(--muted);
}
.run-native-tool__fields dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.run-native-tool__diagnostics > summary {
  cursor: pointer;
  line-height: 32px;
}
.run-transcript__service-history {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.75rem;
}
.run-transcript__service-history ol {
  display: grid;
  gap: 4px;
  padding-left: 18px;
}
.run-transcript__tool-group {
  box-sizing: border-box;
  width: min(96%, 1180px);
  min-width: 0;
  justify-self: start;
  overflow-wrap: anywhere;
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
@media (max-width: 720px) {
  .run-transcript:not(.run-transcript--embedded) {
    padding: 10px;
  }
  .run-activity-item {
    width: 94%;
  }
}
</style>
