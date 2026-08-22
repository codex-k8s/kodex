<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import type {
  Membership,
  PlatformMembershipCreateInput,
  ProjectMembershipCreateInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type ProjectPermission = ProjectMembershipCreateInput["permissions"][number];
type PlatformRole = PlatformMembershipCreateInput["platformRole"];

interface AccessForm {
  userRef: string;
  platformRole: PlatformRole;
  permissions: ProjectPermission[];
  active: boolean;
}

const platform = usePlatformStore();
const route = useRoute();
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
const selected = ref<Membership>();
const dialog = ref(false);
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
  selected.value = undefined;
  Object.assign(form, {
    userRef: "",
    platformRole: "MEMBER",
    permissions: ["VIEW"],
    active: true,
  });
  problem.value = undefined;
  dialog.value = true;
  if (organizationScope.value) {
    void platform.loadPlatformMembershipCandidates();
  } else {
    void platform.loadMembershipCandidates(projectRef.value);
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
  if (!form.userRef || (!organizationScope.value && !projectRef.value)) return;
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

watch(projectRef, () => void load());
onMounted(() => void load());
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
        v-if="canAdd"
        class="button button--primary"
        type="button"
        @click="add"
      >
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
    <AsyncState
      :loading="platform.loading[listKey]"
      :problem="platform.problems[listKey]"
      :empty="list.length === 0"
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
      <div class="entity-list">
        <article
          v-for="membership in list"
          :key="membership.ref"
          class="entity-row"
        >
          <div>
            <h3>{{ membership.user.displayName }}</h3>
            <p>
              {{ membership.user.emailHint }} ·
              {{ $t(`access.roles.${membership.platformRole}`) }}
            </p>
            <p v-if="!organizationScope" class="secondary">
              {{
                $t("access.permissionCount", {
                  count: membership.permissions.length,
                })
              }}
            </p>
          </div>
          <StatusBadge :state="membership.active ? 'ACTIVE' : 'DISABLED'" />
          <div class="entity-row__actions">
            <button
              v-if="membership.nextActions.includes('EDIT')"
              class="button"
              type="button"
              @click="edit(membership)"
            >
              {{ $t("common.edit") }}</button
            ><button
              v-if="membership.nextActions.includes('REVOKE')"
              class="button button--danger"
              type="button"
              :disabled="busy"
              @click="revoke(membership)"
            >
              {{ $t("access.revoke") }}
            </button>
          </div>
        </article>
      </div>
    </AsyncState>
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
      ><form id="membership-form" class="form-grid" @submit.prevent="submit">
        <div v-if="selected" class="field field--wide">
          <span>{{ $t("access.member") }}</span
          ><strong>{{ selected.user.displayName }}</strong>
        </div>
        <AsyncState
          v-else
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
          <label class="field field--wide"
            ><span>{{ $t("access.member") }}</span
            ><select v-model="form.userRef" required autofocus>
              <option value="" disabled>{{ $t("access.chooseMember") }}</option>
              <option
                v-for="candidate in candidates"
                :key="candidate.ref"
                :value="candidate.ref"
              >
                {{ candidate.displayName
                }}{{ candidate.emailHint ? ` · ${candidate.emailHint}` : "" }}
              </option>
            </select></label
          >
        </AsyncState>
        <label v-if="organizationScope" class="field field--wide"
          ><span>{{ $t("access.role") }}</span
          ><select v-model="form.platformRole">
            <option value="OWNER">{{ $t("access.roles.OWNER") }}</option>
            <option value="ADMINISTRATOR">
              {{ $t("access.roles.ADMINISTRATOR") }}
            </option>
            <option value="OPERATOR">{{ $t("access.roles.OPERATOR") }}</option>
            <option value="MEMBER">{{ $t("access.roles.MEMBER") }}</option>
            <option value="AUDITOR">{{ $t("access.roles.AUDITOR") }}</option>
          </select></label
        >
        <fieldset v-else class="permission-grid field--wide">
          <legend>{{ $t("access.permissions") }}</legend>
          <label v-for="permission in permissions" :key="permission"
            ><input
              type="checkbox"
              :checked="form.permissions.includes(permission)"
              :disabled="permission === 'VIEW'"
              @change="togglePermission(permission)"
            />{{ $t(`access.permission.${permission}`) }}</label
          >
        </fieldset>
        <label v-if="selected" class="field field--wide inline-control"
          ><input v-model="form.active" type="checkbox" />
          <span>{{ $t("access.active") }}</span></label
        >
        <ProblemNotice
          v-if="problem"
          class="field--wide"
          :problem="problem"
          compact
        />
      </form>
      <template #actions
        ><button
          class="button"
          type="button"
          :disabled="busy"
          @click="closeDialog"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="membership-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t(selected ? "common.save" : "access.add") }}
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>

<style scoped>
.scope-summary {
  display: grid;
  gap: 4px;
  margin-bottom: 18px;
}
.scope-summary span,
.secondary {
  color: var(--text-secondary);
}
.candidate-state {
  min-height: 92px;
}
.permission-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  border: 0;
  padding: 0;
}
.permission-grid label,
.inline-control {
  display: flex;
  gap: 8px;
  align-items: flex-start;
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
