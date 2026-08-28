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
