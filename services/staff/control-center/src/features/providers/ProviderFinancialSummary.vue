<script setup lang="ts">
import { useI18n } from "vue-i18n";
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useProvidersStore } from "./store";
import ProviderFinancialAccount from "./ProviderFinancialAccount.vue";
import { financialSummary, financialCredits } from "./financial-summary";

const { t } = useI18n();
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
  if (clock !== undefined) clearInterval(clock);
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
const exhausted = computed(() =>
  summary.value.accounts.filter((item) => item.remaining === 0),
);
const ordinary = computed(() =>
  summary.value.accounts.filter((item) => item.remaining !== 0),
);
</script>

<template>
  <section class="provider-finance" :aria-label="t('providers.finance.label')">
    <strong>Codex</strong>
    <span v-if="!summary.total">{{ t("providers.finance.unavailable") }}</span>
    <ul v-else>
      <li v-if="exhausted.length" class="exhausted-group">
        <details>
          <summary>
            <span>{{
              t("providers.finance.exhausted", { count: summary.exhausted })
            }}</span>
            <span
              v-for="item in exhausted"
              :key="item.account.ref"
              class="credit-chip"
              :title="
                item.account.name +
                ': ' +
                financialCredits(item.account, item.available, (key, params) =>
                  t(`providers.finance.${key}`, params ?? {}),
                )
              "
            >
              {{ item.account.name }}:
              {{
                financialCredits(item.account, item.available, (key, params) =>
                  t(`providers.finance.${key}`, params ?? {}),
                )
              }}
            </span>
          </summary>
          <div class="exhausted-accounts">
            <ProviderFinancialAccount
              v-for="item in exhausted"
              :key="item.account.ref"
              :item="item"
            />
          </div>
        </details>
      </li>
      <li v-for="item in ordinary" :key="item.account.ref">
        <ProviderFinancialAccount :item="item" />
      </li>
    </ul>
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
.exhausted-group summary {
  cursor: pointer;
}
.credit-chip {
  display: inline-block;
  max-width: 16rem;
  margin-left: 0.5rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.exhausted-accounts {
  display: grid;
  gap: 0.5rem;
  padding-top: 0.5rem;
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
