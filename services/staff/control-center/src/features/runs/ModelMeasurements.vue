<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import { executionKey, type RunActivityItem } from "./run-activity";
import { validModelMeasurement } from "@/shared/api/model-measurement";
const props = defineProps<{ items: readonly RunActivityItem[] }>();
const { t } = useI18n();
const rows = computed(() => {
  const seen = new Set<string>();
  const measurements = props.items.flatMap((item) => {
    const value = item.modelMeasurement,
      scope = executionKey(item.execution);
    if (!scope || !value || !validModelMeasurement(value)) return [];
    const key = JSON.stringify([scope, value.ref]);
    if (seen.has(key)) return [];
    seen.add(key);
    return [value];
  });
  return (["NORMAL", "COMPACTION", "AUXILIARY"] as const).flatMap((phase) => {
    const values = measurements.filter((value) => value.phase === phase);
    if (!values.length) return [];
    const requests = values.filter((value) => value.source === "HTTP_REQUEST");
    const steps = values.filter((value) => value.source === "NATIVE_STEP");
    const timings = requests.flatMap((value) =>
      value.ttftMs === undefined ? [] : [value.ttftMs],
    );
    const usage = steps.flatMap((value) =>
      value.usage?.completeness === "PARTIAL" ? [value.usage] : [],
    );
    const sum = (
      field:
        | "inputTokens"
        | "outputTokens"
        | "cachedInputTokens"
        | "cacheWriteInputTokens",
    ) =>
      usage.length
        ? usage.reduce((total, value) => total + value[field], 0)
        : undefined;
    return [
      {
        phase,
        requests: requests.length,
        steps: steps.length,
        input: sum("inputTokens"),
        output: sum("outputTokens"),
        cached: sum("cachedInputTokens"),
        written: sum("cacheWriteInputTokens"),
        measured: timings.length,
        ttft: timings.length
          ? Math.round(
              timings.reduce((total, value) => total + value, 0) /
                timings.length,
            )
          : undefined,
      },
    ];
  });
});
</script>
<template>
  <details
    v-if="rows.length"
    class="model-measurements"
    data-testid="model-measurements"
  >
    <summary>
      {{ t("modelMeasurements.title") }} · {{ t("modelMeasurements.partial") }}
    </summary>
    <p>{{ t("modelMeasurements.scope") }}</p>
    <div class="model-measurements__scroll">
      <table>
        <thead>
          <tr>
            <th
              v-for="field in [
                'phase',
                'requests',
                'steps',
                'input',
                'output',
                'cached',
                'written',
                'ttft',
              ]"
              :key="field"
            >
              {{ t(`modelMeasurements.${field}`) }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.phase">
            <th>{{ t(`modelMeasurements.${row.phase}`) }}</th>
            <td>{{ row.requests }}</td>
            <td>{{ row.steps }}</td>
            <td
              v-for="field in ['input', 'output', 'cached', 'written'] as const"
              :key="field"
            >
              {{ row[field] ?? t("modelMeasurements.unknown") }}
            </td>
            <td>
              {{ row.ttft ?? t("modelMeasurements.unknown")
              }}<small>{{
                t("modelMeasurements.measured", {
                  count: row.measured,
                  total: row.requests,
                })
              }}</small>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p>{{ t("modelMeasurements.cost") }}</p>
  </details>
</template>
<style scoped>
.model-measurements {
  margin-block: 0.75rem;
}
.model-measurements__scroll {
  overflow-x: auto;
}
th,
td {
  padding: 0.35rem 0.5rem;
  text-align: start;
  white-space: nowrap;
}
small {
  display: block;
}
</style>
