<script setup lang="ts">
import { useId } from "vue";
import { useI18n } from "vue-i18n";

defineProps<{ modelValue: string; disabled?: boolean }>();
const emit = defineEmits<{ "update:modelValue": [value: string] }>();
const id = `kubernetes-grant-namespace-${useId()}`;
const { t } = useI18n();
</script>

<template>
  <label class="field" :for="id">
    <span>{{ t("kubernetesGrants.namespaceLabel") }}</span>
    <input
      :id="id"
      :value="modelValue"
      :disabled="disabled"
      maxlength="63"
      autocomplete="off"
      autocapitalize="none"
      :placeholder="
        t('kubernetesGrants.namespacePlaceholder', { cluster: '@cluster' })
      "
      :aria-describedby="`${id}-help`"
      @input="
        emit('update:modelValue', ($event.target as HTMLInputElement).value)
      "
    />
    <small :id="`${id}-help`">{{
      t("kubernetesGrants.namespaceHelp", {
        cluster: "@cluster",
        allNamespaces: "@all-namespaces",
      })
    }}</small>
  </label>
</template>
