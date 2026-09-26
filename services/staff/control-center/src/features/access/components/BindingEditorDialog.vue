<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";

import AccessScopeEditor from "@/features/access/components/AccessScopeEditor.vue";
import {
  accessRoleOptions,
  accessSubjectOptions,
} from "@/features/access/entity-pickers";
import {
  emptyBindingDraft,
  scopeToDraft,
  toBindingInput,
  validScope,
  type BindingDraft,
} from "@/features/access/model";
import { permissionMessage } from "@/features/access/presentation";
import type {
  AccessBinding,
  AccessBindingChangeInput,
  AccessBindingInput,
  AccessRole,
  AccessSubject,
  Agent,
  IntegrationConnection,
  PermissionDefinition,
  Project,
  Workflow,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
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
  workflows: Workflow[];
  integrations: IntegrationConnection[];
  busy?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  save: [input: AccessBindingInput | AccessBindingChangeInput];
  "load-project-resources": [projectRef: string];
}>();
const i18n = useI18n();
const permissionMessages = computed(() =>
  i18n.tm("access.permissionsRegistry"),
);

const form = reactive<BindingDraft>(emptyBindingDraft());
const subjectRows = new Map<string, AccessSubject>();
const roleRows = new Map<string, AccessRole>();
const selectedRole = computed(
  () =>
    roleRows.get(form.roleVersionRef) ??
    props.roles.find((role) => role.currentVersion.ref === form.roleVersionRef),
);
const selectedSubject = computed(
  () =>
    subjectRows.get(form.subjectRef) ??
    props.subjects.find((subject) => subject.ref === form.subjectRef),
);
const selectedSubjectOption = computed<AsyncEntityOption | undefined>(() =>
  selectedSubject.value
    ? {
        ref: selectedSubject.value.ref,
        title: selectedSubject.value.displayName,
      }
    : undefined,
);
const selectedRoleOption = computed<AsyncEntityOption | undefined>(() =>
  selectedRole.value
    ? {
        ref: selectedRole.value.currentVersion.ref,
        title: selectedRole.value.currentVersion.name,
        description: selectedRole.value.currentVersion.description,
        meta: `v${String(selectedRole.value.currentVersion.revision)}`,
      }
    : undefined,
);
const rolePermissions = computed(() =>
  props.permissions.filter((permission) =>
    selectedRole.value?.currentVersion.permissionKeys.includes(permission.key),
  ),
);
const permissionRegistryComplete = computed(
  () =>
    !selectedRole.value ||
    rolePermissions.value.length ===
      selectedRole.value.currentVersion.permissionKeys.length,
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
    permissionRegistryComplete.value &&
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
  if (
    draft.scope.projectRef &&
    ["AGENT", "WORKFLOW"].includes(draft.scope.resourceKind)
  )
    emit("load-project-resources", draft.scope.projectRef);
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

function loadSubjects(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  return accessSubjectOptions(
    form.subjectKind,
    query,
    cursor,
    signal,
    pageSize,
    (items) => items.forEach((item) => subjectRows.set(item.ref, item)),
  );
}

function loadRoles(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  return accessRoleOptions(
    query,
    cursor,
    signal,
    pageSize,
    (items) =>
      items.forEach((item) => roleRows.set(item.currentVersion.ref, item)),
    "VERSION",
  );
}

watch(
  () => [props.binding, props.initialSubject, props.defaultProjectRef],
  reset,
  { immediate: true },
);
watch(ownerConditionSupported, (supported) => {
  if (!supported) form.requireOwner = false;
});
watch(
  () => form.subjectKind,
  (kind) => {
    if (selectedSubject.value && selectedSubject.value.kind !== kind)
      form.subjectRef = "";
  },
);
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
            name="access-binding-subject-kind"
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
        <div class="field">
          <span>{{ $t("access.bindingEditor.subject") }}</span>
          <AsyncEntityPicker
            :model-value="form.subjectRef"
            :selected="selectedSubjectOption"
            :load-page="loadSubjects"
            :labels="
              pickerLabels(
                $t('access.bindingEditor.subject'),
                $t('access.bindingEditor.chooseSubject'),
              )
            "
            :context-key="form.subjectKind"
            :placeholder="$t('access.bindingEditor.chooseSubject')"
            :trigger-label="$t('access.bindingEditor.subject')"
            :clearable="false"
            :disabled="busy || Boolean(binding)"
            @update:model-value="form.subjectRef = selection($event)"
          />
        </div>
        <div class="field field--wide">
          <span>{{ $t("access.bindingEditor.role") }}</span>
          <AsyncEntityPicker
            :model-value="form.roleVersionRef"
            :selected="selectedRoleOption"
            :load-page="loadRoles"
            :labels="
              pickerLabels(
                $t('access.bindingEditor.role'),
                $t('access.bindingEditor.chooseRole'),
              )
            "
            :placeholder="$t('access.bindingEditor.chooseRole')"
            :trigger-label="$t('access.bindingEditor.role')"
            :clearable="false"
            :disabled="busy"
            @update:model-value="form.roleVersionRef = selection($event)"
          />
          <small>{{ $t("access.bindingEditor.pinnedVersion") }}</small>
        </div>
      </div>

      <div>
        <h3>{{ $t("access.bindingEditor.scope") }}</h3>
        <p class="muted">{{ $t("access.bindingEditor.scopeHint") }}</p>
        <AccessScopeEditor
          v-model="form.scope"
          :projects="projects"
          :agents="agents"
          :workflows="workflows"
          :integrations="integrations"
          :allowed-scopes="selectedRole?.currentVersion.allowedScopes"
          :allowed-resource-kinds="allowedResourceKinds"
          :busy="busy"
          @load-project-resources="emit('load-project-resources', $event)"
        />
      </div>

      <section v-if="selectedRole" class="selected-role-summary">
        <header>
          <div>
            <strong>{{ selectedRole.currentVersion.name }}</strong>
            <small>
              {{ $t("access.roleKinds." + selectedRole.kind) }} · v{{
                selectedRole.currentVersion.revision
              }}
            </small>
          </div>
          <span>{{
            $t("access.bindingEditor.operationCount", {
              count: selectedRole.currentVersion.permissionKeys.length,
            })
          }}</span>
        </header>
        <p v-if="!permissionRegistryComplete" class="registry-unavailable">
          {{ $t("access.bindingEditor.permissionRegistryUnavailable") }}
        </p>
        <ul>
          <li v-for="permission in rolePermissions" :key="permission.key">
            <div>
              <strong>{{
                permissionMessage(permissionMessages, permission.key, "name")
              }}</strong>
              <small>{{
                permissionMessage(
                  permissionMessages,
                  permission.key,
                  "description",
                )
              }}</small>
            </div>
            <code>{{ permission.key }}</code>
          </li>
        </ul>
      </section>

      <fieldset class="conditions">
        <legend>{{ $t("access.bindingEditor.conditions") }}</legend>
        <label class="field">
          <span>{{ $t("access.bindingEditor.validFrom") }}</span>
          <input
            v-model="form.validFrom"
            name="access-binding-valid-from"
            type="datetime-local"
            :disabled="busy"
          />
        </label>
        <label class="field">
          <span>{{ $t("access.bindingEditor.validUntil") }}</span>
          <input
            v-model="form.validUntil"
            name="access-binding-valid-until"
            type="datetime-local"
            :disabled="busy"
          />
        </label>
        <label class="owner-condition">
          <input
            v-model="form.requireOwner"
            name="access-binding-require-owner"
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
.selected-role-summary {
  display: grid;
  gap: 9px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.registry-unavailable {
  margin: 0;
  padding: 8px 10px;
  color: var(--warning);
  background: var(--warning-soft);
}
.selected-role-summary header,
.selected-role-summary li {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.selected-role-summary header small,
.selected-role-summary li small {
  display: block;
  margin-top: 2px;
  color: var(--muted);
}
.selected-role-summary header > span {
  flex: 0 0 auto;
  padding: 3px 7px;
  border-radius: 6px;
  background: var(--accent-soft);
  color: var(--accent-strong);
  font-size: 0.75rem;
}
.selected-role-summary ul {
  display: grid;
  gap: 6px;
  max-height: 220px;
  margin: 0;
  padding: 0;
  overflow: auto;
  list-style: none;
}
.selected-role-summary li {
  padding: 8px;
  border: 1px solid var(--hairline);
  border-radius: 6px;
  background: var(--surface);
}
.selected-role-summary code {
  color: var(--muted);
  font-size: 0.72rem;
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
