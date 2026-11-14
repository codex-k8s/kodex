<script setup lang="ts">
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { useTelegramNotifications } from "./state";

const props = defineProps<{ projectRef: string }>();
const { t, locale } = useI18n();
function date(value: string) {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
const {
  subscription,
  ready,
  canManage,
  busy,
  problem,
  refreshRequired,
  saved,
  stale,
  mayWrite,
  valid,
  connections,
  connectionPage,
  deliveries,
  deliveryPage,
  connectionRef,
  hours,
  maxEffects,
  events,
  refresh,
  save,
  more,
} = useTelegramNotifications(() => props.projectRef);
const subscriptionState = {
  ACTIVE: "telegramNotifications.active",
  DISABLED: "telegramNotifications.disabled",
  EXPIRED: "telegramNotifications.expired",
  EXHAUSTED: "telegramNotifications.exhausted",
  SCOPE_REVOKED: "telegramNotifications.revoked",
} as const;
const deliveryState = {
  WAITING_APPROVAL: "telegramNotifications.waiting",
  DUE: "telegramNotifications.due",
  CLAIMED: "telegramNotifications.claimed",
  SUCCEEDED: "telegramNotifications.succeeded",
  FAILED: "telegramNotifications.deliveryFailed",
  CANCELLED: "telegramNotifications.cancelled",
  UNKNOWN_OUTCOME: "telegramNotifications.unknown",
} as const;
</script>

<template>
  <section
    class="telegram-notifications"
    aria-labelledby="telegram-notifications-title"
    :aria-busy="busy"
  >
    <h2 id="telegram-notifications-title">
      {{ t("telegramNotifications.title") }}
    </h2>
    <p>{{ t("telegramNotifications.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="busy && !ready" role="status">
      {{ t("telegramNotifications.loading") }}
    </p>
    <p v-if="refreshRequired" role="alert">
      {{ t("telegramNotifications.refreshRequired") }}
    </p>
    <p v-if="stale" role="alert">{{ t("telegramNotifications.stale") }}</p>
    <p v-if="saved" role="status">{{ t("telegramNotifications.saved") }}</p>
    <template v-if="ready">
      <div v-if="subscription" class="telegram-notifications__status">
        <strong>{{ t(subscriptionState[subscription.state]) }}</strong>
        <p>
          {{
            t("telegramNotifications.target", {
              bot: subscription.botId,
              chat: subscription.chatDisplay,
              topic: subscription.topicDisplay,
            })
          }}
        </p>
        <p>
          {{
            t("telegramNotifications.expiry", {
              expiry: date(subscription.expiresAt),
              used: subscription.usedEffects,
              max: subscription.maxEffects,
            })
          }}
        </p>
      </div>
      <p v-else>{{ t("telegramNotifications.absent") }}</p>
      <p v-if="!canManage">{{ t("telegramNotifications.readonly") }}</p>
      <form v-else @submit.prevent="save(true)">
        <fieldset :disabled="!mayWrite">
          <label>
            {{ t("telegramNotifications.connection") }}
            <select v-model="connectionRef" required>
              <option value="" disabled>
                {{ t("telegramNotifications.choose") }}
              </option>
              <option
                v-for="connection in connections"
                :key="connection.ref"
                :value="connection.ref"
              >
                {{ connection.name }}
              </option>
            </select>
          </label>
          <p v-if="connections.length === 0">
            {{ t("telegramNotifications.noConnections") }}
          </p>
          <fieldset>
            <legend>{{ t("telegramNotifications.events") }}</legend>
            <label
              ><input v-model="events" type="checkbox" value="FINISHED" />{{
                t("telegramNotifications.finished")
              }}</label
            >
            <label
              ><input v-model="events" type="checkbox" value="FAILED" />{{
                t("telegramNotifications.failed")
              }}</label
            >
            <label
              ><input v-model="events" type="checkbox" value="HUMAN_GATE" />{{
                t("telegramNotifications.humanGate")
              }}</label
            >
          </fieldset>
          <div class="telegram-notifications__limits">
            <label
              >{{ t("telegramNotifications.duration")
              }}<input
                v-model.number="hours"
                type="number"
                min="1"
                max="168"
                step="1"
                required
            /></label>
            <label
              >{{ t("telegramNotifications.limit")
              }}<input
                v-model.number="maxEffects"
                type="number"
                min="1"
                max="10000"
                step="1"
                required
            /></label>
          </div>
          <p>{{ t("telegramNotifications.scope") }}</p>
          <button
            class="button button--primary"
            type="submit"
            :disabled="!mayWrite || !valid"
          >
            {{
              subscription
                ? t("telegramNotifications.replace")
                : t("telegramNotifications.enable")
            }}
          </button>
          <button
            v-if="subscription?.enabled"
            class="button button--secondary"
            type="button"
            :disabled="!mayWrite"
            @click="save(false)"
          >
            {{ t("telegramNotifications.disable") }}
          </button>
        </fieldset>
      </form>
      <button
        v-if="canManage && connectionPage && connections.length < 100"
        class="button button--secondary"
        type="button"
        :disabled="busy"
        @click="more('connections')"
      >
        {{ t("telegramNotifications.moreConnections") }}
      </button>
      <RouterLink v-if="canManage" to="/integrations?definitionKey=telegram">{{
        t("telegramNotifications.setup")
      }}</RouterLink>
      <p>{{ t("telegramNotifications.quiet") }}</p>
      <h3>{{ t("telegramNotifications.history") }}</h3>
      <p v-if="deliveries.length === 0">
        {{ t("telegramNotifications.empty") }}
      </p>
      <ul v-else class="telegram-notifications__history">
        <li v-for="delivery in deliveries" :key="delivery.ref">
          <strong>{{ t(deliveryState[delivery.state]) }}</strong>
          <span
            >{{ date(delivery.createdAt) }} ·
            {{
              t("telegramNotifications.attempt", { attempt: delivery.attempt })
            }}</span
          >
          <RouterLink :to="`/projects/${projectRef}/runs/${delivery.runRef}`">{{
            t("telegramNotifications.run")
          }}</RouterLink>
          <p v-if="delivery.state === 'UNKNOWN_OUTCOME'">
            {{ t("telegramNotifications.unknownHelp") }}
          </p>
        </li>
      </ul>
      <button
        v-if="deliveryPage && deliveries.length < 100"
        class="button button--secondary"
        type="button"
        :disabled="busy"
        @click="more('deliveries')"
      >
        {{ t("telegramNotifications.more") }}
      </button>
      <p>{{ t("telegramNotifications.bounded") }}</p>
    </template>
    <button
      class="button button--secondary"
      type="button"
      :disabled="busy"
      @click="refresh()"
    >
      {{ t("telegramNotifications.refresh") }}
    </button>
  </section>
</template>

<style scoped>
.telegram-notifications {
  min-width: 0;
  padding: 1rem;
  border: 1px solid var(--color-border, #ddd);
  border-radius: 0.75rem;
  overflow-wrap: anywhere;
}
fieldset {
  border: 0;
  padding: 0;
  min-width: 0;
}
label {
  display: flex;
  gap: 0.5rem;
  margin-block: 0.5rem;
  flex-wrap: wrap;
  align-items: center;
}
select {
  max-width: 100%;
}
.telegram-notifications__limits {
  display: flex;
  flex-wrap: wrap;
  gap: 1rem;
}
.telegram-notifications__history {
  list-style: none;
  padding: 0;
}
.telegram-notifications__history li {
  display: grid;
  gap: 0.4rem;
  padding-block: 0.75rem;
  border-bottom: 1px solid var(--color-border, #ddd);
}
.button {
  margin: 0.25rem 0.5rem 0.25rem 0;
}
</style>
