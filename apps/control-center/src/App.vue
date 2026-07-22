<script setup lang="ts">
import AutomationHistory from "./features/automation-history/AutomationHistory.vue";
import { parseAutomationCallbackReceipts } from "./features/automation-history/contract";

const history = parseAutomationCallbackReceipts(
  window.__MATTERCODEX_AUTOMATION_HISTORY__ ?? [],
);
</script>

<template>
  <main class="shell">
    <header class="masthead">
      <div>
        <p class="eyebrow">MatterCodex / Control Center</p>
        <h1>История автоматизаций</h1>
      </div>
      <p class="masthead__note">
        Только серверные состояния сохранённых ScheduledRun
      </p>
    </header>
    <AutomationHistory :items="history" />
  </main>
</template>
