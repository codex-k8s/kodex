<script setup lang="ts">
import OnboardingGuide from "@/features/onboarding/components/OnboardingGuide.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
</script>
<template>
  <PageFrame
    :title="$t('onboarding.title')"
    :subtitle="$t('onboarding.subtitle')"
  >
    <OnboardingGuide />
  </PageFrame>
</template>
