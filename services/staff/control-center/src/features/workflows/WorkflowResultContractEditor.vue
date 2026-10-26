<script setup lang="ts">
import { computed, useId } from "vue";
import {
  workflowItem,
  workflowResultKinds,
  newWorkflowStepKey,
} from "./result-contract";

const props = defineProps<{
  stepKey: string;
  dependencies: unknown;
  requirements: unknown;
  predecessors: { key: string; name: string }[];
  disabled: boolean;
}>();
const emit = defineEmits<{
  key: [value: string];
  dependencies: [value: string[]];
  requirements: [value: Record<string, unknown>[]];
}>();
const prefix = `workflow-results-${useId()}`;
const control = (index: number, key: string) =>
  `${prefix}-${String(index)}-${key}`;
const text = (value: unknown): string =>
  typeof value === "string" ? value : "";
const deps = computed(() =>
  Array.isArray(props.dependencies) &&
  props.dependencies.every((v) => typeof v === "string")
    ? props.dependencies
    : [],
);
const items = computed(() =>
  Array.isArray(props.requirements) && props.requirements.every(workflowItem)
    ? props.requirements
    : undefined,
);
const dependencyChoices = computed(() => [
  ...props.predecessors,
  ...deps.value
    .filter((key) => !props.predecessors.some((item) => item.key === key))
    .map((key) => ({ key, name: "" })),
]);
function change(index: number, key: string, value: string): void {
  if (props.disabled || !items.value?.[index]) return;
  emit(
    "requirements",
    items.value.map((item, position) => {
      if (position !== index) return { ...item };
      if (key !== "kind") return { ...item, [key]: value };
      // Смена типа удаляет только поля прежнего типа, сохраняя ключ условия.
      const next: Record<string, unknown> = { key: item.key, kind: value };
      if (value === "ARTIFACT")
        Object.assign(next, { fileName: "", mediaType: "text/markdown" });
      if (["PULL_REQUEST", "INTEGRATION_RECEIPT", "DEPLOYMENT"].includes(value))
        next.repository = "";
      if (["REVIEW", "QA", "INTEGRATION_RECEIPT", "DEPLOYMENT"].includes(value))
        next.subjectStepKey = "";
      if (value === "INTEGRATION_RECEIPT")
        next.operation = "github.pull_request.merge";
      if (value === "DEPLOYMENT")
        Object.assign(next, { operation: "ssh.compose.deploy", targetRef: "" });
      return next;
    }),
  );
}
function add(): void {
  if (props.disabled || !items.value || items.value.length >= 4) return;
  emit("requirements", [
    ...items.value.map((item) => ({ ...item })),
    {
      key: newWorkflowStepKey(items.value.map((item) => item.key)),
      kind: "ARTIFACT",
      fileName: "",
      mediaType: "text/markdown",
    },
  ]);
}
function remove(index: number): void {
  if (!props.disabled && items.value)
    emit(
      "requirements",
      items.value
        .filter((_, position) => position !== index)
        .map((item) => ({ ...item })),
    );
}
function selectDependencies(event: Event): void {
  if (props.disabled || !(event.target instanceof HTMLSelectElement)) return;
  emit(
    "dependencies",
    Array.from(event.target.selectedOptions, (option) => option.value),
  );
}
</script>

