<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import AccessTabs from "@/features/access/components/AccessTabs.vue";
import AccessModelOverview from "@/features/access/components/AccessModelOverview.vue";
import BindingEditorDialog from "@/features/access/components/BindingEditorDialog.vue";
import BindingsPanel from "@/features/access/components/BindingsPanel.vue";
import EffectiveAccessPanel from "@/features/access/components/EffectiveAccessPanel.vue";
import GroupsPanel from "@/features/access/components/GroupsPanel.vue";
import ParticipantsPanel from "@/features/access/components/ParticipantsPanel.vue";
import ProjectMembershipEditorDialog from "@/features/access/components/ProjectMembershipEditorDialog.vue";
import RoleEditorDialog from "@/features/access/components/RoleEditorDialog.vue";
import RolesPanel from "@/features/access/components/RolesPanel.vue";
import { accessSections, type AccessSection } from "@/features/access/model";
import { useAccessStore } from "@/features/access/store";
import type {
  AccessBinding,
  AccessBindingChangeInput,
  AccessBindingInput,
  AccessRole,
  AccessRoleInput,
  AccessSubject,
  Membership,
  OidcGroup,
  ProjectMembershipChangeInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const access = useAccessStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const projectRef = computed(() =>
  typeof route.params.projectRef === "string" ? route.params.projectRef : "",
);
const memberRef = computed(() =>
  typeof route.query.memberRef === "string" ? route.query.memberRef : "",
);
const memberSearch = computed(() =>
  projectRef.value && memberRef.value
    ? (access.projectMemberships.find(
        (membership) =>
          membership.projectRef === projectRef.value &&
          membership.user.ref === memberRef.value,
      )?.user.displayName ?? "")
    : "",
);
const selectedSubjectRef = computed(() =>
  memberSearch.value ? memberRef.value : "",
);
const routeSection = computed(() => {
  const raw =
    route.name === "access" ? route.params.section : route.query.section;
  return typeof raw === "string" &&
    accessSections.includes(raw as AccessSection)
    ? (raw as AccessSection)
    : "participants";
});
const participantSubjects = computed(() =>
  access.subjects.filter((subject) => subject.kind !== "OIDC_GROUP"),
);
const bindingSubjects = computed<AccessSubject[]>(() => [
  ...access.subjects.filter((subject) => subject.kind !== "OIDC_GROUP"),
  ...access.groups.map((group) => ({
    ref: group.ref,
    kind: "OIDC_GROUP" as const,
    displayName: group.displayName,
    active: group.state === "ACTIVE",
    oidcGroupRefs: [],
  })),
]);
const counts = computed(() => ({
  participants: participantSubjects.value.length,
  groups: access.groups.length,
  roles: access.roles.length,
  bindings: access.bindings.filter((binding) => binding.state === "ACTIVE")
    .length,
}));
const editorAgentsProjectRef = ref("");
const editorAgents = computed(
  () => access.agents[editorAgentsProjectRef.value] ?? [],
);
const editorWorkflows = computed(
  () => access.workflows[editorAgentsProjectRef.value] ?? [],
);
const roleDialog = ref(false);
const bindingDialog = ref(false);
const selectedRole = ref<AccessRole>();
const selectedBinding = ref<AccessBinding>();
const selectedMembership = ref<Membership>();
const initialSubject = ref<AccessSubject>();
const mutationBusy = ref(false);
const mutationProblem = ref<AppProblem>();
const confirmation = ref<
  | { kind: "ARCHIVE_ROLE"; role: AccessRole }
  | { kind: "REVOKE_BINDING"; binding: AccessBinding }
  | { kind: "REVOKE_MEMBERSHIP"; membership: Membership }
>();

function selectSection(section: AccessSection): void {
  if (route.name === "project-access") {
    void router.push({
      name: "project-access",
      params: { projectRef: projectRef.value },
      query: { section },
    });
    return;
  }
  void router.push({ name: "access", params: { section } });
}

async function loadSection(section = routeSection.value): Promise<void> {
  if (section === "participants") {
    await Promise.all([
      access.loadSubjects(memberSearch.value),
      access.loadBindings({ projectRef: projectRef.value || undefined }),
    ]);
  } else if (section === "groups") {
    await Promise.all([
      access.loadGroups(),
      access.loadSubjects(),
      access.loadBindings({ projectRef: projectRef.value || undefined }),
    ]);
  } else if (section === "roles") {
    await access.loadRoles(true);
  } else if (section === "bindings") {
    await Promise.all([
      access.loadSubjects(),
      access.loadBindings({
        projectRef: projectRef.value || undefined,
        includeRevoked: false,
      }),
    ]);
  } else {
    await Promise.all([
      access.loadSubjects(),
      access.loadPermissions(),
      access.loadRoles(),
    ]);
  }
}

async function loadBaseline(): Promise<void> {
  await Promise.all([
    access.loadPermissions(),
    access.loadProjects(),
    access.loadRoles(true),
    access.loadGroups(),
    access.loadIntegrations(),
    access.loadMembershipPresentation(projectRef.value, memberRef.value),
  ]);
  await loadSection();
}

async function loadProjectResources(value: string): Promise<void> {
  editorAgentsProjectRef.value = value;
  await Promise.all([access.loadAgents(value), access.loadWorkflows(value)]);
}

function createRole(): void {
  selectedRole.value = undefined;
  mutationProblem.value = undefined;
  roleDialog.value = true;
}

async function editRole(role: AccessRole): Promise<void> {
  selectedRole.value = role;
  mutationProblem.value = undefined;
  roleDialog.value = true;
  await access.loadRoleVersions(role.ref);
}

async function saveRole(input: AccessRoleInput): Promise<void> {
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    await access.saveRole(input, selectedRole.value);
    roleDialog.value = false;
  } catch (error) {
    mutationProblem.value = asProblem(error);
    if (mutationProblem.value.kind === "conflict") await access.loadRoles(true);
  } finally {
    mutationBusy.value = false;
  }
}

