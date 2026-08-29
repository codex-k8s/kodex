<script setup lang="ts">
import { computed, reactive, watch } from "vue";
import { useI18n } from "vue-i18n";

import { accessScopeKinds, roleInput } from "@/features/access/model";
import { permissionMessage } from "@/features/access/presentation";
import type {
  AccessRole,
  AccessRoleInput,
  AccessRoleVersion,
  AccessScopeKind,
  PermissionDefinition,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  role?: AccessRole;
  permissions: PermissionDefinition[];
  versions: AccessRoleVersion[];
  busy?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  save: [input: AccessRoleInput];
}>();
const i18n = useI18n();
const permissionMessages = computed(() =>
  i18n.tm("access.permissionsRegistry"),
);

const form = reactive({
  name: "",
  description: "",
  permissionKeys: [] as string[],
  allowedScopes: [] as AccessScopeKind[],
  changeComment: "",
});

const compatibleScopes = computed(() => {
  if (form.permissionKeys.length === 0) return accessScopeKinds;
  return accessScopeKinds.filter((scope) =>
    form.permissionKeys.every((key) =>
      props.permissions
        .find((permission) => permission.key === key)
        ?.allowedScopes.includes(scope),
    ),
  );
});
const valid = computed(
  () =>
    form.name.trim().length > 0 &&
    form.permissionKeys.length > 0 &&
    form.allowedScopes.length > 0 &&
    form.allowedScopes.every((scope) => compatibleScopes.value.includes(scope)),
);

function reset(): void {
  const version = props.role?.currentVersion;
  form.name = version?.name ?? "";
  form.description = version?.description ?? "";
  form.permissionKeys = [...(version?.permissionKeys ?? [])];
  form.allowedScopes = [...(version?.allowedScopes ?? [])];
  form.changeComment = "";
}

function togglePermission(key: string): void {
  const index = form.permissionKeys.indexOf(key);
  if (index >= 0) form.permissionKeys.splice(index, 1);
  else form.permissionKeys.push(key);
  form.allowedScopes = form.allowedScopes.filter((scope) =>
    compatibleScopes.value.includes(scope),
  );
}

function toggleScope(scope: AccessScopeKind): void {
  const index = form.allowedScopes.indexOf(scope);
  if (index >= 0) form.allowedScopes.splice(index, 1);
  else form.allowedScopes.push(scope);
}

function submit(): void {
  if (!valid.value || props.busy) return;
  emit(
    "save",
    roleInput(
      form.name,
      form.description,
      form.permissionKeys,
      form.allowedScopes,
      form.changeComment,
    ),
  );
}

watch(() => props.role, reset, { immediate: true });
</script>

<template>
  <ModalDialog
    :title="
      $t(role ? 'access.roleEditor.editTitle' : 'access.roleEditor.createTitle')
    "
    :busy="busy"
    @close="emit('close')"
  >
    <form id="access-role-form" class="role-form" @submit.prevent="submit">
      <section v-if="role" class="version-banner">
        <div>
          <strong>{{ $t("access.roleEditor.newVersion") }}</strong>
          <p>{{ $t("access.roleEditor.newVersionHint") }}</p>
        </div>
        <StatusBadge :state="role.state" />
      </section>

      <label class="field">
        <span>{{ $t("common.name") }}</span>
        <input v-model="form.name" required maxlength="160" :disabled="busy" />
      </label>
      <label class="field">
        <span>{{ $t("access.roleEditor.description") }}</span>
        <textarea
          v-model="form.description"
          maxlength="2000"
          :disabled="busy"
          :placeholder="$t('access.roleEditor.descriptionPlaceholder')"
        />
      </label>

      <fieldset class="role-fieldset">
        <legend>{{ $t("access.roleEditor.permissions") }}</legend>
        <p>{{ $t("access.roleEditor.permissionsHint") }}</p>
        <div class="permission-catalog">
          <label
            v-for="permission in permissions"
            :key="permission.key"
            class="permission-option"
          >
            <input
              type="checkbox"
              :checked="form.permissionKeys.includes(permission.key)"
              :disabled="busy"
              @change="togglePermission(permission.key)"
            />
            <span>
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
            </span>
            <span
              class="risk"
              :class="`risk--${permission.risk.toLowerCase()}`"
              >{{ $t(`access.risk.${permission.risk}`) }}</span
            >
          </label>
        </div>
      </fieldset>

      <fieldset class="role-fieldset">
        <legend>{{ $t("access.roleEditor.scopes") }}</legend>
        <p>{{ $t("access.roleEditor.scopesHint") }}</p>
        <div class="scope-options">
          <label v-for="scope in accessScopeKinds" :key="scope">
            <input
              type="checkbox"
              :checked="form.allowedScopes.includes(scope)"
              :disabled="busy || !compatibleScopes.includes(scope)"
              @change="toggleScope(scope)"
            />
            <span>
              <strong>{{ $t(`access.scope.values.${scope}`) }}</strong>
              <small>{{ $t(`access.scope.hints.${scope}`) }}</small>
            </span>
          </label>
        </div>
      </fieldset>

      <label class="field">
        <span>{{ $t("access.roleEditor.changeComment") }}</span>
        <input
          v-model="form.changeComment"
          maxlength="500"
          :disabled="busy"
          :placeholder="$t('access.roleEditor.changeCommentPlaceholder')"
        />
      </label>

      <details v-if="role" class="version-history">
        <summary>
          {{ $t("access.roleEditor.history", { count: versions.length }) }}
        </summary>
        <article v-for="version in versions" :key="version.ref">
          <strong>{{
            $t("access.roleEditor.revision", { revision: version.revision })
          }}</strong>
          <span>{{
            version.changeComment || $t("access.roleEditor.noComment")
          }}</span>
          <small>{{ new Date(version.createdAt).toLocaleString() }}</small>
        </article>
      </details>

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
        form="access-role-form"
        :disabled="busy || !valid"
      >
        {{
          $t(
            role
              ? "access.roleEditor.publishVersion"
              : "access.roleEditor.create",
          )
        }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.role-form {
  display: grid;
  gap: 18px;
  width: min(860px, 82vw);
}
.version-banner {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding: 12px;
  border: 1px solid #e9d7a1;
  border-radius: 8px;
  background: #fff9e8;
}
.version-banner p,
.role-fieldset > p {
  margin: 4px 0 0;
  color: var(--muted);
}
.role-fieldset {
  display: grid;
  gap: 10px;
  padding: 0;
  border: 0;
}
.role-fieldset legend {
  font-weight: 600;
}
.permission-catalog,
.scope-options {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.permission-option,
.scope-options label {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 9px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  font-weight: 400;
}
.permission-option input,
.scope-options input {
  width: auto;
  min-height: auto;
  margin-top: 3px;
}
.permission-option small,
.scope-options small {
  display: block;
  margin-top: 3px;
  color: var(--muted);
}
.risk {
  padding: 2px 6px;
  border-radius: 999px;
  color: var(--muted);
  background: #edf1f5;
  font-size: 0.72rem;
}
.risk--write,
.risk--approve {
  color: #725100;
  background: #fff0c7;
}
.risk--admin {
  color: #8a2626;
  background: #fde2e2;
}
.version-history article {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 12px;
  padding: 8px 0;
  border-bottom: 1px solid var(--border);
}
.version-history small {
  color: var(--muted);
}
@media (max-width: 760px) {
  .role-form {
    width: auto;
  }
  .permission-catalog,
  .scope-options {
    grid-template-columns: 1fr;
  }
  .version-history article {
    grid-template-columns: 1fr;
  }
}
</style>
