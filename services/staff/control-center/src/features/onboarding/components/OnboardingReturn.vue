<script setup lang="ts">
import { ArrowLeft } from "@lucide/vue";
import { watch } from "vue";
import { useRoute } from "vue-router";
import { useOnboardingStore } from "../store";
const guide = useOnboardingStore();
const route = useRoute();
watch(
  () => route.params.projectRef,
  (value) => {
    if (guide.active && typeof value === "string") guide.selectProject(value);
  },
  { immediate: true },
);
</script>
<template>
  <div v-if="guide.active" class="onboarding-return">
    <RouterLink :to="guide.returnTo"
      ><ArrowLeft :size="16" aria-hidden="true" />{{
        $t("onboarding.backToSetup")
      }}</RouterLink
    >
    <span>{{ $t(`onboarding.steps.${guide.step}.title`) }}</span>
  </div>
</template>
<style scoped>
.onboarding-return {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 18px;
  padding: 10px 14px;
  background: var(--accent-soft);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.onboarding-return a {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
  color: var(--accent);
  text-decoration: none;
}
.onboarding-return span {
  color: var(--text-secondary);
  font-size: 12px;
}
</style>
