<script setup lang="ts">
import { computed, ref } from "vue";
import { Plus, Trash2, Maximize2 } from "@lucide/vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import {
  emptyPackageField,
  resolvePackageField,
  type PackageFieldSchema,
} from "./integration-package";
const props = defineProps<{
  schema: PackageFieldSchema;
  modelValue: unknown;
  fieldKey: string;
  disabled?: boolean;
}>();
const emit = defineEmits<{ "update:modelValue": [value: unknown] }>();
const field = computed(() => resolvePackageField(props.schema));
const record = computed<Record<string, unknown>>(() =>
  props.modelValue &&
  typeof props.modelValue === "object" &&
  !Array.isArray(props.modelValue)
    ? (props.modelValue as Record<string, unknown>)
    : {},
);
const entries = computed<unknown[]>(() =>
  Array.isArray(props.modelValue) ? props.modelValue : [],
);
const expanded = ref(false);
const scalar = computed(() =>
  typeof props.modelValue === "string" || typeof props.modelValue === "number"
    ? props.modelValue
    : "",
);
function write(value: unknown): void {
  if (!props.disabled) emit("update:modelValue", value);
}
function updateProperty(key: string, value: unknown): void {
  write({ ...record.value, [key]: value });
}
function removeProperty(key: string): void {
  write(
    Object.fromEntries(
      Object.entries(record.value).filter(([name]) => name !== key),
    ),
  );
}
function updateEntry(index: number, value: unknown): void {
  write(
    entries.value.map((item, position) => (position === index ? value : item)),
  );
}
function updateInput(event: Event): void {
  if (
    !(
      event.target instanceof HTMLInputElement ||
      event.target instanceof HTMLSelectElement
    )
  )
    return;
  if (field.value.type === "integer") {
    const value = event.target.value === "" ? "" : Number(event.target.value);
    if (value === "" || Number.isSafeInteger(value)) write(value);
  } else write(event.target.value);
}
</script>
<template>
  <div v-if="field.type === 'object'" class="package-object">
    <template v-for="(child, key) in field.properties" :key="key">
      <div
        v-if="field.required?.includes(key) || record[key] !== undefined"
        class="package-property"
      >
        <div
          v-if="
            resolvePackageField(child).type === 'object' ||
            resolvePackageField(child).type === 'array'
          "
          class="package-heading"
        >
          <strong>{{ $t(`managed.packageFields.${key}`) }}</strong>
          <button
            v-if="!field.required?.includes(key)"
            type="button"
            class="icon-button"
            :disabled="disabled"
            :title="$t('common.delete')"
            :aria-label="$t('common.delete')"
            @click="removeProperty(key)"
          >
            <Trash2 :size="16" />
          </button>
        </div>
        <IntegrationPackageField
          :schema="child"
          :field-key="key"
          :model-value="record[key]"
          :disabled="disabled"
          @update:model-value="updateProperty(key, $event)"
        />
        <button
          v-if="
            !field.required?.includes(key) &&
            resolvePackageField(child).type !== 'object' &&
            resolvePackageField(child).type !== 'array'
          "
          type="button"
          class="icon-button"
          :disabled="disabled"
          :title="$t('common.delete')"
          :aria-label="$t('common.delete')"
          @click="removeProperty(key)"
        >
          <Trash2 :size="16" />
        </button>
      </div>
      <button
        v-else
        type="button"
        class="button package-add"
        :disabled="disabled"
        @click="updateProperty(key, emptyPackageField(child))"
      >
        <Plus :size="16" />{{ $t(`managed.packageFields.${key}`) }}
      </button>
    </template>
  </div>
  <component
    :is="expanded ? ModalDialog : 'div'"
    v-else-if="field.type === 'array'"
    :open="expanded || undefined"
    :title="$t(`managed.packageFields.${fieldKey}`)"
    size="xl"
    class="package-array"
    @close="expanded = false"
  >
    <div class="package-heading">
      <span>{{ entries.length }} / {{ field.maxItems }}</span>
      <button
        v-if="!expanded && entries.length > 6"
        type="button"
        class="icon-button"
        :title="$t('managed.expandFields')"
        :aria-label="$t('managed.expandFields')"
        @click="expanded = true"
      >
        <Maximize2 :size="16" />
      </button>
      <button
        type="button"
        class="icon-button"
        :disabled="disabled || entries.length >= (field.maxItems ?? 48)"
        :title="$t('managed.addField')"
        :aria-label="$t('managed.addField')"
        @click="write([...entries, emptyPackageField(field.items ?? {})])"
      >
        <Plus :size="16" />
      </button>
    </div>
    <ol class="package-entries">
      <li
        v-for="(entry, index) in expanded ? entries : entries.slice(0, 6)"
        :key="index"
      >
        <details
          v-if="resolvePackageField(field.items ?? {}).type === 'object'"
        >
          <summary>
            {{ $t(`managed.packageFields.${fieldKey}`) }} {{ index + 1 }}
          </summary>
          <IntegrationPackageField
            :schema="field.items ?? {}"
            :model-value="entry"
            :field-key="fieldKey"
            :disabled="disabled"
            @update:model-value="updateEntry(index, $event)"
          />
        </details>
        <IntegrationPackageField
          v-else
          :schema="field.items ?? {}"
          :model-value="entry"
          :field-key="fieldKey"
          :disabled="disabled"
          @update:model-value="updateEntry(index, $event)"
        />
        <button
          type="button"
          class="icon-button"
          :disabled="disabled"
          :title="$t('common.delete')"
          :aria-label="$t('common.delete')"
          @click="write(entries.filter((_, position) => position !== index))"
        >
          <Trash2 :size="16" />
        </button>
      </li>
    </ol>
  </component>
  <label v-else class="package-scalar">
    <span>{{ $t(`managed.packageFields.${fieldKey}`) }}</span>
    <input
      v-if="field.const !== undefined"
      :value="scalar"
      readonly
      :disabled="disabled"
    />
    <select
      v-else-if="field.enum"
      :value="scalar"
      :disabled="disabled"
      @change="updateInput"
    >
      <option v-if="!field.enum.includes(String(scalar))" :value="scalar">
        {{ scalar || $t("managed.selectField") }}
      </option>
      <option v-for="value in field.enum" :key="value" :value="value">
        {{ value }}
      </option>
    </select>
    <input
      v-else-if="field.type === 'boolean'"
      type="checkbox"
      :checked="modelValue === true"
      :disabled="disabled"
      @change="write(($event.target as HTMLInputElement).checked)"
    />
    <VoiceTextarea
      v-else-if="fieldKey === 'description'"
      :model-value="String(scalar)"
      :disabled="disabled"
      :maxlength="field.maxLength"
      rows="3"
      @update:model-value="write"
    />
    <input
      v-else
      :type="field.type === 'integer' ? 'number' : 'text'"
      :value="scalar"
      :disabled="disabled"
      :min="field.minimum"
      :max="field.maximum"
      :maxlength="field.maxLength"
      :pattern="field.pattern"
      :step="field.type === 'integer' ? 1 : undefined"
      @input="updateInput"
    />
  </label>
</template>
<style scoped>
.package-object,
.package-property,
.package-array {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.package-object {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.package-property:has(.package-object),
.package-property:has(.package-array) {
  grid-column: 1 / -1;
}
.package-heading {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-width: 0;
}
.package-heading strong {
  overflow-wrap: anywhere;
}
.package-scalar {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.package-scalar :is(input, select, textarea) {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.package-scalar input[type="checkbox"] {
  width: 18px;
  height: 18px;
}
.package-add {
  justify-self: start;
  max-width: 100%;
  white-space: normal;
}
.package-entries {
  display: grid;
  gap: 8px;
  padding: 0;
  margin: 0;
  list-style: none;
}
.package-entries > li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 36px;
  align-items: start;
  gap: 8px;
  border-bottom: 1px solid var(--border);
  padding: 8px 0;
}
.package-entries summary {
  cursor: pointer;
  padding: 8px 0;
  overflow-wrap: anywhere;
}
@media (max-width: 720px) {
  .package-object {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
