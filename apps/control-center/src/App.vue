<script setup lang="ts">
import { onUnmounted, ref } from "vue";

import AutomationHistory from "./features/automation-history/AutomationHistory.vue";
import { listAutomationHistory } from "./generated/sdk.gen";
import type { AutomationHistoryItem } from "./generated/types.gen";

const automationHistoryRefreshIntervalMs = 5_000;

const history = ref<AutomationHistoryItem[]>([]);
const readToken = ref("");
const loading = ref(false);
const connected = ref(false);
const errorMessage = ref("");
let refreshTimer: ReturnType<typeof setInterval> | undefined;
let activeReadToken = "";

async function connect(): Promise<void> {
  const token = readToken.value.trim();
  if (token) activeReadToken = token;
  readToken.value = "";
  if (!activeReadToken) {
    errorMessage.value = "Введите read-only token.";
    return;
  }
  if (refreshTimer !== undefined) clearInterval(refreshTimer);
  connected.value = true;
  await refresh();
  refreshTimer = setInterval(() => void refresh(), automationHistoryRefreshIntervalMs);
}

async function refresh(): Promise<void> {
  loading.value = true;
  errorMessage.value = "";
  try {
    const response = await listAutomationHistory({
      auth: activeReadToken,
      query: { limit: 100 },
    });
    if (response.error || !response.data) {
      errorMessage.value = "Сервер не вернул историю автоматизаций.";
      return;
    }
    history.value = response.data.items;
  } catch {
    errorMessage.value = "Не удалось загрузить историю автоматизаций.";
  } finally {
    loading.value = false;
  }
}

onUnmounted(() => {
  if (refreshTimer !== undefined) clearInterval(refreshTimer);
  activeReadToken = "";
});
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
    <form class="history-access" @submit.prevent="connect">
      <label for="history-read-token">Read-only token</label>
      <div class="history-access__controls">
        <input
          id="history-read-token"
          v-model="readToken"
          type="password"
          autocomplete="off"
          spellcheck="false"
          :disabled="loading"
        />
        <button type="submit" :disabled="loading">
          {{ connected ? "Обновить" : "Подключить" }}
        </button>
      </div>
      <p class="history-access__hint">
        Token хранится только в памяти открытой вкладки.
      </p>
    </form>
    <p v-if="loading" class="history-status" role="status">Загрузка…</p>
    <p v-else-if="errorMessage" class="history-status history-status--error" role="alert">
      {{ errorMessage }}
    </p>
    <AutomationHistory :items="history" />
  </main>
</template>
