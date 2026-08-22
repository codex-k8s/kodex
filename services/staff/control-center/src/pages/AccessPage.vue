<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import type {
  Membership,
  MembershipInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const projectRef = computed(() =>
  typeof route.params.projectRef === "string"
    ? route.params.projectRef
    : typeof route.query.projectRef === "string"
      ? route.query.projectRef
      : "",
);
const list = computed(() => Object.values(platform.memberships));
const selected = ref<Membership>();
const busy = ref(false);
const problem = ref<AppProblem>();
const form = reactive<MembershipInput>({
  userRef: "",
  platformRole: "MEMBER",
  permissions: ["VIEW"],
  active: true,
});
const permissions: MembershipInput["permissions"] = [
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

function chooseProject(event: Event): void {
  const value = (event.target as HTMLSelectElement).value;
  void router.push(
    value
      ? { path: "/administration/access", query: { projectRef: value } }
      : "/administration/access",
  );
}

function edit(membership: Membership): void {
  selected.value = membership;
  Object.assign(form, {
    userRef: membership.user.ref,
    platformRole: membership.platformRole,
    permissions: [...membership.permissions],
    active: membership.active,
  });
  problem.value = undefined;
}

function togglePermission(
  permission: MembershipInput["permissions"][number],
): void {
  const index = form.permissions.indexOf(permission);
  if (index >= 0) form.permissions.splice(index, 1);
  else form.permissions.push(permission);
}

async function submit(): Promise<void> {
  if (!selected.value || !projectRef.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.saveMembership(
      projectRef.value,
      { ...form, permissions: [...form.permissions] },
      selected.value,
    );
    selected.value = undefined;
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function revoke(membership: Membership): Promise<void> {
  if (!projectRef.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await platform.revokeMembership(projectRef.value, membership);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

watch(
  projectRef,
  (value) => {
    if (value) void platform.loadMembers(value);
  },
  { immediate: true },
);
onMounted(() => void platform.loadProjects());
</script>

<template>
  <PageFrame :title="$t('access.title')" :subtitle="$t('access.subtitle')">
    <section v-if="!route.params.projectRef" class="panel project-choice">
      <label class="field"
        ><span>{{ $t("access.project") }}</span
        ><select :value="projectRef" @change="chooseProject">
          <option value="">{{ $t("app.chooseProject") }}</option>
          <option
            v-for="project in platform.projectList"
            :key="project.ref"
            :value="project.ref"
          >
            {{ project.name }}
          </option>
        </select></label
      >
    </section>
    <AsyncState
      v-if="projectRef"
      :loading="platform.loading.members"
      :problem="platform.problems.members"
      :empty="list.length === 0"
      :empty-title="$t('access.emptyTitle')"
      @retry="platform.loadMembers(projectRef)"
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
    <section v-else class="empty-state">
      <div class="empty-state__icon" aria-hidden="true">+</div>
      <h2>{{ $t("access.chooseProject") }}</h2>
      <p>{{ $t("access.chooseProjectText") }}</p>
    </section>
    <ProblemNotice v-if="problem && !selected" :problem="problem" compact />
    <ModalDialog
      v-if="selected"
      :title="$t('access.edit')"
      :busy="busy"
      @close="selected = undefined"
      ><form id="membership-form" class="form-grid" @submit.prevent="submit">
        <div class="field field--wide">
          <span>{{ $t("access.member") }}</span
          ><strong>{{ selected.user.displayName }}</strong>
        </div>
        <label class="field field--wide"
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
        <fieldset class="permission-grid field--wide">
          <legend>{{ $t("access.permissions") }}</legend>
          <label v-for="permission in permissions" :key="permission"
            ><input
              type="checkbox"
              :checked="form.permissions.includes(permission)"
              @change="togglePermission(permission)"
            />{{ $t(`access.permission.${permission}`) }}</label
          >
        </fieldset>
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
          @click="selected = undefined"
        >
          {{ $t("common.cancel") }}</button
        ><button
          class="button button--primary"
          form="membership-form"
          type="submit"
          :disabled="busy"
        >
          {{ $t("common.save") }}
        </button></template
      ></ModalDialog
    >
  </PageFrame>
</template>

<style scoped>
.project-choice {
  max-width: 520px;
  margin-bottom: 18px;
}
.permission-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  border: 0;
  padding: 0;
}
.permission-grid label {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  font-weight: 400;
}
.permission-grid input {
  width: auto;
  min-height: auto;
}
@media (max-width: 620px) {
  .permission-grid {
    grid-template-columns: 1fr;
  }
}
</style>
