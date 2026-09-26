<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  accessAgentOptions,
  accessIntegrationOptions,
  accessProjectOptions,
  accessWorkflowOptions,
} from "@/features/access/entity-pickers";
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
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";

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
const i18n = useI18n();
const chosenProject = ref<AsyncEntityOption>();
const chosenResource = ref<AsyncEntityOption>();

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
const selectedProjectOption = computed<AsyncEntityOption | undefined>(() => {
  const project = props.projects.find(
    (item) => item.ref === props.modelValue.projectRef,
  );
  if (project)
    return {
      ref: project.ref,
      title: project.name,
      description: project.purpose,
    };
  return chosenProject.value?.ref === props.modelValue.projectRef
    ? chosenProject.value
    : undefined;
});
const selectedResourceOption = computed<AsyncEntityOption | undefined>(() => {
  if (props.modelValue.resourceKind === "AGENT") {
    const agent = props.agents.find(
      (item) => item.ref === props.modelValue.resourceRef,
    );
    if (agent)
      return {
        ref: agent.ref,
        title: agent.name,
        description: agent.roleDescription,
      };
  }
  if (props.modelValue.resourceKind === "WORKFLOW") {
    const workflow = props.workflows.find(
      (item) => item.ref === props.modelValue.resourceRef,
    );
    if (workflow)
      return {
        ref: workflow.ref,
        title: workflow.name,
        description: workflow.purpose,
      };
  }
  if (props.modelValue.resourceKind === "INTEGRATION") {
    const integration = props.integrations.find(
      (item) => item.ref === props.modelValue.resourceRef,
    );
    if (integration)
      return {
        ref: integration.ref,
        title: integration.name,
        description: integration.definitionKey,
      };
  }
  return chosenResource.value?.ref === props.modelValue.resourceRef
    ? chosenResource.value
    : undefined;
});

function selection(value: string | null | readonly string[]): string {
  return typeof value === "string" ? value : "";
}

function pickerLabels(label: string, searchPlaceholder: string) {
  return {
    label,
    searchPlaceholder,
    loading: i18n.t("common.loading"),
    loadingMore: i18n.t("common.loading"),
    empty: i18n.t("common.empty"),
    error: i18n.t("errors.default"),
    retry: i18n.t("common.retry"),
  };
}

function loadResources(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  if (props.modelValue.resourceKind === "AGENT")
    return accessAgentOptions(
      props.modelValue.projectRef,
      query,
      cursor,
      signal,
      pageSize,
    );
  if (props.modelValue.resourceKind === "WORKFLOW")
    return accessWorkflowOptions(
      props.modelValue.projectRef,
      query,
      cursor,
      signal,
      pageSize,
    );
  if (props.modelValue.resourceKind === "INTEGRATION")
    return accessIntegrationOptions(query, cursor, signal, pageSize);
  return Promise.resolve({ items: [] });
}

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
    chosenProject.value = undefined;
    chosenResource.value = undefined;
    next.resourceRef = "";
    if (patch.projectRef && ["AGENT", "WORKFLOW"].includes(next.resourceKind))
      emit("load-project-resources", patch.projectRef);
  }
  if (patch.resourceKind !== undefined) {
    chosenResource.value = undefined;
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
        name="access-scope-kind"
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

    <div v-if="modelValue.kind !== 'ORGANIZATION'" class="field">
      <span>{{ $t("access.scope.project") }}</span>
      <AsyncEntityPicker
        :model-value="modelValue.projectRef"
        :selected="selectedProjectOption"
        :load-page="accessProjectOptions"
        :labels="
          pickerLabels(
            $t('access.scope.project'),
            $t('access.scope.chooseProject'),
          )
        "
        :placeholder="$t('access.scope.chooseProject')"
        :trigger-label="$t('access.scope.project')"
        :clearable="false"
        :disabled="busy"
        @select="chosenProject = $event"
        @update:model-value="update({ projectRef: selection($event) })"
      />
    </div>

    <label
      v-if="['RESOURCE_KIND', 'RESOURCE_INSTANCE'].includes(modelValue.kind)"
      class="field"
    >
      <span>{{ $t("access.scope.resourceKind") }}</span>
      <select
        name="access-scope-resource-kind"
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

    <div v-if="usesPicker" class="field">
      <span>{{ $t(`access.resourceKinds.${modelValue.resourceKind}`) }}</span>
      <AsyncEntityPicker
        :model-value="modelValue.resourceRef"
        :selected="selectedResourceOption"
        :load-page="loadResources"
        :labels="
          pickerLabels(
            $t(`access.resourceKinds.${modelValue.resourceKind}`),
            $t('access.scope.chooseResource'),
          )
        "
        :context-key="`${modelValue.projectRef}:${modelValue.resourceKind}`"
        :placeholder="$t('access.scope.chooseResource')"
        :trigger-label="$t(`access.resourceKinds.${modelValue.resourceKind}`)"
        :clearable="false"
        :disabled="busy || !modelValue.projectRef"
        @select="chosenResource = $event"
        @update:model-value="update({ resourceRef: selection($event) })"
      />
      <small>{{ $t("access.scope.exactResourceHint") }}</small>
    </div>

    <label v-else-if="modelValue.kind === 'RESOURCE_INSTANCE'" class="field">
      <span>{{ $t("access.scope.resourceRef") }}</span>
      <input
        name="access-scope-resource-ref"
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
@media (max-width: 720px) {
  .scope-editor {
    grid-template-columns: 1fr;
  }
  .contract-boundary ul {
    grid-template-columns: 1fr;
  }
}
</style>
