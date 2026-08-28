<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import AccessTabs from "@/features/access/components/AccessTabs.vue";
import BindingEditorDialog from "@/features/access/components/BindingEditorDialog.vue";
import BindingsPanel from "@/features/access/components/BindingsPanel.vue";
import EffectiveAccessPanel from "@/features/access/components/EffectiveAccessPanel.vue";
import GroupsPanel from "@/features/access/components/GroupsPanel.vue";
import ParticipantsPanel from "@/features/access/components/ParticipantsPanel.vue";
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
  OidcGroup,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const access = useAccessStore();
const route = useRoute();
const router = useRouter();
const { t } = useI18n();

const projectRef = computed(() =>
  typeof route.params.projectRef === "string" ? route.params.projectRef : "",
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
const roleDialog = ref(false);
const bindingDialog = ref(false);
const selectedRole = ref<AccessRole>();
const selectedBinding = ref<AccessBinding>();
const initialSubject = ref<AccessSubject>();
const mutationBusy = ref(false);
const mutationProblem = ref<AppProblem>();

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
      access.loadSubjects(),
      access.loadBindings({ projectRef: projectRef.value || undefined }),
    ]);
  } else if (section === "groups") {
    await Promise.all([access.loadGroups(), access.loadSubjects()]);
  } else if (section === "roles") {
    await access.loadRoles(true);
  } else if (section === "bindings") {
    await access.loadBindings({
      projectRef: projectRef.value || undefined,
      includeRevoked: true,
    });
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
  ]);
  await loadSection();
}

async function loadAgents(value: string): Promise<void> {
  editorAgentsProjectRef.value = value;
  await access.loadAgents(value);
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

async function archiveRole(role: AccessRole): Promise<void> {
  if (
    !window.confirm(
      t("access.rolesWorkspace.archiveConfirm", {
        name: role.currentVersion.name,
      }),
    )
  )
    return;
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    await access.archiveRole(role);
  } catch (error) {
    mutationProblem.value = asProblem(error);
    if (mutationProblem.value.kind === "conflict") await access.loadRoles(true);
  } finally {
    mutationBusy.value = false;
  }
}

function createBinding(subject?: AccessSubject): void {
  selectedBinding.value = undefined;
  initialSubject.value = subject;
  editorAgentsProjectRef.value = projectRef.value;
  mutationProblem.value = undefined;
  bindingDialog.value = true;
}

function createGroupBinding(group: OidcGroup): void {
  createBinding({
    ref: group.ref,
    kind: "OIDC_GROUP",
    displayName: group.displayName,
    active: group.state === "ACTIVE",
    oidcGroupRefs: [],
  });
}

function editBinding(binding: AccessBinding): void {
  selectedBinding.value = binding;
  initialSubject.value = undefined;
  editorAgentsProjectRef.value = binding.scope.projectRef ?? "";
  mutationProblem.value = undefined;
  bindingDialog.value = true;
  if (
    binding.scope.kind === "RESOURCE_INSTANCE" &&
    binding.scope.resourceKind === "AGENT" &&
    binding.scope.projectRef
  )
    void loadAgents(binding.scope.projectRef);
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

async function revokeBinding(binding: AccessBinding): Promise<void> {
  if (!window.confirm(t("access.bindingsWorkspace.revokeConfirm"))) return;
  mutationBusy.value = true;
  mutationProblem.value = undefined;
  try {
    await access.revokeBinding(binding);
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
watch(projectRef, () => void loadSection());
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
    <AccessTabs
      :active="routeSection"
      :counts="counts"
      @select="selectSection"
    />
    <ProblemNotice
      v-if="mutationProblem && !roleDialog && !bindingDialog"
      :problem="mutationProblem"
      compact
    />

    <ParticipantsPanel
      v-if="routeSection === 'participants'"
      :subjects="participantSubjects"
      :groups="access.groups"
      :bindings="access.bindings"
      :loading="access.loading.subjects"
      :problem="access.problems.subjects"
      :has-more="Boolean(access.subjectNextPageToken)"
      @search="access.loadSubjects($event)"
      @more="access.loadSubjects($event, undefined, true)"
      @bind="createBinding"
      @retry="loadSection"
    />
    <GroupsPanel
      v-else-if="routeSection === 'groups'"
      :groups="access.groups"
      :loading="access.loading.groups"
      :problem="access.problems.groups"
      :has-more="Boolean(access.groupNextPageToken)"
      @search="access.loadGroups($event)"
      @more="access.loadGroups($event, true)"
      @bind="createGroupBinding"
      @retry="loadSection"
    />
    <RolesPanel
      v-else-if="routeSection === 'roles'"
      :roles="access.roles"
      :loading="access.loading.roles"
      :problem="access.problems.roles"
      :has-more="Boolean(access.roleNextPageToken)"
      @create="createRole"
      @edit="editRole"
      @archive="archiveRole"
      @more="access.loadRoles(true, true)"
      @retry="loadSection"
    />
    <BindingsPanel
      v-else-if="routeSection === 'bindings'"
      :bindings="access.bindings"
      :projects="access.projects"
      :agents-by-project="access.agents"
      :loading="access.loading.bindings"
      :problem="access.problems.bindings"
      :has-more="Boolean(access.bindingNextPageToken)"
      @create="createBinding()"
      @edit="editBinding"
      @revoke="revokeBinding"
      @more="
        access.loadBindings(
          { projectRef: projectRef || undefined, includeRevoked: true },
          true,
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
      @load-agents="loadAgents"
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
    <BindingEditorDialog
      v-if="bindingDialog"
      :binding="selectedBinding"
      :initial-subject="initialSubject"
      :default-project-ref="projectRef"
      :subjects="bindingSubjects"
      :roles="access.roles"
      :permissions="access.permissions"
      :projects="access.projects"
      :agents="editorAgents"
      :busy="mutationBusy"
      :problem="mutationProblem"
      @close="bindingDialog = false"
      @save="saveBinding"
      @load-agents="loadAgents"
    />
  </PageFrame>
</template>
