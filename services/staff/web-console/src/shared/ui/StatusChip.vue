<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  label: string;
  tone?: 'neutral' | 'success' | 'warning' | 'error' | 'info';
}>();

const color = computed(() => {
  switch (props.tone) {
    case 'success':
      return 'success';
    case 'warning':
      return 'warning';
    case 'error':
      return 'error';
    case 'info':
      return 'info';
    default:
      return undefined;
  }
});
</script>

<template>
  <v-chip :color="color" size="small" variant="tonal" label>{{ label }}</v-chip>
</template>
