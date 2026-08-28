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
  Project,
} from "@/shared/api/generated/openapi/types.gen";

const props = defineProps<{
  modelValue: ScopeDraft;
  projects: Project[];
  agents: Agent[];
  allowedScopes?: AccessScopeKind[];
  allowedResourceKinds?: AccessResourceKind[];
  busy?: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: ScopeDraft];
  "load-agents": [projectRef: string];
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
    if (patch.projectRef && next.resourceKind === "AGENT")
      emit("load-agents", patch.projectRef);
  }
  if (patch.resourceKind !== undefined) {
    next.resourceRef = "";
    if (patch.resourceKind === "AGENT" && next.projectRef)
      emit("load-agents", next.projectRef);
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

    <label
      v-if="
        modelValue.kind === 'RESOURCE_INSTANCE' &&
        modelValue.resourceKind === 'AGENT'
      "
      class="field"
    >
      <span>{{ $t("access.scope.agent") }}</span>
      <select
        :value="modelValue.resourceRef"
        required
        :disabled="busy || !modelValue.projectRef"
        @change="
          update({ resourceRef: ($event.target as HTMLSelectElement).value })
        "
      >
        <option value="" disabled>{{ $t("access.scope.chooseAgent") }}</option>
        <option v-for="agent in agents" :key="agent.ref" :value="agent.ref">
          {{ agent.name }} · {{ agent.roleDescription }}
        </option>
      </select>
      <small>{{ $t("access.scope.agentHint") }}</small>
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
@media (max-width: 720px) {
  .scope-editor {
    grid-template-columns: 1fr;
  }
}
</style>