<template>
  <fieldset :disabled="disabled" class="workflow-result-contract">
    <legend>{{ $t("workflowResults.title") }}</legend>
    <label class="field">
      <span>{{ $t("workflowResults.key") }}</span>
      <input
        :id="`${prefix}-key`"
        :name="`${prefix}-key`"
        :value="stepKey"
        maxlength="96"
        @input="
          !disabled && emit('key', ($event.target as HTMLInputElement).value)
        "
      />
    </label>
    <label class="field">
      <span>{{ $t("workflowResults.dependencies") }}</span>
      <select
        :id="`${prefix}-dependencies`"
        :name="`${prefix}-dependencies`"
        multiple
        :value="deps"
        @change="selectDependencies"
      >
        <option
          v-for="prior in dependencyChoices"
          :key="prior.key"
          :value="prior.key"
        >
          {{ prior.name || $t("workflowResults.unknownDependency") }} ·
          {{ prior.key }}
        </option>
      </select>
    </label>
    <p>{{ $t("workflowResults.help") }}</p>
    <p v-if="!items" class="field-error" role="status">
      {{ $t("workflowResults.invalid") }}
    </p>
    <fieldset v-for="(item, index) in items ?? []" :key="index">
      <legend>{{ $t("workflowResults.kind") }} {{ index + 1 }}</legend>
      <label class="field"
        ><span>{{ $t("workflowResults.resultKey") }}</span
        ><input
          :id="control(index, 'key')"
          :name="control(index, 'key')"
          :value="text(item.key)"
          maxlength="96"
          @input="
            change(index, 'key', ($event.target as HTMLInputElement).value)
          "
      /></label>
      <label class="field"
        ><span>{{ $t("workflowResults.kind") }}</span
        ><select
          :id="control(index, 'kind')"
          :name="control(index, 'kind')"
          :value="text(item.kind)"
          @change="
            change(index, 'kind', ($event.target as HTMLSelectElement).value)
          "
        >
          <option v-for="kind in workflowResultKinds" :key="kind" :value="kind">
            {{ $t(`workflowResults.kinds.${kind}`) }}
          </option>
        </select></label
      >
      <template v-if="item.kind === 'ARTIFACT'">
        <label
          v-for="field in ['fileName', 'mediaType']"
          :key="field"
          class="field"
          ><span>{{ $t(`workflowResults.${field}`) }}</span
          ><input
            :id="control(index, field)"
            :name="control(index, field)"
            :value="text(item[field])"
            maxlength="160"
            @input="
              change(index, field, ($event.target as HTMLInputElement).value)
            "
        /></label>
      </template>
      <label
        v-if="
          ['PULL_REQUEST', 'INTEGRATION_RECEIPT', 'DEPLOYMENT'].includes(
            text(item.kind),
          )
        "
        class="field"
        ><span>{{ $t("workflowResults.repository") }}</span
        ><input
          :id="control(index, 'repository')"
          :name="control(index, 'repository')"
          :value="text(item.repository)"
          maxlength="200"
          @input="
            change(
              index,
              'repository',
              ($event.target as HTMLInputElement).value,
            )
          "
      /></label>
      <label
        v-if="
          ['REVIEW', 'QA', 'INTEGRATION_RECEIPT', 'DEPLOYMENT'].includes(
            text(item.kind),
          )
        "
        class="field"
        ><span>{{ $t("workflowResults.subject") }}</span
        ><select
          :id="control(index, 'subject')"
          :name="control(index, 'subject')"
          :value="text(item.subjectStepKey)"
          @change="
            change(
              index,
              'subjectStepKey',
              ($event.target as HTMLSelectElement).value,
            )
          "
        >
          <option value="">{{ $t("workflowResults.missing") }}</option>
          <option v-for="key in deps" :key="key" :value="key">
            {{ predecessors.find((prior) => prior.key === key)?.name || key }}
          </option>
        </select></label
      >
      <label v-if="item.kind === 'DEPLOYMENT'" class="field"
        ><span>{{ $t("workflowResults.target") }}</span
        ><input
          :id="control(index, 'target')"
          :name="control(index, 'target')"
          :value="text(item.targetRef)"
          maxlength="96"
          @input="
            change(
              index,
              'targetRef',
              ($event.target as HTMLInputElement).value,
            )
          "
      /></label>
      <button class="button" type="button" @click="remove(index)">
        {{ $t("workflowResults.remove") }}
      </button>
    </fieldset>
    <button
      class="button"
      type="button"
      :disabled="disabled || !items || items.length >= 4"
      @click="add"
    >
      {{ $t("workflowResults.add") }}
    </button>
  </fieldset>
</template>

<style scoped>
.workflow-result-contract,
.workflow-result-contract fieldset {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.workflow-result-contract select,
.workflow-result-contract input {
  min-width: 0;
  max-width: 100%;
}
</style>
