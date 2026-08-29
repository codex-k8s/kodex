<script setup lang="ts">
import { computed, watch } from "vue";

import {
  accessResourceKinds,
  accessScopeKinds,
  type ScopeDraft,
} from "@/features/access/model";
import type {
  AccessScopeKind,
  AccessResourceKind,
  Agent,
  IntegrationConnection,
  Project,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  modelValue: ScopeDraft;
  projects: Project[];
  agents: Agent[];
  workflows: Workflow[];
  integrations: IntegrationConnection[];
  allowedScopes?: AccessScopeKind[];
  allowedResourceKinds?: AccessResourceKind[];
  busy?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: ScopeDraft];
  "load-project-resources": [projectRef: string];
}>();

const scopes = computed(() =>
  accessScopeKinds.filter(
    (scope) => !props.allowedScopes || props.allowedScopes.includes(scope),
  ),
);
const resourceKinds = computed(() =>
  accessResourceKinds.filter(
    (kind) =>
      !props.allowedResourceKinds || props.allowedResourceKinds.includes(kind),
  ),
);
const pickerResourceKinds: AccessResourceKind[] = [
  "AGENT",
  "WORKFLOW",
  "INTEGRATION",
];
const usesPicker = computed(
  () =>
    props.modelValue.kind === "RESOURCE_INSTANCE" &&
    pickerResourceKinds.includes(props.modelValue.resourceKind),
);
const resourceOptions = computed(() => {
  if (props.modelValue.resourceKind === "AGENT") {
    return props.agents.map((agent) => ({
      ref: agent.ref,
      name: agent.name,
      description: agent.roleDescription,
    }));
  }
  if (props.modelValue.resourceKind === "WORKFLOW") {
    return props.workflows.map((workflow) => ({
      ref: workflow.ref,
      name: workflow.name,
      description: workflow.purpose,
    }));
  }
  if (props.modelValue.resourceKind === "INTEGRATION") {
    return props.integrations.map((integration) => ({
      ref: integration.ref,
      name: integration.name,
      description: integration.definitionKey,
    }));
  }
  return [];
});

function update(patch: Partial<ScopeDraft>): void {
  const next = { ...props.modelValue, ...patch };
  if (patch.kind === "ORGANIZATION") {
    next.projectRef = "";
    next.resourceKind = "ORGANIZATION";
    next.resourceRef = "";
  }
  if (patch.kind === "PROJECT") {
    next.resourceKind = "PROJECT";
    next.resourceRef = "";
  }
  if (patch.kind === "RESOURCE_KIND") next.resourceRef = "";
  if (patch.projectRef !== undefined) {
    next.resourceRef = "";
    if (patch.projectRef && ["AGENT", "WORKFLOW"].includes(next.resourceKind))
      emit("load-project-resources", patch.projectRef);
  }
  if (patch.resourceKind !== undefined) {
    next.resourceRef = "";
    if (["AGENT", "WORKFLOW"].includes(patch.resourceKind) && next.projectRef)
      emit("load-project-resources", next.projectRef);
  }
  emit("update:modelValue", next);
}

watch(
  [scopes, resourceKinds],
  () => {
    const patch: Partial<ScopeDraft> = {};
    if (!scopes.value.includes(props.modelValue.kind) && scopes.value[0])
      patch.kind = scopes.value[0];
    if (
      !resourceKinds.value.includes(props.modelValue.resourceKind) &&
      resourceKinds.value[0]
    )
      patch.resourceKind = resourceKinds.value[0];
    if (Object.keys(patch).length > 0) update(patch);
  },
  { immediate: true },
);
</script>

