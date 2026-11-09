<script setup lang="ts">
import { computed, watch, useId } from "vue";
import { useI18n } from "vue-i18n";
import type {
  IntegrationIntent,
  IntegrationApprovalConsentSelection,
} from "@/shared/api/generated/openapi/types.gen";
import {
  defaultIntegrationConsent,
  integrationConsentPolicy,
  validIntegrationConsent,
} from "../integration-approval-consent";
const props = defineProps<{ intent: IntegrationIntent; disabled?: boolean }>();
const model = defineModel<IntegrationApprovalConsentSelection>();
const policy = computed(() => integrationConsentPolicy(props.intent));
const { t } = useI18n();
const id = useId();
watch(
  policy,
  (value) => {
    if (value && !model.value) model.value = defaultIntegrationConsent(value);
  },
  { immediate: true },
);
const valid = computed(() =>
  validIntegrationConsent(props.intent, model.value),
);
</script>
<template>
  <fieldset v-if="policy && model" class="stack" :disabled="disabled">
    <legend>{{ t("integrationConsents.choiceTitle") }}</legend>
    <p class="muted">{{ t("integrationConsents.choiceHelp") }}</p>
    <label class="field" :for="`${id}-duration`"
      ><span>{{ t("integrationConsents.duration") }}</span>
      <input
        :id="`${id}-duration`"
        v-model.number="model.durationSeconds"
        type="number"
        min="1"
        :max="policy.maxDurationSeconds"
        step="1"
        required
      />
    </label>
    <label class="field" :for="`${id}-effects`"
      ><span>{{ t("integrationConsents.effects") }}</span>
      <input
        :id="`${id}-effects`"
        v-model.number="model.maxEffects"
        type="number"
        min="1"
        :max="policy.maxEffects"
        step="1"
        required
      />
    </label>
    <label class="field" :for="`${id}-lifetime`"
      ><span>{{ t("integrationConsents.lifetime") }}</span>
      <select :id="`${id}-lifetime`" v-model="model.lifetime">
        <option
          v-for="life in policy.allowedLifetimes"
          :key="life"
          :value="life"
        >
          {{ t(`integrationConsents.lifetimes.${life}`) }}
        </option>
      </select>
    </label>
    <p v-if="!valid" role="alert">{{ t("integrationConsents.invalid") }}</p>
  </fieldset>
</template>
