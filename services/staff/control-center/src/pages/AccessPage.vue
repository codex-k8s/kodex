<script setup lang="ts">
import { Plus } from "@lucide/vue";
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";

import AccessMembersPanel from "@/features/access/ui/AccessMembersPanel.vue";
import AccessRolesPanel from "@/features/access/ui/AccessRolesPanel.vue";
import AccessSectionTabs from "@/features/access/ui/AccessSectionTabs.vue";
import AccessUnavailablePanel from "@/features/access/ui/AccessUnavailablePanel.vue";
import {
  buildSystemRoles,
  filterMemberships,
  type AccessSection,
  type PlatformRole,
  type ProjectPermission,
} from "@/features/access/ui/model";
import { usePlatformStore } from "@/features/platform/store";
import type { Membership } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

interface AccessForm {
  userRef: string;
  platformRole: PlatformRole;
  permissions: ProjectPermission[];
  active: boolean;
}

const platform = usePlatformStore();
const route = useRoute();
const activeSection = ref<AccessSection>("MEMBERS");
const memberSearch = ref("");
const projectRef = computed(() =>
  typeof route.params.projectRef === "string" ? route.params.projectRef : "",
);
const organizationScope = computed(() => projectRef.value === "");
const list = computed(() =>
  Object.values(
    organizationScope.value
      ? platform.platformMemberships
      : platform.memberships,
  ),
);
const filteredList = computed(() =>
  filterMemberships(list.value, memberSearch.value),
);
const systemRoles = computed(() => buildSystemRoles(list.value));
const candidates = computed(() =>
  Object.values(
    organizationScope.value
      ? platform.platformMembershipCandidates
      : platform.membershipCandidates,
  ),
);
const listKey = computed(() =>
  organizationScope.value ? "platformMembers" : "members",
);
const candidateKey = computed(() =>
  organizationScope.value ? "platformMemberCandidates" : "memberCandidates",
);
const canAdd = computed(() =>
  (organizationScope.value
    ? platform.platformMembershipActions
    : platform.projectMembershipActions
  ).includes("MANAGE_MEMBERS"),
);
const unavailableSection = computed(
  () =>
    activeSection.value as Extract<
      AccessSection,
      "GROUPS" | "EFFECTIVE" | "AGENT_SCOPE"
    >,
);

const selected = ref<Membership>();
const dialog = ref(false);
const candidateSearch = ref("");
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive<AccessForm>({
  userRef: "",
  platformRole: "MEMBER",
  permissions: ["VIEW"],
  active: true,
});
const permissions: ProjectPermission[] = [
  "VIEW",
  "MANAGE",
  "MANAGE_MEMBERS",
  "MANAGE_AGENTS",
  "MANAGE_WORKFLOWS",
  "LAUNCH_RUNS",
  "CANCEL_RUNS",
  "RESOLVE_GATES",
  "MANAGE_ARTIFACTS",
  "MANAGE_SCHEDULES",
  "MANAGE_INTEGRATIONS",
  "VIEW_AUDIT",
];

function edit(membership: Membership): void {
  if (!membership.nextActions.includes("EDIT")) return;
  selected.value = membership;
  Object.assign(form, {
    userRef: membership.user.ref,
    platformRole: membership.platformRole,
    permissions: [...membership.permissions],
    active: membership.active,
  });
  problem.value = undefined;
  dialog.value = true;
}

function add(): void {
  if (!canAdd.value) return;
  selected.value = undefined;
  Object.assign(form, {
    userRef: "",
    platformRole: "MEMBER",
    permissions: ["VIEW"],
    active: true,
  });
  problem.value = undefined;
  candidateSearch.value = "";
  dialog.value = true;
  if (organizationScope.value) {
    void platform.loadPlatformMembershipCandidates();
  } else {
    void platform.loadMembershipCandidates(projectRef.value);
  }
}

let candidateSearchTimer: ReturnType<typeof setTimeout> | undefined;

