<script setup lang="ts">
import { useId } from "vue";
import { useI18n } from "vue-i18n";
import { RouterLink } from "vue-router";
import type { IntegrationCredentialPreflightIssue } from "@/shared/api/generated/openapi/types.gen";

// Одинаковый полный список используется обычным launch и запуском turn помощника.
defineProps<{ issues: readonly IntegrationCredentialPreflightIssue[] }>();
const { t } = useI18n();
const titleId = `personal-preflight-${useId()}`;
</script>

<template>
  <section class="personal-preflight" role="alert" :aria-labelledby="titleId">
    <h3 :id="titleId">{{ t("personalConnections.preflight.title") }}</h3>
    <p>{{ t("personalConnections.preflight.description") }}</p>
    <ul>
      <li
        v-for="issue in issues"
        :key="`${issue.connectionRef}:${issue.capabilityKey}`"
      >
        <strong>{{ issue.connectionName || issue.connectionRef }}</strong>
        — {{ t(`personalConnections.preflight.reasons.${issue.problem}`) }}
        <small>{{ issue.capabilityKey }}</small>
      </li>
    </ul>
    <RouterLink
      class="button"
      :to="{ name: 'integrations', query: { section: 'personal' } }"
      target="_blank"
      rel="noopener"
      >{{ t("personalConnections.preflight.settings") }}</RouterLink
    >
  </section>
</template>

<style scoped>
.personal-preflight {
  padding: 16px;
  border: 1px solid var(--border-strong);
  border-radius: 12px;
}
.personal-preflight h3 {
  margin-top: 0;
}
.personal-preflight li {
  margin: 8px 0;
  overflow-wrap: anywhere;
}
.personal-preflight small {
  display: block;
  color: var(--muted);
}
</style>
