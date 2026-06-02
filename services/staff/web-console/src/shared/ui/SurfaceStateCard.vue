<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    icon: string;
    title: string;
    text: string;
    status: string;
    tone?: 'live' | 'waiting' | 'neutral';
  }>(),
  {
    tone: 'neutral',
  },
);

const color = computed(() => {
  switch (props.tone) {
    case 'live':
      return 'success';
    case 'waiting':
      return 'warning';
    default:
      return 'info';
  }
});
</script>

<template>
  <div class="surface-state-card">
    <div class="surface-state-card__icon">
      <v-icon :icon="icon" :color="color" size="26" />
    </div>
    <div class="surface-state-card__body">
      <div class="surface-state-card__topline">
        <div class="surface-state-card__title">{{ title }}</div>
        <v-chip :color="color" size="small" variant="tonal" label>{{ status }}</v-chip>
      </div>
      <div class="surface-state-card__text">{{ text }}</div>
    </div>
  </div>
</template>

<style scoped>
.surface-state-card {
  align-items: flex-start;
  border: 1px solid #e1e6ef;
  border-radius: 8px;
  display: flex;
  gap: 12px;
  padding: 14px;
}

.surface-state-card__icon {
  align-items: center;
  background: #f4f6f8;
  border-radius: 8px;
  display: inline-flex;
  height: 42px;
  justify-content: center;
  width: 42px;
}

.surface-state-card__body {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.surface-state-card__topline {
  align-items: center;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.surface-state-card__title {
  color: #121826;
  font-weight: 700;
}

.surface-state-card__text {
  color: #667085;
  font-size: 0.875rem;
  line-height: 1.45;
}
</style>