function loadCandidates(): void {
  if (!dialog.value || selected.value) return;
  if (organizationScope.value) {
    void platform.loadPlatformMembershipCandidates(candidateSearch.value);
  } else {
    void platform.loadMembershipCandidates(
      projectRef.value,
      candidateSearch.value,
    );
  }
}

function closeDialog(): void {
  dialog.value = false;
  selected.value = undefined;
  problem.value = undefined;
}

function togglePermission(permission: ProjectPermission): void {
  const index = form.permissions.indexOf(permission);
  if (index >= 0) form.permissions.splice(index, 1);
  else form.permissions.push(permission);
}

async function load(): Promise<void> {
  if (organizationScope.value) {
    await platform.loadPlatformMembers();
  } else {
    await platform.loadMembers(projectRef.value);
  }
}

async function submit(): Promise<void> {
  if (
    !form.userRef ||
    (!organizationScope.value && !projectRef.value) ||
    (selected.value
      ? !selected.value.nextActions.includes("EDIT")
      : !canAdd.value)
  )
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    if (organizationScope.value) {
      await platform.savePlatformMembership(
        {
          userRef: form.userRef,
          platformRole: form.platformRole,
          active: form.active,
        },
        selected.value,
      );
    } else {
      await platform.saveMembership(
        projectRef.value,
        {
          userRef: form.userRef,
          permissions: [...form.permissions],
          active: form.active,
        },
        selected.value,
      );
    }
    await load();
    closeDialog();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function revoke(membership: Membership): Promise<void> {
  if (!membership.nextActions.includes("REVOKE")) return;
  busy.value = true;
  problem.value = undefined;
  try {
    if (organizationScope.value) {
      await platform.revokePlatformMembership(membership);
    } else {
      await platform.revokeMembership(projectRef.value, membership);
    }
    await load();
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

watch(projectRef, () => {
  activeSection.value = "MEMBERS";
  memberSearch.value = "";
  void load();
});
watch(candidateSearch, () => {
  if (candidateSearchTimer) clearTimeout(candidateSearchTimer);
  candidateSearchTimer = setTimeout(loadCandidates, 250);
});
onMounted(() => void load());
onUnmounted(() => {
  if (candidateSearchTimer) clearTimeout(candidateSearchTimer);
});
</script>

<template>
  <PageFrame
    :title="$t(organizationScope ? 'access.organizationTitle' : 'access.title')"
    :subtitle="
      $t(organizationScope ? 'access.organizationSubtitle' : 'access.subtitle')
    "
  >
    <template #actions>
      <button
        v-if="activeSection === 'MEMBERS' && canAdd"
        class="button button--primary"
        type="button"
        @click="add"
      >
        <Plus :size="16" aria-hidden="true" />
        {{ $t(organizationScope ? "access.addOrganization" : "access.add") }}
      </button>
    </template>

    <section class="scope-summary" aria-live="polite">
      <strong>{{
        $t(
          organizationScope
            ? "access.organizationScope"
            : "access.projectScope",
        )
      }}</strong>
      <span>{{
        $t(
          organizationScope
            ? "access.organizationScopeHint"
            : "access.projectScopeHint",
        )
      }}</span>
    </section>

    <AccessSectionTabs
      :active="activeSection"
      :member-count="list.length"
      :role-count="systemRoles.length"
      @select="activeSection = $event"
    />

    <AsyncState
      v-if="activeSection === 'MEMBERS'"
      :loading="platform.loading[listKey]"
      :problem="platform.problems[listKey]"
      :empty="filteredList.length === 0"
      :empty-title="
        $t(
          organizationScope
            ? 'access.organizationEmptyTitle'
            : 'access.emptyTitle',
        )
      "
      :empty-text="
        $t(
          organizationScope
            ? 'access.organizationEmptyText'
            : 'access.emptyText',
        )
      "
      @retry="load"
    >
      <AccessMembersPanel
        :memberships="filteredList"
        :organization-scope="organizationScope"
        :search="memberSearch"
        :busy="busy"
        @update:search="memberSearch = $event"
        @edit="edit"
        @revoke="revoke"
      />
    </AsyncState>

    <AsyncState
      v-else-if="activeSection === 'ROLES'"
      :loading="platform.loading[listKey]"
      :problem="platform.problems[listKey]"
      @retry="load"
    >
      <AccessRolesPanel
        :roles="systemRoles"
        :organization-scope="organizationScope"
      />
    </AsyncState>

    <AccessUnavailablePanel v-else :section="unavailableSection" />

    <ProblemNotice v-if="problem && !dialog" :problem="problem" compact />

    <ModalDialog
      v-if="dialog"
      :title="
        $t(
          selected
            ? 'access.edit'
            : organizationScope
              ? 'access.addOrganization'
              : 'access.add',
        )
      "
      :busy="busy"
      @close="closeDialog"
    >
      <form id="membership-form" class="form-grid" @submit.prevent="submit">
        <div v-if="selected" class="field field--wide">
          <span>{{ $t("access.member") }}</span>
          <strong>{{ selected.user.displayName }}</strong>
        </div>
        <template v-else>
          <label class="field field--wide">
            <span>{{ $t("access.searchMember") }}</span>
            <input
              v-model="candidateSearch"
              type="search"
              :placeholder="$t('access.searchMemberPlaceholder')"
              autocomplete="off"
              autofocus
            />
          </label>
          <AsyncState
            class="field--wide candidate-state"
            :loading="platform.loading[candidateKey]"
            :problem="platform.problems[candidateKey]"
            :empty="candidates.length === 0"
            :empty-title="$t('access.noCandidates')"
            :empty-text="
              $t(
                organizationScope
                  ? 'access.noOrganizationCandidatesText'
                  : 'access.noCandidatesText',
              )
            "
            @retry="add"
          >
            <label class="field field--wide">
              <span>{{ $t("access.member") }}</span>
              <select v-model="form.userRef" required>
                <option value="" disabled>
                  {{ $t("access.chooseMember") }}
                </option>
                <option
                  v-for="candidate in candidates"
                  :key="candidate.ref"
                  :value="candidate.ref"
                >
                  {{ candidate.displayName
                  }}{{ candidate.emailHint ? ` · ${candidate.emailHint}` : "" }}
                </option>
              </select>
            </label>
          </AsyncState>
        </template>
        <label v-if="organizationScope" class="field field--wide">
          <span>{{ $t("access.role") }}</span>
          <select v-model="form.platformRole">
            <option value="OWNER">{{ $t("access.roles.OWNER") }}</option>
            <option value="ADMINISTRATOR">
              {{ $t("access.roles.ADMINISTRATOR") }}
            </option>
            <option value="OPERATOR">{{ $t("access.roles.OPERATOR") }}</option>
            <option value="MEMBER">{{ $t("access.roles.MEMBER") }}</option>
            <option value="AUDITOR">{{ $t("access.roles.AUDITOR") }}</option>
          </select>
        </label>
        <fieldset v-else class="permission-grid field--wide">
          <legend>{{ $t("access.permissions") }}</legend>
          <label v-for="permission in permissions" :key="permission">
            <input
              type="checkbox"
              :checked="form.permissions.includes(permission)"
              :disabled="permission === 'VIEW'"
              @change="togglePermission(permission)"
            />
            {{ $t(`access.permission.${permission}`) }}
          </label>
        </fieldset>
        <label v-if="selected" class="field field--wide inline-control">
          <input v-model="form.active" type="checkbox" />
          <span>{{ $t("access.active") }}</span>
        </label>
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="closeDialog"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--primary"
          form="membership-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t(selected ? "common.save" : "access.add") }}
        </button>
      </template>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.scope-summary {
  display: grid;
  gap: 4px;
  margin-bottom: 10px;
  padding: 10px 12px;
  border-left: 3px solid var(--accent);
  color: var(--text-secondary);
  background: var(--panel);
}
.candidate-state {
  min-height: 92px;
}
.permission-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding: 0;
  border: 0;
}
.permission-grid label,
.inline-control {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-weight: 400;
}
.permission-grid input,
.inline-control input {
  width: auto;
  min-height: auto;
}
@media (max-width: 620px) {
  .permission-grid {
    grid-template-columns: 1fr;
  }
}
</style>