<template>
  <div class="scope-editor">
    <label class="field">
      <span>{{ $t("access.scope.kind") }}</span>
      <select
        :value="modelValue.kind"
        :disabled="busy"
        @change="
          update({
            kind: ($event.target as HTMLSelectElement)
              .value as ScopeDraft['kind'],
          })
        "
      >
        <option v-for="scope in scopes" :key="scope" :value="scope">
          {{ $t(`access.scope.values.${scope}`) }}
        </option>
      </select>
    </label>

    <label v-if="modelValue.kind !== 'ORGANIZATION'" class="field">
      <span>{{ $t("access.scope.project") }}</span>
      <select
        :value="modelValue.projectRef"
        required
        :disabled="busy"
        @change="
          update({ projectRef: ($event.target as HTMLSelectElement).value })
        "
      >
        <option value="" disabled>
          {{ $t("access.scope.chooseProject") }}
        </option>
        <option
          v-for="project in projects"
          :key="project.ref"
          :value="project.ref"
        >
          {{ project.name }}
        </option>
      </select>
    </label>

    <label
      v-if="['RESOURCE_KIND', 'RESOURCE_INSTANCE'].includes(modelValue.kind)"
      class="field"
    >
      <span>{{ $t("access.scope.resourceKind") }}</span>
      <select
        :value="modelValue.resourceKind"
        :disabled="busy"
        @change="
          update({
            resourceKind: ($event.target as HTMLSelectElement)
              .value as ScopeDraft['resourceKind'],
          })
        "
      >
        <option v-for="kind in resourceKinds" :key="kind" :value="kind">
          {{ $t(`access.resourceKinds.${kind}`) }}
        </option>
      </select>
    </label>

    <label v-if="usesPicker" class="field">
      <span>{{ $t(`access.resourceKinds.${modelValue.resourceKind}`) }}</span>
      <select
        :value="modelValue.resourceRef"
        required
        :disabled="busy || !modelValue.projectRef"
        @change="
          update({ resourceRef: ($event.target as HTMLSelectElement).value })
        "
      >
        <option value="" disabled>
          {{ $t("access.scope.chooseResource") }}
        </option>
        <option
          v-for="resource in resourceOptions"
          :key="resource.ref"
          :value="resource.ref"
        >
          {{ resource.name }} · {{ resource.description }}
        </option>
      </select>
      <small>{{ $t("access.scope.exactResourceHint") }}</small>
    </label>

    <label v-else-if="modelValue.kind === 'RESOURCE_INSTANCE'" class="field">
      <span>{{ $t("access.scope.resourceRef") }}</span>
      <input
        :value="modelValue.resourceRef"
        required
        :disabled="busy"
        autocomplete="off"
        :placeholder="$t('access.scope.resourceRefPlaceholder')"
        @input="
          update({ resourceRef: ($event.target as HTMLInputElement).value })
        "
      />
      <small>{{ $t("access.scope.resourceRefHint") }}</small>
    </label>

    <aside class="contract-boundary">
      <strong>{{ $t("access.scope.contractBoundary") }}</strong>
      <ul>
        <li>
          <span>{{ $t("access.scope.operationCondition") }}</span>
          <small>{{ $t("access.scope.operationConditionHint") }}</small>
        </li>
        <li class="unavailable">
          <span>{{ $t("access.scope.environmentCondition") }}</span>
          <small>{{ $t("access.scope.environmentUnavailable") }}</small>
        </li>
      </ul>
    </aside>
  </div>
</template>

<style scoped>
.scope-editor {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface-subtle, #f7f9fb);
}
.contract-boundary {
  grid-column: 1 / -1;
  padding-top: 10px;
  border-top: 1px solid var(--hairline);
}
.contract-boundary ul {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin: 8px 0 0;
  padding: 0;
  list-style: none;
}
.contract-boundary li {
  padding: 8px 10px;
  border-radius: 6px;
  background: var(--surface);
}
.contract-boundary li span,
.contract-boundary li small {
  display: block;
}
.contract-boundary li small {
  margin-top: 3px;
  color: var(--muted);
}
.contract-boundary li.unavailable {
  color: var(--warning);
  background: var(--warning-soft);
}
@media (max-width: 720px) {
  .scope-editor {
    grid-template-columns: 1fr;
  }
  .contract-boundary ul {
    grid-template-columns: 1fr;
  }
}
</style>
