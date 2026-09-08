<script setup lang="ts">
import { ref, type Directive } from "vue";
import ModalDialog from "../../src/shared/ui/ModalDialog.vue";
import schema from "../../src/shared/api/generated/integration-package/schema.json";
const open = ref(false);
const chooseOwner = ref(false);
const vChooseFocus: Directive<HTMLInputElement, boolean> = {
  mounted(element, binding) {
    if (binding.value) element.focus();
  },
};
</script>
<template>
  <button
    type="button"
    @click="
      chooseOwner = false;
      open = true;
    "
  >
    Открыть
  </button>
  <button
    type="button"
    @click="
      chooseOwner = true;
      open = true;
    "
  >
    Открыть с выбором поля
  </button>
  <ModalDialog v-if="open" title="Подключение" @close="open = false">
    <label>Название<input data-dialog-initial-focus value="GitHub" /></label>
    <label>Организация<input v-choose-focus="chooseOwner" /></label>
    <label>Ключ<input :pattern="schema.$defs.key.pattern" /></label>
    <label
      >Hostname<input
        :pattern="schema.$defs.networkDestination.properties.hostname.pattern"
    /></label>
    <label
      >Версия<input
        :pattern="schema.properties.metadata.properties.version.pattern"
    /></label>
    <template #actions
      ><button type="button" @click="open = false">Готово</button></template
    >
  </ModalDialog>
</template>
