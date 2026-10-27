<script setup lang="ts">
import type { IntegrationConfigurationField } from "@/shared/api/generated/openapi/types.gen";
import { useI18n } from "vue-i18n";
import IntegrationIntegerBounds from "./IntegrationIntegerBounds.vue";
import { integrationFieldTypeKey } from "./field-type";

// Описание схемы общее для подключения и входов capability; сами поля остаются
// неизменными server-owned данными. Компонент не редактирует и не исполняет их.
defineProps<{ fields: readonly IntegrationConfigurationField[] }>();
const { t } = useI18n();

function fieldType(field: IntegrationConfigurationField): string {
  return t(integrationFieldTypeKey(field.valueType));
}
</script>

<template>
  <dl class="field-schema">
    <div v-for="field in fields" :key="field.key">
      <dt>
        <strong>{{ field.label }}</strong>
        <code>{{ field.key }}</code>
      </dt>
      <dd>
        <span class="type-token">{{ fieldType(field) }}</span>
        <span>{{ field.required ? "обязательное" : "необязательное" }}</span>
        <span>{{ field.help }}</span>
        <IntegrationIntegerBounds :field="field" />
      </dd>
    </div>
  </dl>
</template>

<style scoped>
.field-schema {
  display: grid;
  gap: 6px;
  margin: 0;
}
.field-schema > div {
  display: grid;
  grid-template-columns: minmax(150px, 0.65fr) minmax(0, 1.35fr);
  gap: 10px;
  padding: 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--surface);
}
.field-schema dt,
.field-schema dd {
  display: grid;
  min-width: 0;
  gap: 3px;
  margin: 0;
}
.field-schema code,
.field-schema dd {
  overflow-wrap: anywhere;
  font-size: 0.72rem;
}
.field-schema dd {
  color: var(--muted);
}
.type-token {
  width: fit-content;
  padding: 2px 5px;
  border-radius: 5px;
  color: var(--accent-strong);
  background: var(--accent-soft);
  font-family: var(--font-mono);
}
@media (max-width: 700px) {
  .field-schema > div {
    grid-template-columns: 1fr;
  }
}
</style>