function archiveRole(role: AccessRole): void {
  mutationProblem.value = undefined;
  confirmation.value = { kind: "ARCHIVE_ROLE", role };
}

function revokeBinding(binding: AccessBinding): void {
  mutationProblem.value = undefined;
  confirmation.value = { kind: "REVOKE_BINDING", binding };
}

function editMembership(membership: Membership): void {
  if (!projectRef.value || !membership.nextActions.includes("EDIT")) return;
  selectedMembership.value = membership;
  mutationProblem.value = undefined;
}

async function saveMembership(
  input: ProjectMembershipChangeInput,
): Promise<void> {
  const membership = selectedMembership.value;
  if (!membership || !projectRef.value || mutationBusy.value) return;
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    await access.saveProjectMembership(projectRef.value, membership, input);
    selectedMembership.value = undefined;
  } catch (error) {
    mutationProblem.value = asProblem(error);
    if (mutationProblem.value.kind === "conflict") {
      await access.loadMembershipPresentation(
        projectRef.value,
        memberRef.value,
      );
      selectedMembership.value = access.projectMemberships.find(
        (item) => item.ref === membership.ref,
      );
    }
  } finally {
    mutationBusy.value = false;
  }
}

function revokeMembership(membership: Membership): void {
  if (!projectRef.value || !membership.nextActions.includes("REVOKE")) return;
  mutationProblem.value = undefined;
  confirmation.value = { kind: "REVOKE_MEMBERSHIP", membership };
}

function closeConfirmation(force = false): void {
  if (mutationBusy.value && !force) return;
  confirmation.value = undefined;
  mutationProblem.value = undefined;
}

async function confirmMutation(): Promise<void> {
  const requested = confirmation.value;
  if (!requested) return;
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    if (requested.kind === "ARCHIVE_ROLE") {
      await access.archiveRole(requested.role);
    } else if (requested.kind === "REVOKE_BINDING") {
      await access.revokeBinding(requested.binding);
    } else {
      await access.revokeProjectMembership(
        projectRef.value,
        requested.membership,
      );
    }
    closeConfirmation(true);
  } catch (error) {
    mutationProblem.value = asProblem(error);
    if (mutationProblem.value.kind === "conflict") {
      if (requested.kind === "ARCHIVE_ROLE") {
        await access.loadRoles(true);
      } else if (requested.kind === "REVOKE_BINDING") {
        await access.loadBindings({
          projectRef: projectRef.value || undefined,
          includeRevoked: true,
        });
      } else {
        await access.loadMembershipPresentation(
          projectRef.value,
          memberRef.value,
        );
      }
    }
  } finally {
    mutationBusy.value = false;
  }
}

async function createBinding(subject?: AccessSubject): Promise<void> {
  if (mutationBusy.value) return;
  mutationBusy.value = true;
  selectedBinding.value = undefined;
  initialSubject.value = subject;
  editorAgentsProjectRef.value = projectRef.value;
  mutationProblem.value = undefined;
  try {
    await access.loadBindingRoles();
    if (access.problems.bindingRoles) {
      mutationProblem.value = access.problems.bindingRoles;
      return;
    }
    bindingDialog.value = true;
  } finally {
    mutationBusy.value = false;
  }
}

