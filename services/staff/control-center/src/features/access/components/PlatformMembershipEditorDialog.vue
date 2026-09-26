<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { fetchPlatformMembershipCandidates } from "@/features/access/api";
import type {
  Membership,
  PlatformMembershipChangeInput,
  PlatformMembershipCreateInput,
  UserSummary,
} from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

type PlatformRole = PlatformMembershipCreateInput["platformRole"];
const roles: PlatformRole[] = [
  "MEMBER",
  "OPERATOR",
  "AUDITOR",
  "ADMINISTRATOR",
  "OWNER",
];

const props = defineProps<{
  membership?: Membership;
  busy?: boolean;
  problem?: AppProblem;
}>();
const emit = defineEmits<{
  close: [];
  create: [input: PlatformMembershipCreateInput];
  update: [input: PlatformMembershipChangeInput];
}>();

const selectedUser = ref<UserSummary>();
const role = ref<PlatformRole>(props.membership?.platformRole ?? "MEMBER");
const active = ref(props.membership?.active ?? true);
watch(
  () => props.membership,
  (membership) => {
    if (!membership) return;
    role.value = membership.platformRole;
    active.value = membership.active;
  },
);
const selectedOption = computed<AsyncEntityOption | undefined>(() =>
  selectedUser.value
    ? {
        ref: selectedUser.value.ref,
        title: selectedUser.value.displayName,
        description: selectedUser.value.emailHint,
      }
    : undefined,
);

async function loadCandidates(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
  pageSize?: number,
): Promise<AsyncEntityOptionPage> {
  const page = await fetchPlatformMembershipCandidates({
    query,
    pageToken: cursor,
    pageSize,
    signal,
  });
  return {
    items: page.items.map((user) => ({
      ref: user.ref,
      title: user.displayName,
      description: user.emailHint,
    })),
    nextPageToken: page.nextPageToken,
  };
}

function save(): void {
  if (props.busy) return;
  if (props.membership) {
    if (!props.membership.nextActions.includes("EDIT")) return;
    emit("update", { platformRole: role.value, active: active.value });
  } else if (selectedUser.value) {
    emit("create", {
      userRef: selectedUser.value.ref,
      platformRole: role.value,
    });
  }
}
</script>

<template>
  <ModalDialog
    :title="membership ? 'Изменить участника' : 'Добавить участника'"
    :busy="busy"
    size="md"
    @close="emit('close')"
  >
    <div class="platform-member-editor">
      <p class="platform-member-editor__hint">
        Личность и группы поступают из Keycloak. Здесь назначается только роль
        Kodex; доступ к работе в Проекте настраивается отдельно.
      </p>
      <div v-if="membership" class="field">
        <span>Участник</span>
        <strong>{{ membership.user.displayName }}</strong>
      </div>
      <div v-else class="field">
        <span>Участник Keycloak</span>
        <AsyncEntityPicker
          :model-value="selectedUser?.ref ?? ''"
          :selected="selectedOption"
          :load-page="loadCandidates"
          :labels="{
            label: 'Участник Keycloak',
            searchPlaceholder: 'Имя или email',
            loading: 'Ищем участников…',
            loadingMore: 'Загружаем ещё…',
            empty: 'Доступных для добавления пользователей нет',
            error: 'Не удалось найти участников',
            retry: 'Повторить',
          }"
          placeholder="Выбрать пользователя"
          :disabled="busy"
          :clearable="false"
          @select="
            selectedUser = {
              ref: $event.ref,
              displayName: $event.title,
              emailHint: $event.description,
            }
          "
        />
      </div>
      <label class="field">
        <span>Роль в платформе</span>
        <select v-model="role" :disabled="busy">
          <option v-for="item in roles" :key="item" :value="item">
            {{ $t(`access.platformRoles.${item}`) }}
          </option>
        </select>
      </label>
      <label v-if="membership" class="platform-member-editor__active">
        <input v-model="active" type="checkbox" :disabled="busy" />
        Активен
      </label>
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
        :disabled="busy || (!membership && !selectedUser)"
        @click="save"
      >
        {{ membership ? $t("common.save") : "Добавить" }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.platform-member-editor {
  display: grid;
  gap: 16px;
}
.platform-member-editor__hint {
  margin: 0;
  color: var(--muted);
  line-height: 1.45;
}
.platform-member-editor__active {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
