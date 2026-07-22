<script setup lang="ts">
import { computed } from "vue";

import type { AutomationCallbackReceipt } from "./contract";

const props = defineProps<{
  items: readonly AutomationCallbackReceipt[];
}>();

const orderedItems = computed(() => [...props.items].reverse());

function stateLabel(item: AutomationCallbackReceipt): string {
  if (item.status === "waiting_owner") return "Ожидается решение владельца";
  if (item.status === "succeeded" && item.outcome === "requires_human")
    return "Решение принято";
  if (item.status === "succeeded") return "Завершено успешно";
  if (item.status === "failed") return "Завершено с ошибкой";
  if (item.status === "running") return "Выполняется";
  return "В очереди";
}

function nextActionLabel(item: AutomationCallbackReceipt): string {
  if (item.next_action === "retry_same_callback")
    return "Повторить тот же callback для восстановления карточки";
  if (item.next_action === "wait_for_owner_response")
    return "Ответить в точном треде запуска";
  if (item.next_action === "none") return "Действий не требуется";
  return "Ожидать обновления сервера";
}
</script>

<template>
  <section class="history" aria-labelledby="history-title">
    <div class="history__heading">
      <div>
        <p class="eyebrow">ScheduledRun</p>
        <h2 id="history-title">Сохранённые состояния</h2>
      </div>
      <span class="history__count" :aria-label="`Запусков: ${items.length}`">{{
        items.length
      }}</span>
    </div>

    <p v-if="orderedItems.length === 0" class="empty" role="status">
      История появится после принятого callback автоматизации.
    </p>

    <ol v-else class="timeline">
      <li
        v-for="item in orderedItems"
        :key="item.schedule_run_id"
        class="run"
        :data-state="item.status"
      >
        <div class="run__rail" aria-hidden="true"><span /></div>
        <article>
          <div class="run__topline">
            <span class="run__state">{{ stateLabel(item) }}</span>
            <span v-if="item.duplicate" class="run__replay">точный replay</span>
          </div>
          <code>{{ item.schedule_run_id }}</code>
          <dl>
            <div>
              <dt>Итог</dt>
              <dd>{{ item.outcome || "ожидается" }}</dd>
            </div>
            <div v-if="item.owner_attention_id">
              <dt>Owner attention</dt>
              <dd>
                #{{ item.owner_attention_id }} ·
                {{ item.human_decision_status }}
              </dd>
            </div>
            <div>
              <dt>Следующее действие</dt>
              <dd>{{ nextActionLabel(item) }}</dd>
            </div>
          </dl>
        </article>
      </li>
    </ol>
  </section>
</template>