function createGroupBinding(group: OidcGroup): void {
  void createBinding({
    ref: group.ref,
    kind: "OIDC_GROUP",
    displayName: group.displayName,
    active: group.state === "ACTIVE",
    oidcGroupRefs: [],
  });
}

async function editBinding(binding: AccessBinding): Promise<void> {
  if (mutationBusy.value) return;
  mutationBusy.value = true;
  selectedBinding.value = binding;
  initialSubject.value = undefined;
  editorAgentsProjectRef.value = binding.scope.projectRef ?? "";
  mutationProblem.value = undefined;
  try {
    await access.loadBindingRoles();
    if (access.problems.bindingRoles) {
      mutationProblem.value = access.problems.bindingRoles;
      return;
    }
    bindingDialog.value = true;
    if (
      binding.scope.kind === "RESOURCE_INSTANCE" &&
      ["AGENT", "WORKFLOW"].includes(binding.scope.resourceKind ?? "") &&
      binding.scope.projectRef
    )
      void loadProjectResources(binding.scope.projectRef);
  } finally {
    mutationBusy.value = false;
  }
}

async function saveBinding(
  input: AccessBindingInput | AccessBindingChangeInput,
): Promise<void> {
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    await access.saveBinding(input, selectedBinding.value);
    bindingDialog.value = false;
  } catch (error) {
    mutationProblem.value = asProblem(error);
    if (mutationProblem.value.kind === "conflict") {
      await access.loadBindings({
        projectRef: projectRef.value || undefined,
        includeRevoked: true,
      });
    }
  } finally {
    mutationBusy.value = false;
  }
}

watch(routeSection, (section) => void loadSection(section));
watch([projectRef, memberRef], ([value, selected]) => {
  void Promise.all([
    loadSection(),
    access.loadMembershipPresentation(value, selected),
  ]);
});
onMounted(() => void loadBaseline());
</script>

