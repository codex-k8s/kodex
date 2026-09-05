<script setup lang="ts">
import { ref } from "vue";
import ProviderModelSelector from "../../src/features/providers/ProviderModelSelector.vue";
const model = ref("model-current");
const secondary = ref(false);
const available = ref(false);
</script>
<template>
  <main style="max-width: 640px; padding: 16px; margin: auto">
    <label
      ><input v-model="secondary" type="checkbox" />Вторая учётная запись</label
    >
    <ProviderModelSelector
      v-model="model"
      definition-key="openai-codex"
      :account-refs="
        secondary ? ['pacc_primary', 'pacc_secondary'] : ['pacc_primary']
      "
      @availability-change="available = $event"
    />
    <output data-testid="model">{{ model }}</output>
    <button :disabled="!available">Сохранить</button>
  </main>
</template>
