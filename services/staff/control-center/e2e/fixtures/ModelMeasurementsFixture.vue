<script setup lang="ts">
import RunTranscript from "../../src/features/runs/RunTranscript.vue";
import type { RunActivityItem } from "../../src/features/runs/run-activity";
const execution = {
  runRef: "run_synthetic",
  nodeRef: "nod_synthetic",
  sessionRef: "ses_synthetic",
  turnRef: "trn_synthetic",
  turnNumber: 1,
  attempt: 1,
};
const working: RunActivityItem = {
  id: "evt_working",
  kind: "system",
  actor: "Kodex",
  occurredAt: "2026-11-12T00:00:00Z",
  historical: false,
  state: "RUNNING",
  execution,
  summary: "i18n:MODEL_CONTEXT_COMPACTING",
  serviceProgressCode: "MODEL_CONTEXT_COMPACTING",
  eventType: "TURN_PROGRESS",
  messageKind: "INTERMEDIATE_MESSAGE",
};
const items: RunActivityItem[] = [
  working,
  {
    ...working,
    id: "evt_request",
    modelMeasurement: {
      ref: "request_synthetic01",
      source: "HTTP_REQUEST",
      phase: "NORMAL",
      outcome: "COMPLETED",
      completeness: "COMPLETE",
      durationMs: 300,
      ttftMs: 42,
    },
  },
  {
    ...working,
    id: "evt_step",
    modelMeasurement: {
      ref: "step_synthetic01",
      source: "NATIVE_STEP",
      phase: "COMPACTION",
      outcome: "COMPLETED",
      completeness: "PARTIAL",
      usage: {
        completeness: "PARTIAL",
        inputTokens: 12000,
        outputTokens: 320,
        totalTokens: 12320,
        cachedInputTokens: 4000,
        cacheWriteInputTokens: 1000,
        reasoningOutputTokens: 20,
        modelContextWindow: 16000,
      },
    },
  },
];
</script>
<template>
  <main style="padding: 1rem; max-width: 70rem; margin: auto">
    <h1>Измерения синтетического запуска</h1>
    <RunTranscript :items="items" />
  </main>
</template>
