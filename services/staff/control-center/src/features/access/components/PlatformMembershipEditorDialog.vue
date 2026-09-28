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
    :title="
      $t(
        membership
          ? 'access.platformMembershipEditor.editTitle'
          : 'access.platformMembershipEditor.createTitle',
      )
    "
    :busy="busy"
    size="md"
    @close="emit('close')"
  >
    <div class="platform-member-editor">
      <p class="platform-member-editor__hint">
        {{ $t("access.platformMembershipEditor.hint") }}
      </p>
      <div v-if="membership" class="field">
        <span>{{ $t("access.platformMembershipEditor.member") }}</span>
        <strong>{{ membership.user.displayName }}</strong>
      </div>
      <div v-else class="field">
        <span>{{ $t("access.platformMembershipEditor.keycloakMember") }}</span>
        <AsyncEntityPicker
          :model-value="selectedUser?.ref ?? ''"
          :selected="selectedOption"
          :load-page="loadCandidates"
          :labels="{
            label: $t('access.platformMembershipEditor.keycloakMember'),
            searchPlaceholder: $t(
              'access.platformMembershipEditor.searchPlaceholder',
            ),
            loading: $t('access.platformMembershipEditor.loading'),
            loadingMore: $t('access.platformMembershipEditor.loadingMore'),
            empty: $t('access.platformMembershipEditor.empty'),
            error: $t('access.platformMembershipEditor.error'),
            retry: $t('common.retry'),
          }"
          :placeholder="$t('access.platformMembershipEditor.chooseUser')"
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
        <span>{{ $t("access.platformMembershipEditor.role") }}</span>
        <select v-model="role" :disabled="busy">
          <option v-for="item in roles" :key="item" :value="item">
            {{ $t(`access.platformRoles.${item}`) }}
          </option>
        </select>
      </label>
      <label v-if="membership" class="platform-member-editor__active">
        <input v-model="active" type="checkbox" :disabled="busy" />
        {{ $t("access.platformMembershipEditor.active") }}
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
        {{
          membership
            ? $t("common.save")
            : $t("access.platformMembershipEditor.create")
        }}
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
