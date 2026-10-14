<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useProvidersStore } from "./store";
import {
  financialSummary,
  financialCredits,
  financialReset,
} from "./financial-summary";

const { t, locale } = useI18n();
const props = defineProps<{ live: boolean }>();
const providers = useProvidersStore();
const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  clock = setInterval(() => {
    now.value = Date.now();
  }, 30_000);
});
onBeforeUnmount(() => {
  if (clock) clearInterval(clock);
});
const summary = computed(() =>
  financialSummary(
    providers.financialAccounts,
    props.live,
    providers.financialComplete,
    now.value,
    providers.financialOverview,
  ),
);
</script>

<template>
  <section class="provider-finance" :aria-label="t('providers.finance.label')">
    <strong>Codex</strong>
    <span v-if="!summary.total">{{ t("providers.finance.unavailable") }}</span>
    <ul v-else>
      <li
        v-for="item in summary.accounts"
        :key="item.account.ref"
        :class="{ exhausted: item.remaining === 0 }"
      >
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
          v-for="(window, index) in item.available && item.remaining !== 0
            ? (item.observation?.windows ?? [])
            : []"
          :key="index"
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
        <small v-if="item.account.financialSummary && item.remaining !== 0">{{
          t("providers.finance.observed", {
            date: new Date(
              item.account.financialSummary.receivedAt,
            ).toLocaleString(locale),
          })
        }}</small>
      </li>
    </ul>
    <small v-if="summary.exhausted">{{
      t("providers.finance.exhausted", { count: summary.exhausted })
    }}</small>
    <small v-if="summary.unknown || !summary.complete">{{
      t("providers.finance.incomplete")
    }}</small>
  </section>
</template>

<style scoped>
.provider-finance {
  position: sticky;
  top: var(--topbar-height);
  z-index: 20;
  background: var(--surface);
  flex: 0 0 auto;
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1rem;
  padding: 0.65rem 1rem;
  border-bottom: 1px solid var(--border);
  font-size: 0.8rem;
}
.provider-finance ul {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1.5rem;
  padding: 0;
  margin: 0;
  list-style: none;
}
.provider-finance li {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.65rem;
  min-width: 0;
}
.account-name {
  max-width: 12rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 600;
}
.provider-finance small {
  color: var(--muted);
}
@media (max-width: 640px) {
  .provider-finance ul {
    width: 100%;
    display: grid;
  }
  .provider-finance li {
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.35rem;
  }
}
</style>
