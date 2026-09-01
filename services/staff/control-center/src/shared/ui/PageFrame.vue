<script setup lang="ts">
defineProps<{ title: string; subtitle?: string; eyebrow?: string }>();
</script>

<template>
  <main class="page-frame" tabindex="-1">
    <header class="page-header">
      <div class="page-header__copy">
        <p v-if="eyebrow" class="eyebrow">{{ eyebrow }}</p>
        <h1 :title="title">{{ title }}</h1>
        <p v-if="subtitle" class="page-header__subtitle">{{ subtitle }}</p>
      </div>
      <div class="page-header__actions"><slot name="actions" /></div>
    </header>
    <slot />
  </main>
</template>

<style scoped>
.page-frame.page-frame {
  width: 100%;
  min-width: 0;
  max-width: none;
  margin: 0;
}
.page-header__copy {
  min-width: 0;
  flex: 1 1 auto;
}
.page-header__actions {
  flex: 0 1 auto;
}
@media (max-width: 760px) {
  .page-header {
    align-items: stretch;
    flex-direction: column;
  }
  .page-header__actions {
    justify-content: flex-start;
  }
}
</style>
