<script setup lang="ts">
import { X } from "@lucide/vue";

defineProps<{ open: boolean; title: string }>();
defineEmits<{ close: [] }>();
</script>

<template>
  <Teleport to="body">
    <div
      v-if="open"
      class="modal-backdrop"
      role="presentation"
      @mousedown.self="$emit('close')"
    >
      <section
        class="modal"
        role="dialog"
        aria-modal="true"
        :aria-label="title"
      >
        <header class="modal__header">
          <h2>{{ title }}</h2>
          <button
            class="icon-button"
            type="button"
            :aria-label="$t('common.close')"
            @click="$emit('close')"
          >
            <X :size="18" aria-hidden="true" />
          </button>
        </header>
        <div class="modal__body"><slot /></div>
      </section>
    </div>
  </Teleport>
</template>
