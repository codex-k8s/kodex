<script setup lang="ts">
import { onMounted, ref } from "vue";

defineProps<{ title: string; busy?: boolean }>();
const emit = defineEmits<{ close: [] }>();
const panel = ref<HTMLElement>();

onMounted(() => panel.value?.focus());
</script>

<template>
  <div
    class="modal-backdrop"
    role="presentation"
    @mousedown.self="!busy && emit('close')"
  >
    <section
      ref="panel"
      class="modal"
      role="dialog"
      aria-modal="true"
      :aria-label="title"
      tabindex="-1"
      @keydown.esc="!busy && emit('close')"
    >
      <header class="modal__header">
        <h2>{{ title }}</h2>
        <button
          class="icon-button"
          type="button"
          :aria-label="$t('common.close')"
          :disabled="busy"
          @click="emit('close')"
        >
          ×
        </button>
      </header>
      <div class="modal__body"><slot /></div>
      <footer class="modal__footer"><slot name="actions" /></footer>
    </section>
  </div>
</template>
