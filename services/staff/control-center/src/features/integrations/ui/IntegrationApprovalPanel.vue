<script setup lang="ts">
import { ShieldCheck } from "@lucide/vue";
import { useI18n } from "vue-i18n";
const { t } = useI18n();
</script>
<template>
  <section class="approval-panel panel stack" aria-labelledby="approval-title">
    <h2 id="approval-title">{{ t("integrationsRedesign.approvalsTitle") }}</h2>
    <p>{{ t("integrationsRedesign.decisionsEntry") }}</p>
    <RouterLink class="button button--primary" to="/decisions">
      <ShieldCheck :size="17" aria-hidden="true" />
      {{ t("integrationsRedesign.openDecisions") }}
    </RouterLink>
  </section>
</template>
<style scoped>
.approval-panel {
  padding: 20px;
  align-items: start;
}
</style>
