<script setup lang="ts">
import { computed, reactive, watch } from "vue";

import AccessScopeEditor from "@/features/access/components/AccessScopeEditor.vue";
import {
  emptyBindingDraft,
  scopeToDraft,
  toBindingInput,
  validScope,
  type BindingDraft,
} from "@/features/access/model";
import type {
  AccessBinding,
  AccessBindingChangeInput,
  AccessBindingInput,
  AccessRole,
  AccessSubject,
  Agent,
  PermissionDefinition,
  Project,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = defineProps<{
  binding?: AccessBinding;
  initialSubject?: AccessSubject;
  defaultProjectRef?: string;
  subjects: AccessSubject[];
  roles: AccessRole[];
  permissions: PermissionDefinition[];
  projects: Project[];
  agents: Agent[];
  busy?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  save: [input: AccessBindingInput | AccessBindingChangeInput];
  "load-agents": [projectRef: string];
}>();

const form = reactive<BindingDraft>(emptyBindingDraft());
const selectedRole = computed(() =>
  props.roles.find((role) => role.currentVersion.ref === form.roleVersionRef),
);
const availableSubjects = computed(() =>
  props.subjects.filter((subject) => subject.kind === form.subjectKind),
);
const rolePermissions = computed(() =>
  props.permissions.filter((permission) =>
    selectedRole.value?.currentVersion.permissionKeys.includes(permission.key),
  ),
);
const allowedResourceKinds = computed(() => [
  ...new Set(
    rolePermissions.value.flatMap((permission) => permission.resourceKinds),
  ),
]);
const ownerConditionSupported = computed(() =>
  rolePermissions.value.some(
    (permission) => permission.ownerConditionSupported,
  ),
);
const valid = computed(
  () =>
    Boolean(form.subjectRef) &&
    Boolean(form.roleVersionRef) &&
    validScope(form.scope) &&
    selectedRole.value?.currentVersion.allowedScopes.includes(
      form.scope.kind,
    ) &&
    (!form.validFrom || !form.validUntil || form.validFrom < form.validUntil),
);

function toLocalDateTime(value?: string): string {
  if (!value) return "";
  const date = new Date(value);
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function reset(): void {
  const draft = emptyBindingDraft(props.defaultProjectRef ?? "");
  if (props.binding) {
    draft.subjectKind = props.binding.subject.kind;
    draft.subjectRef = props.binding.subject.ref;
    draft.roleVersionRef = props.binding.roleVersion.ref;
    draft.scope = scopeToDraft(props.binding.scope);
    draft.validFrom = toLocalDateTime(props.binding.conditions.validFrom);
    draft.validUntil = toLocalDateTime(props.binding.conditions.validUntil);
    draft.requireOwner = props.binding.conditions.requireOwner;
  } else if (props.initialSubject) {
    draft.subjectKind = props.initialSubject.kind;
    draft.subjectRef = props.initialSubject.ref;
  }
  Object.assign(form, draft);
  if (draft.scope.projectRef && draft.scope.resourceKind === "AGENT")
    emit("load-agents", draft.scope.projectRef);
}

function submit(): void {
  if (!valid.value || props.busy) return;
  const input = toBindingInput(form);
  if (props.binding) {
    emit("save", {
      roleVersionRef: input.roleVersionRef,
      scope: input.scope,
      conditions: input.conditions,
    });
  } else emit("save", input);
}

watch(
  () => [props.binding, props.initialSubject, props.defaultProjectRef],
  reset,
  { immediate: true },
);
watch(ownerConditionSupported, (supported) => {
  if (!supported) form.requireOwner = false;
});
</script>

<template>
  <ModalDialog
    :title="
      $t(
        binding
          ? 'access.bindingEditor.editTitle'
          : 'access.bindingEditor.createTitle',
      )
    "
    :busy="busy"
    @close="emit('close')"
  >
    <form
      id="access-binding-form"
      class="binding-form"
      @submit.prevent="submit"
    >
      <section class="binding-explanation">
        <strong>{{ $t("access.bindingEditor.modelTitle") }}</strong>
        <p>{{ $t("access.bindingEditor.modelHint") }}</p>
      </section>

      <div class="form-grid">
        <label class="field">
          <span>{{ $t("access.bindingEditor.subjectKind") }}</span>
          <select
            v-model="form.subjectKind"
            :disabled="busy || Boolean(binding)"
          >
            <option value="USER">{{ $t("access.subjectKinds.USER") }}</option>
            <option value="OIDC_GROUP">
              {{ $t("access.subjectKinds.OIDC_GROUP") }}
            </option>
            <option value="SERVICE">
              {{ $t("access.subjectKinds.SERVICE") }}
            </option>
          </select>
        </label>
        <label class="field">
          <span>{{ $t("access.bindingEditor.subject") }}</span>
          <select
            v-model="form.subjectRef"
            required
            :disabled="busy || Boolean(binding)"
          >
            <option value="" disabled>
              {{ $t("access.bindingEditor.chooseSubject") }}
            </option>
            <option
              v-for="subject in availableSubjects"
              :key="subject.ref"
              :value="subject.ref"
            >
              {{ subject.displayName }}
            </option>
          </select>
        </label>
        <label class="field field--wide">
          <span>{{ $t("access.bindingEditor.role") }}</span>
          <select v-model="form.roleVersionRef" required :disabled="busy">
            <option value="" disabled>
              {{ $t("access.bindingEditor.chooseRole") }}
            </option>
            <option
              v-for="role in roles.filter((item) => item.state === 'ACTIVE')"
              :key="role.currentVersion.ref"
              :value="role.currentVersion.ref"
            >
              {{ role.currentVersion.name }} · v{{
                role.currentVersion.revision
              }}
              ·
              {{ $t(`access.roleKinds.${role.kind}`) }}
            </option>
          </select>
          <small>{{ $t("access.bindingEditor.pinnedVersion") }}</small>
        </label>
      </div>

      <div>
        <h3>{{ $t("access.bindingEditor.scope") }}</h3>
        <p class="muted">{{ $t("access.bindingEditor.scopeHint") }}</p>
        <AccessScopeEditor
          v-model="form.scope"
          :projects="projects"
          :agents="agents"
          :allowed-scopes="selectedRole?.currentVersion.allowedScopes"
          :allowed-resource-kinds="allowedResourceKinds"
          :busy="busy"
          @load-agents="emit('load-agents', $event)"
        />
      </div>

      <fieldset class="conditions">
        <legend>{{ $t("access.bindingEditor.conditions") }}</legend>
        <label class="field">
          <span>{{ $t("access.bindingEditor.validFrom") }}</span>
          <input
            v-model="form.validFrom"
            type="datetime-local"
            :disabled="busy"
          />
        </label>
        <label class="field">
          <span>{{ $t("access.bindingEditor.validUntil") }}</span>
          <input
            v-model="form.validUntil"
            type="datetime-local"
            :disabled="busy"
          />
        </label>
        <label class="owner-condition">
          <input
            v-model="form.requireOwner"
            type="checkbox"
            :disabled="busy || !ownerConditionSupported"
          />
          <span>
            <strong>{{ $t("access.bindingEditor.ownerOnly") }}</strong>
            <small>{{ $t("access.bindingEditor.ownerOnlyHint") }}</small>
          </span>
        </label>
      </fieldset>

      <p
        v-if="
          form.validFrom && form.validUntil && form.validFrom >= form.validUntil
        "
        class="field-error"
      >
        {{ $t("access.bindingEditor.invalidWindow") }}
      </p>
      <ProblemNotice v-if="problem" :problem="problem" compact />
    </form>
    <template #actions>
      <button
        class="button"
        type="button"
        :disabled="busy"
        @click="emit('close')"
      >
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        type="submit"
        form="access-binding-form"
        :disabled="busy || !valid"
      >
        {{ $t(binding ? "common.save" : "access.bindingEditor.create") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.binding-form {
  display: grid;
  gap: 18px;
  width: min(760px, 80vw);
}
.binding-explanation {
  padding: 12px;
  border-left: 3px solid var(--accent);
  background: var(--accent-soft);
}
.binding-explanation p,
.muted {
  margin: 4px 0 0;
  color: var(--muted);
}
.conditions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.conditions legend {
  padding-inline: 5px;
  font-weight: 600;
}
.owner-condition {
  display: flex;
  grid-column: 1 / -1;
  align-items: flex-start;
  gap: 9px;
  font-weight: 400;
}
.owner-condition input {
  width: auto;
  min-height: auto;
  margin-top: 3px;
}
.owner-condition small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
}
@media (max-width: 720px) {
  .binding-form {
    width: auto;
  }
  .conditions {
    grid-template-columns: 1fr;
  }
}
</style>
