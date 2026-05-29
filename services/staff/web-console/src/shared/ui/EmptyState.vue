<script setup lang="ts">
defineProps<{
  icon: string;
  title: string;
  text?: string;
}>();
</script>

<template>
  <div class="empty-state">
    <v-icon :icon="icon" size="34" color="primary" />
    <div>
      <div class="empty-state__title">{{ title }}</div>
      <div v-if="text" class="empty-state__text">{{ text }}</div>
    </div>
    <slot />
  </div>
</template>

<style scoped>
.empty-state {
  align-items: center;
  border: 1px dashed #cfd8e6;
  border-radius: 12px;
  color: #475467;
  display: flex;
  gap: 14px;
  min-height: 96px;
  padding: 18px;
}

.empty-state__title {
  color: #121826;
  font-weight: 700;
}

.empty-state__text {
  color: #667085;
  font-size: 0.875rem;
  margin-top: 4px;
}
</style>
