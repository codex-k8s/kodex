<script setup lang="ts">
import { useI18n } from "vue-i18n";
import {
  financialCredits,
  financialReset,
  type financialSummary,
} from "./financial-summary";

defineProps<{
  item: ReturnType<typeof financialSummary>["accounts"][number];
}>();
const { t, locale } = useI18n();
</script>

<template>
  <div class="financial-account">
    <span class="account-name" :title="item.account.name">{{
      item.account.name
    }}</span>
    <span>{{
      item.remaining === undefined
        ? item.stale && item.account.financialSummary
          ? t("providers.finance.stale")
          : t("providers.finance.limitUnknown")
        : t("providers.finance.remaining", { remaining: item.remaining })
    }}</span>
    <span :title="t('providers.finance.balanceUnit')">{{
      financialCredits(item.account, item.available, (key, params) =>
        t(`providers.finance.${key}`, params ?? {}),
      )
    }}</span>
    <span v-if="item.available && item.observation?.spendControlReached">{{
      t("providers.finance.spending")
    }}</span>
    <span
      v-if="
        item.available &&
        item.observation?.restriction &&
        item.observation.restriction !== 'rate_limit_reached'
      "
      >{{ t("providers.finance.restriction") }}</span
    >
    <span
      v-for="(window, index) in item.available
        ? (item.observation?.windows ?? [])
        : []"
      :key="index"
      class="window"
    >
      <small>{{
        t("providers.finance.window", {
          duration:
            window.durationMinutes === undefined
              ? t("providers.finance.durationUnknown")
              : t("providers.finance.minutes", {
                  duration: window.durationMinutes,
                }),
          remaining: 100 - window.usedPercent,
        })
      }}</small>
      <small v-if="financialReset(window.resetsAt, locale)">{{
        t("providers.finance.reset", {
          date: financialReset(window.resetsAt, locale),
        })
      }}</small>
    </span>
    <small v-if="item.account.financialSummary">{{
      t("providers.finance.observed", {
        date: new Date(item.account.financialSummary.receivedAt).toLocaleString(
          locale,
        ),
      })
    }}</small>
  </div>
</template>

<style scoped>
.financial-account {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.65rem;
  min-width: 0;
  overflow-wrap: anywhere;
}
.account-name {
  max-width: 12rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 600;
}
.window {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.65rem;
}
small {
  color: var(--muted);
}
</style>
