<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { assistantLaunchedRunTarget } from "@/features/assistant/model";
import { usePlatformStore } from "@/features/platform/store";
import { requestSignal } from "@/shared/api/client";
import { getRun } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  AssistantPlan,
  Run,
} from "@/shared/api/generated/openapi/types.gen";
import { unwrap } from "@/shared/api/problem";
import SafeMarkdown from "@/shared/ui/SafeMarkdown.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { requestConfirmation } from "@/shared/ui/confirmation";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const { t } = useI18n();
const platform = usePlatformStore();
const target = computed(() =>
  assistantLaunchedRunTarget(props.plan, props.operationRef),
);
const readback = ref<Run>();
const run = computed(() => {
  const exact = target.value;
  return exact ? (platform.runs[exact.runRef] ?? readback.value) : undefined;
});
const loading = ref(false);
const stopping = ref(false);
const problem = ref(false);
let refresh: (() => Promise<void>) | undefined;

async function readExactRun(
  projectRef: string,
  runRef: string,
  signal?: AbortSignal,
): Promise<Run> {
  const current = (
    await unwrap(
      getRun({
        path: { runRef },
        signal: requestSignal(signal),
      }),
    )
  ).data;
  if (
    current.ref !== runRef ||
    current.projectRef !== projectRef ||
    current.source !== "SYSTEM_ASSISTANT"
  )
    throw new Error("Assistant run readback mismatch");
  return current;
}

watch(
  target,
  (value, _previous, onCleanup) => {
    readback.value = undefined;
    loading.value = false;
    problem.value = false;
    if (!value) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      try {
        const current = await readExactRun(
          value.projectRef,
          value.runRef,
          controller.signal,
        );
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        readback.value = current;
        platform.runs[current.ref] = current;
        problem.value = false;
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    const cached = platform.runs[value.runRef];
    if (
      cached?.projectRef === value.projectRef &&
      cached.source === "SYSTEM_ASSISTANT"
    )
      readback.value = cached;
    else void refresh();
  },
  { immediate: true },
);

async function stopRun(): Promise<void> {
  const exact = target.value;
  if (
    !exact ||
    !run.value?.nextActions.includes("CANCEL") ||
    stopping.value ||
    !(await requestConfirmation({
      message: t("assistant.launchedRun.stopConfirm"),
      tone: "danger",
    }))
  )
    return;
  stopping.value = true;
  try {
    const current = await readExactRun(exact.projectRef, exact.runRef);
    // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- план мог смениться во время сетевого запроса.
    if (target.value?.runRef !== exact.runRef) return;
    if (!current.nextActions.includes("CANCEL")) {
      readback.value = current;
      platform.runs[current.ref] = current;
      return;
    }
    const changed = await platform.changeRun(current, { action: "CANCEL" });
    if (changed.ref !== exact.runRef || changed.projectRef !== exact.projectRef)
      throw new Error("Assistant run cancellation readback mismatch");
    readback.value = changed;
    problem.value = false;
  } catch {
    problem.value = true;
  } finally {
    stopping.value = false;
  }
}
</script>

<template>
  <section v-if="target" class="assistant-run-card" aria-live="polite">
    <header>
      <strong>{{ $t("assistant.launchedRun.title") }}</strong>
      <StatusBadge v-if="run" :state="run.state" />
    </header>
    <p v-if="loading && !run">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="assistant-run-card__problem" role="alert">
      {{ $t("assistant.launchedRun.loadFailed") }}
    </p>
    <template v-if="run">
      <p>{{ run.title }}</p>
      <p v-if="run.currentActivity || run.activitySummary">
        {{ run.currentActivity || run.activitySummary }}
      </p>
      <section v-if="run.resultSummary" class="assistant-run-card__result">
        <strong>{{ $t("assistant.launchedRun.result") }}</strong>
        <SafeMarkdown :content="run.resultSummary" />
      </section>
      <p v-if="run.safeErrorCode" class="assistant-run-card__problem">
        {{ run.safeErrorCode }}
      </p>
      <p v-if="run.state === 'WAITING_HUMAN'">
        {{ $t("assistant.launchedRun.awaitingDecision") }}
      </p>
    </template>
    <div class="assistant-run-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading || stopping"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        class="button button--primary"
        :to="{
          name: 'project-run',
          params: { projectRef: target.projectRef, runRef: target.runRef },
        }"
        @click="emit('navigate')"
      >
        {{ $t("assistant.launchedRun.open") }}
      </RouterLink>
      <button
        v-if="run?.nextActions.includes('CANCEL')"
        class="button button--danger"
        type="button"
        :disabled="loading || stopping"
        @click="stopRun"
      >
        {{ $t("assistant.launchedRun.stop") }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.assistant-run-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-run-card header,
.assistant-run-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-run-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-run-card__result {
  display: grid;
  gap: 8px;
  max-height: min(34vh, 320px);
  overflow-y: auto;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
.assistant-run-card__result :deep(.safe-markdown > :first-child) {
  margin-top: 0;
}
.assistant-run-card__result :deep(.safe-markdown > :last-child) {
  margin-bottom: 0;
}
.assistant-run-card__problem {
  color: var(--danger);
}
</style>
