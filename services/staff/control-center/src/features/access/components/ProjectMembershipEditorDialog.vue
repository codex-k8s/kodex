<script setup lang="ts">
import { ref, watch } from "vue";

import { projectPermissionOrder } from "@/features/access/ui/model";
import type {
  Membership,
  ProjectMembershipChangeInput,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

type ProjectPermission = ProjectMembershipChangeInput["permissions"][number];

const props = defineProps<{
  membership: Membership;
  busy?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  save: [input: ProjectMembershipChangeInput];
}>();

const active = ref(props.membership.active);
const permissions = ref<ProjectPermission[]>([...props.membership.permissions]);

watch(
  () => props.membership,
  (membership) => {
    active.value = membership.active;
    permissions.value = [...membership.permissions];
  },
);

function toggle(permission: ProjectPermission, checked: boolean): void {
  const selected = new Set(permissions.value);
  if (checked) selected.add(permission);
  else selected.delete(permission);
  permissions.value = projectPermissionOrder.filter((item) =>
    selected.has(item),
  );
}

function save(): void {
  if (props.busy) return;
  emit("save", { active: active.value, permissions: permissions.value });
}
</script>

<template>
  <ModalDialog
    :title="$t('access.projectMembershipEditor.title')"
    :busy="busy"
    size="lg"
    @close="emit('close')"
  >
    <div class="membership-editor">
      <p class="membership-editor__subject">
        {{ membership.user.displayName }}
      </p>
      <p class="membership-editor__hint">
        {{ $t("access.projectMembershipEditor.scope") }}
      </p>
      <label class="membership-editor__active">
        <input v-model="active" type="checkbox" :disabled="busy" />
        {{ $t("access.projectMembershipEditor.active") }}
      </label>
      <fieldset class="membership-editor__permissions" :disabled="busy">
        <legend>{{ $t("access.projectMembershipEditor.permissions") }}</legend>
        <label v-for="permission in projectPermissionOrder" :key="permission">
          <input
            type="checkbox"
            :checked="permissions.includes(permission)"
            @change="
              toggle(permission, ($event.target as HTMLInputElement).checked)
            "
          />
          <span>{{ $t(`access.permission.${permission}`) }}</span>
        </label>
      </fieldset>
      <ProblemNotice v-if="problem" :problem="problem" compact />
    </div>
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
        type="button"
        :disabled="busy"
        @click="save"
      >
        {{ $t("common.save") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.membership-editor {
  display: grid;
  gap: 14px;
}
.membership-editor__subject {
  margin: 0;
  font-weight: 600;
}
.membership-editor__hint {
  margin: 0;
  color: var(--muted);
}
.membership-editor__active,
.membership-editor__permissions label {
  display: flex;
  align-items: center;
  gap: 9px;
  min-height: 32px;
}
.membership-editor__permissions {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 5px 18px;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
}
.membership-editor__permissions legend {
  padding: 0 5px;
  font-weight: 600;
}
@media (max-width: 700px) {
  .membership-editor__permissions {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
