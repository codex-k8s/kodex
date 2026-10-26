<script setup lang="ts">
import type { WorkflowAttemptResult } from "@/shared/api/generated/openapi/types.gen";
defineProps<{ attempts: WorkflowAttemptResult[] }>();
</script>

<template>
  <section v-if="attempts.length" class="workflow-attempt-results">
    <h3>{{ $t("workflowResults.attempts") }}</h3>
    <article
      v-for="attempt in attempts"
      :key="`${attempt.attempt}/${attempt.runRef}`"
    >
      <h4>{{ $t("runs.attempt", { attempt: attempt.attempt }) }}</h4>
      <p :class="{ 'field-error': !attempt.accepted }">
        {{
          $t(
            attempt.accepted
              ? "workflowResults.accepted"
              : "workflowResults.rejected",
          )
        }}
      </p>
      <ul v-if="attempt.results.length">
        <li v-for="result in attempt.results" :key="result.key">
          {{ $t(`workflowResults.kinds.${result.kind}`) }}
          <span v-if="result.repository">
            · {{ result.repository
            }}<template v-if="result.number">
              #{{ result.number }}</template
            ></span
          >
          <span v-if="result.kind === 'DEPLOYMENT'">
            ·
            {{
              $t(
                result.healthOk === true
                  ? "workflowResults.healthy"
                  : "workflowResults.healthUnverified",
              )
            }}</span
          >
          <details>
            <summary>{{ $t("common.advanced") }}</summary>
            <dl>
              <div>
                <dt>{{ $t("workflowResults.resultKey") }}</dt>
                <dd>{{ result.key }}</dd>
              </div>
              <div
                v-for="field in [
                  'digest',
                  'artifactRef',
                  'artifactRevision',
                  'receiptRef',
                  'headSha',
                  'targetRef',
                  'bundleSha256',
                  'imageDigest',
                ] as const"
                :key="field"
              >
                <template v-if="result[field]"
                  ><dt>{{ field }}</dt>
                  <dd>
                    <code>{{ result[field] }}</code>
                  </dd></template
                >
              </div>
            </dl>
          </details>
        </li>
      </ul>
      <details v-if="attempt.reason">
        <summary>{{ $t("workflowResults.reason") }}</summary>
        <code>{{ attempt.reason }}</code>
      </details>
    </article>
  </section>
</template>

<style scoped>
.workflow-attempt-results {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.workflow-attempt-results article {
  border-top: 1px solid var(--border);
  padding-top: 8px;
}
.workflow-attempt-results code {
  overflow-wrap: anywhere;
}
.workflow-attempt-results dd {
  margin-inline-start: 0;
}
</style>