<template>
  <PageFrame
    :title="$t('access.workspaceTitle')"
    :subtitle="
      $t(
        projectRef
          ? 'access.workspaceProjectSubtitle'
          : 'access.workspaceSubtitle',
      )
    "
  >
    <AccessModelOverview :project-context="Boolean(projectRef)" />
    <AccessTabs
      :active="routeSection"
      :counts="counts"
      @select="selectSection"
    />
    <ProblemNotice
      v-if="access.problems.permissions"
      :problem="access.problems.permissions"
      @retry="access.loadPermissions"
    />
    <ProblemNotice
      v-if="mutationProblem && !roleDialog && !bindingDialog && !confirmation"
      :problem="mutationProblem"
      compact
    />

    <ParticipantsPanel
      v-if="routeSection === 'participants'"
      :initial-query="memberSearch"
      :selected-subject-ref="selectedSubjectRef"
      :subjects="participantSubjects"
      :groups="access.groups"
      :bindings="access.bindings"
      :platform-memberships="access.platformMemberships"
      :project-memberships="access.projectMemberships"
      :project-ref="projectRef"
      :platform-memberships-unavailable="
        Boolean(access.problems.platformMemberships)
      "
      :project-memberships-unavailable="
        Boolean(access.problems.projectMemberships)
      "
      :loading="access.loading.subjects"
      :problem="access.problems.subjects"
      :has-more="Boolean(access.subjectNextPageToken)"
      :mutation-busy="mutationBusy"
      @edit-membership="editMembership"
      @revoke-membership="revokeMembership"
      @search="
        (query, pageSize) =>
          access.loadSubjects(query, undefined, false, pageSize)
      "
      @more="
        (query, pageSize) =>
          access.loadSubjects(query, undefined, true, pageSize)
      "
      @bind="createBinding"
      @retry="loadSection"
    />
    <GroupsPanel
      v-else-if="routeSection === 'groups'"
      :groups="access.groups"
      :bindings="access.bindings"
      :bindings-unavailable="Boolean(access.problems.bindings)"
      :loading="access.loading.groups"
      :problem="access.problems.groups"
      :has-more="Boolean(access.groupNextPageToken)"
      @search="(query, pageSize) => access.loadGroups(query, false, pageSize)"
      @more="(query, pageSize) => access.loadGroups(query, true, pageSize)"
      @bind="createGroupBinding"
      @retry="loadSection"
    />
    <RolesPanel
      v-else-if="routeSection === 'roles'"
      :roles="access.roles"
      :permissions="access.permissions"
      :permission-registry-unavailable="Boolean(access.problems.permissions)"
      :loading="access.loading.roles"
      :problem="access.problems.roles"
      :has-more="Boolean(access.roleNextPageToken)"
      @create="createRole"
      @edit="editRole"
      @archive="archiveRole"
      @search="
        (query, pageSize) => access.loadRoles(true, false, pageSize, query)
      "
      @more="(query, pageSize) => access.loadRoles(true, true, pageSize, query)"
      @retry="loadSection"
    />
    <BindingsPanel
      v-else-if="routeSection === 'bindings'"
      :bindings="access.bindings"
      :roles="access.roles"
      :projects="access.projects"
      :agents-by-project="access.agents"
      :loading="access.loading.bindings"
      :problem="access.problems.bindings"
      :has-more="Boolean(access.bindingNextPageToken)"
      @create="createBinding()"
      @edit="editBinding"
      @revoke="revokeBinding"
      @search="
        (query, includeRevoked, pageSize) =>
          access.loadBindings(
            {
              query,
              projectRef: projectRef || undefined,
              includeRevoked,
            },
            false,
            pageSize,
          )
      "
      @more="
        (query, includeRevoked, pageSize) =>
          access.loadBindings(
            {
              query,
              projectRef: projectRef || undefined,
              includeRevoked,
            },
            true,
            pageSize,
          )
      "
      @retry="loadSection"
    />
    <EffectiveAccessPanel
      v-else
      :subjects="bindingSubjects"
      :permissions="access.permissions"
      :roles="access.roles"
      :projects="access.projects"
      :agents="editorAgents"
      :workflows="editorWorkflows"
      :integrations="access.integrations"
      :effective="access.effective"
      :explanation="access.explanation"
      :simulation="access.simulation"
      :loading="
        access.loading.effective ||
        access.loading.explanation ||
        access.loading.simulation
      "
      :problem="
        access.problems.effective ||
        access.problems.explanation ||
        access.problems.simulation
      "
      @query="access.queryEffective"
      @explain="access.explain"
      @simulate="access.simulate"
      @load-project-resources="loadProjectResources"
      @clear="access.clearDecision"
    />

    <RoleEditorDialog
      v-if="roleDialog"
      :role="selectedRole"
      :permissions="access.permissions"
      :versions="
        selectedRole ? (access.roleVersions[selectedRole.ref] ?? []) : []
      "
      :busy="mutationBusy"
      :problem="mutationProblem"
      @close="roleDialog = false"
      @save="saveRole"
    />
    <ProjectMembershipEditorDialog
      v-if="selectedMembership"
      :membership="selectedMembership"
      :busy="mutationBusy"
      :problem="mutationProblem"
      @close="selectedMembership = undefined"
      @save="saveMembership"
    />
    <BindingEditorDialog
      v-if="bindingDialog"
      :binding="selectedBinding"
      :initial-subject="initialSubject"
      :default-project-ref="projectRef"
      :subjects="bindingSubjects"
      :roles="access.bindingRoles"
      :permissions="access.permissions"
      :projects="access.projects"
      :agents="editorAgents"
      :workflows="editorWorkflows"
      :integrations="access.integrations"
      :busy="mutationBusy"
      :problem="mutationProblem"
      @close="bindingDialog = false"
      @save="saveBinding"
      @load-project-resources="loadProjectResources"
    />
    <ModalDialog
      v-if="confirmation"
      :title="
        confirmation.kind === 'ARCHIVE_ROLE'
          ? 'Архивировать роль'
          : confirmation.kind === 'REVOKE_BINDING'
            ? 'Отозвать назначение'
            : t('access.projectMembershipEditor.revoke')
      "
      :busy="mutationBusy"
      size="md"
      @close="closeConfirmation"
    >
      <p class="confirmation-copy">
        {{
          confirmation.kind === "ARCHIVE_ROLE"
            ? t("access.rolesWorkspace.archiveConfirm", {
                name: confirmation.role.currentVersion.name,
              })
            : confirmation.kind === "REVOKE_BINDING"
              ? t("access.bindingsWorkspace.revokeConfirm")
              : t("access.projectMembershipEditor.revokeConfirm", {
                  name: confirmation.membership.user.displayName,
                })
        }}
      </p>
      <ProblemNotice
        v-if="mutationProblem"
        :problem="mutationProblem"
        compact
      />
      <template #actions>
        <button
          class="button"
          type="button"
          :disabled="mutationBusy"
          @click="closeConfirmation()"
        >
          {{ $t("common.cancel") }}
        </button>
        <button
          class="button button--danger"
          type="button"
          :disabled="mutationBusy"
          @click="confirmMutation"
        >
          {{
            mutationBusy
              ? "Выполняем…"
              : confirmation.kind === "ARCHIVE_ROLE"
                ? "Архивировать"
                : confirmation.kind === "REVOKE_BINDING"
                  ? "Отозвать"
                  : t("access.projectMembershipEditor.revoke")
          }}
        </button>
      </template>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.confirmation-copy {
  margin: 0;
}
</style>
