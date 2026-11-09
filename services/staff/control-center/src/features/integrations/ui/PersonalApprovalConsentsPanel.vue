<script setup lang="ts">
import { onMounted, onBeforeUnmount, useId } from "vue";
import { useI18n } from "vue-i18n";
import { usePersonalApprovalConsents } from "../personal-approval-consents";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import SafeStructuredData from "@/shared/ui/SafeStructuredData.vue";
const model = usePersonalApprovalConsents();
const { items, nextPageToken, loading, busyRef, problem } = model;
const { t } = useI18n();
const id = useId();
onMounted(() => {
  window.addEventListener("pagehide", model.suspend);
  void model.load();
});
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
function values(raw: string): unknown {
  return JSON.parse(raw);
}
</script>
<template>
  <section class="stack" :aria-labelledby="`${id}-title`">
    <header>
      <h2 :id="`${id}-title`">{{ t("integrationConsents.title") }}</h2>
      <p>{{ t("integrationConsents.description") }}</p>
      <button
        class="button"
        type="button"
        :disabled="loading || !!busyRef"
        @click="model.load()"
      >
        {{ t("integrationConsents.refresh") }}
      </button>
    </header>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="!loading && !problem && !items.length">
      {{ t("integrationConsents.empty") }}
    </p>
    <article
      v-for="item in items"
      :key="item.ref"
      class="card stack"
      :data-consent-ref="item.ref"
    >
      <h3>{{ item.connectionName }} · {{ item.capabilityKey }}</h3>
      <p>
        {{ t(`integrationConsents.states.${item.state}`) }} ·
        {{ t(`integrationConsents.lifetimes.${item.lifetime}`) }}
      </p>
      <SafeStructuredData :value="values(item.scopeValuesJSON)" literal />
      <p>
        {{
          t("integrationConsents.remaining", {
            used: item.reservedEffects,
            total: item.maxEffects,
          })
        }}
        ·
        {{
          t("integrationConsents.expires", {
            at: new Date(item.expiresAt).toLocaleString(),
          })
        }}
      </p>
      <button
        v-if="item.state !== 'REVOKED'"
        class="button"
        type="button"
        :disabled="loading || !!busyRef || !!problem"
        @click="model.revoke(item)"
      >
        {{ t("integrationConsents.revoke") }}
      </button>
    </article>
    <button
      v-if="nextPageToken"
      class="button"
      type="button"
      :disabled="loading || !!busyRef"
      @click="model.load(true)"
    >
      {{ t("integrationConsents.more") }}
    </button>
  </section>
</template>
