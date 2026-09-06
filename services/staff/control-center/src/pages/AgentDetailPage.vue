<script setup lang="ts">
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { Play, Power, PowerOff } from "@lucide/vue";
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import { onBeforeRouteUpdate, useRoute, useRouter } from "vue-router";

import InstructionHistory from "@/features/agents/components/InstructionHistory.vue";
import AgentAccessPanel from "@/features/agents/detail/AgentAccessPanel.vue";
import AgentApplyState from "@/features/agents/detail/AgentApplyState.vue";
import AvatarCropDialog from "@/features/agents/detail/AvatarCropDialog.vue";
import AgentEnvironmentPanel from "@/features/agents/detail/AgentEnvironmentPanel.vue";
import AgentInstructionsPanel from "@/features/agents/detail/AgentInstructionsPanel.vue";
import AgentProfilePanel from "@/features/agents/detail/AgentProfilePanel.vue";
import AgentRuntimePanel from "@/features/agents/detail/AgentRuntimePanel.vue";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import {
  agentDetailTabFromQuery,
  sameProfileDraft,
  type AgentBackendFeatureAvailability,
  type AgentDetailTab,
  type AgentProfileDraft,
  type ApplyBoundary,
} from "@/features/agents/detail/model";
import { usePlatformStore } from "@/features/platform/store";
import { requestSignal } from "@/shared/api/client";
import {
  removeAgentAvatar,
  uploadAgentAvatar,
} from "@/shared/api/generated/openapi/sdk.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { runPath } from "@/shared/routes";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import PublicationImpactSelection from "@/features/runtime/PublicationImpactSelection.vue";
import {
  prepareInstructionPublication,
  publishInstructions,
  readInstructionPublicationAgent,
} from "@/features/agents/detail/instruction-publication";
import {
  publicationPlanIdentity,
  publicationSelection,
  restorePublicationImpact,
} from "@/features/runtime/publication-impact";
import type { RevisionImpactPlan } from "@/shared/api/generated/openapi/types.gen";
import {
  readPublicationAttempt,
  rememberPublicationAttempt,
  publicationRefusalClearsIntent,
  forgetPublicationAttempt,
  type PublicationAttempt,
} from "@/features/runtime/publication-attempt";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";

const platform = usePlatformStore();
const { locale, t } = useI18n();
const route = useRoute();
const router = useRouter();
const agentRef = computed(() => String(route.params.agentRef));
const projectRef = computed(() => String(route.params.projectRef));
const agent = computed(() => platform.agents[agentRef.value]);
const canEdit = computed(
  () => agent.value?.nextActions.includes("EDIT") ?? false,
);
const canManageAvatar = computed(() => canEdit.value);
const canManageCapabilities = computed(
  () => agent.value?.nextActions.includes("MANAGE_CAPABILITIES") ?? false,
);
const instructionHistory = computed(
  () => platform.instructionVersions[agentRef.value] ?? [],
);
const instructionState = computed(
  () =>
    agent.value?.draftInstructions?.state ??
    agent.value?.publishedInstructions?.state ??
    "DRAFT",
);
const instructionValidationMessages = computed(
  () => agent.value?.draftInstructions?.validationMessages ?? [],
);

const activeTab = ref<AgentDetailTab>(agentDetailTabFromQuery(route.query.tab));
const profileDraft = ref<AgentProfileDraft>({
  name: "",
  purpose: "",
  roleDescription: "",
});
const instructions = ref("");
const task = ref("");
const avatarFile = ref<File>();
const busy = ref(false);
const loaded = ref(false);
let contextGeneration = 0;
function captureScope(): () => boolean {
  const generation = contextGeneration;
  const agent = agentRef.value;
  const project = projectRef.value;
  return () =>
    generation === contextGeneration &&
    agent === agentRef.value &&
    project === projectRef.value;
}
const capabilityBusy = ref("");
const problem = ref<AppProblem>();
const instructionPlan = ref<RevisionImpactPlan>();
const instructionUnknown = ref(false);
const instructionAttempt = ref<PublicationAttempt>();
let instructionKey = "";
function clearInstructionAttempt(ref: string): void {
  forgetPublicationAttempt("AGENT_INSTRUCTIONS", ref, window.sessionStorage);
  instructionAttempt.value = undefined;
}
watch(agentRef, () => {
  instructionPlan.value = undefined;
  instructionUnknown.value = false;
  instructionAttempt.value = undefined;
});
const applyState = ref<"APPLIED" | "DRAFT" | "RUNNING" | "FAILED">("APPLIED");
const applyScope = ref(t("agents.profile"));
const applyBoundary = ref<ApplyBoundary>("next-run");
const tabApplyStates = reactive<
  Record<
    AgentDetailTab,
    {
      state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED";
      scope: string;
      boundary: ApplyBoundary;
    }
  >
>({
  profile: { state: "APPLIED", scope: "profile", boundary: "next-run" },
  instructions: {
    state: "APPLIED",
    scope: "instructions",
    boundary: "published",
  },
  runtime: { state: "APPLIED", scope: "runtime", boundary: "next-turn" },
  environment: {
    state: "APPLIED",
    scope: "environment",
    boundary: "next-turn",
  },
  access: { state: "APPLIED", scope: "access", boundary: "next-run" },
});
const avatarAsset = computed<AgentBackendFeatureAvailability>(() =>
  canManageAvatar.value
    ? { state: "AVAILABLE", code: "avatar_asset" }
    : {
        state: "UNAVAILABLE",
        code: "avatar_asset",
        reason: agentDetailCopy(locale.value).gaps.avatar,
      },
);

const currentProfile = computed<AgentProfileDraft>(() => ({
  name: agent.value?.name ?? "",
  purpose: agent.value?.purpose ?? "",
  roleDescription: agent.value?.roleDescription ?? "",
}));
const profileDirty = computed(
  () => !sameProfileDraft(profileDraft.value, currentProfile.value),
);
const authoritativeInstructions = computed(
  () =>
    agent.value?.draftInstructions?.content ??
    agent.value?.publishedInstructions?.content ??
    "",
);
const instructionsDirty = computed(
  () => instructions.value !== authoritativeInstructions.value,
);
useUnsavedChanges(
  computed(
    () =>
      busy.value ||
      Boolean(capabilityBusy.value) ||
      (loaded.value && (profileDirty.value || instructionsDirty.value)),
  ),
  () => t("managed.discard"),
  { ignoreQueryOnly: true },
);
onBeforeRouteUpdate(
  (to, from) => to.path !== from.path || (!busy.value && !capabilityBusy.value),
);
const applyReadback = computed(() => {
  const value = agent.value;
  if (!value) return undefined;
  if (activeTab.value === "instructions") {
    const draft = value.draftInstructions?.revision;
    const published = value.publishedInstructions?.revision;
    return [
      draft !== undefined ? `draft r${String(draft)}` : undefined,
      published !== undefined ? `published r${String(published)}` : undefined,
    ]
      .filter(Boolean)
      .join(" · ");
  }
  if (activeTab.value === "runtime")
    return [value.runtimeName, value.runtimeRevision]
      .filter(Boolean)
      .join(" · ");
  return `Agent v${String(value.version)}`;
});

const tabs = computed<Array<{ id: AgentDetailTab; label: string }>>(() => [
  { id: "profile", label: t("agents.profile") },
  { id: "instructions", label: t("agents.instructions") },
  { id: "runtime", label: "Runtime" },
  { id: "environment", label: t("roleEnvironments.title") },
  { id: "access", label: t("agents.capabilities") },
]);

function tabScope(tab: AgentDetailTab): string {
  return tabs.value.find((item) => item.id === tab)?.label ?? tab;
}

function tabBoundary(tab: AgentDetailTab): ApplyBoundary {
  if (tab === "instructions") return "published";
  if (tab === "runtime" || tab === "environment") return "next-turn";
  return "next-run";
}

function tabHasDraft(tab: AgentDetailTab): boolean {
  if (tab === "profile") return profileDirty.value;
  if (tab === "instructions") return instructionsDirty.value;
  return false;
}

function activateTab(tab: AgentDetailTab): void {
  activeTab.value = tab;
  const snapshot = tabApplyStates[tab];
  if (tabHasDraft(tab)) snapshot.state = "DRAFT";
  snapshot.scope = tabScope(tab);
  snapshot.boundary = tabBoundary(tab);
  applyState.value = snapshot.state;
  applyScope.value = snapshot.scope;
  applyBoundary.value = snapshot.boundary;
}

function selectTab(tab: AgentDetailTab): void {
  if (busy.value || capabilityBusy.value) return;
  if (route.query.tab !== tab) {
    void router.replace({ query: { ...route.query, tab } });
  }
}

function setApplyState(
  state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED",
  scope: string,
  boundary: ApplyBoundary,
): void {
  const snapshot = tabApplyStates[activeTab.value];
  snapshot.state = state;
  snapshot.scope = scope;
  snapshot.boundary = boundary;
  applyState.value = state;
  applyScope.value = scope;
  applyBoundary.value = boundary;
}

function markDraft(scope: string, boundary: ApplyBoundary): void {
  setApplyState("DRAFT", scope, boundary);
}

function markApplying(scope: string, boundary: ApplyBoundary): void {
  setApplyState("RUNNING", scope, boundary);
}

function markApplied(): void {
  setApplyState("APPLIED", applyScope.value, applyBoundary.value);
}

function markCurrent(scope: string, boundary: ApplyBoundary): void {
  applyScope.value = scope;
  applyBoundary.value = boundary;
  markApplied();
}

function markFailed(): void {
  setApplyState("FAILED", applyScope.value, applyBoundary.value);
}

function syncProfile(value = agent.value): void {
  if (!value) return;
  profileDraft.value = {
    name: value.name,
    purpose: value.purpose,
    roleDescription: value.roleDescription,
  };
}

function syncInstructions(): void {
  instructions.value = authoritativeInstructions.value;
}

async function load(): Promise<void> {
  const active = captureScope();
  await Promise.all([
    platform.loadProject(projectRef.value),
    platform.loadAgent(agentRef.value),
    platform.loadInstructionVersions(agentRef.value),
    platform.loadCapabilities(),
  ]);
  if (!active()) return;
  syncProfile();
  syncInstructions();
  loaded.value = true;
}

function avatarMutationHeaders(headers: MutationHeaders) {
  const version = headers["If-Match"];
  if (!version) throw new Error("Agent avatar version header is unavailable");
  return {
    "Idempotency-Key": headers["Idempotency-Key"],
    "If-Match": version,
    "X-CSRF-Token": headers["X-CSRF-Token"],
  };
}

async function uploadAvatar(file: File): Promise<void> {
  const current = agent.value;
  if (!current) throw new Error("Agent state is unavailable");
  const result = await mutate(
    (headers) =>
      uploadAgentAvatar({
        path: { projectRef: projectRef.value, agentRef: current.ref },
        body: file,
        headers: {
          ...avatarMutationHeaders(headers),
          "X-File-Name": file.name,
        },
        signal: requestSignal(),
      }),
    current.version,
  );
  platform.agents[result.data.ref] = result.data;
}

async function clearAvatar(): Promise<void> {
  const current = agent.value;
  if (!current) throw new Error("Agent state is unavailable");
  const result = await mutate(
    (headers) =>
      removeAgentAvatar({
        path: { agentRef: current.ref },
        headers: avatarMutationHeaders(headers),
        signal: requestSignal(),
      }),
    current.version,
  );
  platform.agents[result.data.ref] = result.data;
}

function markAvatarApplied(): void {
  if (profileDirty.value)
    setApplyState("DRAFT", tabScope("profile"), "next-run");
  else setApplyState("APPLIED", tabScope("profile"), "next-run");
}

async function applyAvatar(file: File): Promise<void> {
  if (!agent.value || !canManageAvatar.value || busy.value) return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    await uploadAvatar(file);
    if (!active()) return;
    avatarFile.value = undefined;
    markAvatarApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

async function removeAvatar(): Promise<void> {
  if (!agent.value?.avatar?.artifactRef || !canManageAvatar.value || busy.value)
    return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    await clearAvatar();
    if (!active()) return;
    markAvatarApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

function updateProfile(value: AgentProfileDraft): void {
  if (busy.value || !canEdit.value) return;
  profileDraft.value = value;
  if (sameProfileDraft(value, currentProfile.value))
    markCurrent(tabScope("profile"), "next-run");
  else markDraft(tabScope("profile"), "next-run");
}

function updateInstructions(value: string): void {
  if (busy.value || !canEdit.value) return;
  instructions.value = value;
  if (value === authoritativeInstructions.value)
    markCurrent(tabScope("instructions"), "published");
  else markDraft(tabScope("instructions"), "published");
}

async function saveProfile(): Promise<void> {
  if (busy.value || !agent.value || !canEdit.value || !profileDirty.value)
    return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    const updated = await platform.saveAgent(
      projectRef.value,
      {
        name: profileDraft.value.name.trim(),
        purpose: profileDraft.value.purpose.trim(),
        roleDescription: profileDraft.value.roleDescription.trim(),
        roleDefinitionRef: agent.value.roleDefinitionRef,
        runtimeRef: agent.value.runtimeRef,
      },
      agent.value,
    );
    if (!active()) return;
    syncProfile(updated);
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

async function saveInstructions(): Promise<void> {
  if (
    busy.value ||
    !agent.value?.nextActions.includes("EDIT") ||
    !instructionsDirty.value ||
    !instructions.value.trim()
  )
    return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  try {
    const updated = await platform.saveInstructions(
      agent.value,
      instructions.value,
    );
    if (!active()) return;
    instructions.value =
      updated.draftInstructions?.content ??
      updated.publishedInstructions?.content ??
      "";
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

async function publishInstructionSelection(selected: string[]): Promise<void> {
  const hadUnknownAttempt = instructionAttempt.value !== undefined;
  const current = agent.value,
    plan = instructionPlan.value;
  if (
    !current ||
    !plan ||
    busy.value ||
    instructionUnknown.value ||
    instructionsDirty.value
  )
    return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    publicationSelection(plan, selected);
    if (
      current.version !== plan.sourceVersion ||
      current.draftInstructions?.ref !== plan.draftRef ||
      current.draftInstructions.version !== plan.draftVersion
    )
      throw new Error("Instruction publication intent changed");
    const attempt: PublicationAttempt = {
      kind: "AGENT_INSTRUCTIONS",
      ownerRef: current.ref,
      planRef: plan.ref,
      version: current.version,
      selectedItemRefs: [...selected],
      key: instructionKey,
    };
    rememberPublicationAttempt(attempt, window.sessionStorage);
    instructionAttempt.value = attempt;
    instructionUnknown.value = true;
    const result = await publishInstructions(
      current,
      plan,
      selected,
      instructionKey,
    );
    if (!active()) return;
    instructionPlan.value = result.plan;
    instructionUnknown.value = false;
    clearInstructionAttempt(current.ref);
    await platform.loadAgent(current.ref);
    if (platform.problems.agent) throw platform.problems.agent;
    await platform.loadInstructionVersions(current.ref);
    if (platform.problems.instructionVersions)
      throw platform.problems.instructionVersions;
    if (active()) markApplied();
  } catch (error) {
    if (!active()) return;
    const normalized = asProblem(error);
    if (publicationRefusalClearsIntent(hadUnknownAttempt, normalized.status)) {
      instructionUnknown.value = false;
      clearInstructionAttempt(current.ref);
    }
    problem.value = normalized;
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}
async function retryInstructionPublication(): Promise<void> {
  const attempt = instructionAttempt.value,
    plan = instructionPlan.value;
  if (!attempt || !plan || busy.value || instructionsDirty.value) return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  try {
    const report = await restorePublicationImpact(
      attempt.planRef,
      requestSignal(),
    );
    if (!active()) return;
    if (publicationPlanIdentity(report.plan) !== publicationPlanIdentity(plan))
      throw new Error("Instruction recovery plan changed");
    if (report.plan.state !== "PREPARED") {
      busy.value = false;
      await recoverInstructionPublication();
      return;
    }
    const fresh = await readInstructionPublicationAgent(attempt.ownerRef);
    if (!active()) return;
    if (
      fresh.version !== attempt.version ||
      agent.value?.version !== attempt.version ||
      report.plan.sourceVersion !== attempt.version ||
      fresh.draftInstructions?.ref !== plan.draftRef ||
      fresh.draftInstructions.version !== plan.draftVersion
    )
      throw new Error(
        "Original instruction publication intent is no longer applicable",
      );
    instructionPlan.value = report.plan;
    instructionKey = attempt.key;
    instructionUnknown.value = false;
    busy.value = false;
    await publishInstructionSelection(attempt.selectedItemRefs);
  } catch (error) {
    if (active()) problem.value = asProblem(error);
  } finally {
    if (active()) busy.value = false;
  }
}
async function recoverInstructionPublication(): Promise<void> {
  const current = agent.value,
    plan = instructionPlan.value;
  if (!current || !plan || busy.value) return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  try {
    const report = await restorePublicationImpact(plan.ref, requestSignal());
    if (!active()) return;
    if (
      publicationPlanIdentity(report.plan) === publicationPlanIdentity(plan) &&
      report.plan.state === "EXPIRED"
    ) {
      instructionPlan.value = report.plan;
      instructionUnknown.value = false;
      clearInstructionAttempt(current.ref);
      return;
    }
    if (
      publicationPlanIdentity(report.plan) !== publicationPlanIdentity(plan) ||
      report.plan.state !== "APPLIED"
    )
      throw new Error("Instruction publication outcome is not confirmed");
    instructionPlan.value = report.plan;
    instructionUnknown.value = false;
    clearInstructionAttempt(current.ref);
    await platform.loadAgent(current.ref);
    if (platform.problems.agent) throw platform.problems.agent;
    await platform.loadInstructionVersions(current.ref);
    if (platform.problems.instructionVersions)
      throw platform.problems.instructionVersions;
    if (active()) markApplied();
  } catch (error) {
    if (active()) problem.value = asProblem(error);
  } finally {
    if (active()) busy.value = false;
  }
}
async function instructionAction(
  action: "VALIDATE" | "PUBLISH",
): Promise<void> {
  if (
    busy.value ||
    !agent.value?.nextActions.includes(action) ||
    instructionsDirty.value
  )
    return;
  const active = captureScope();
  const ref = agentRef.value;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    if (action === "PUBLISH") {
      const stored = readPublicationAttempt(
        "AGENT_INSTRUCTIONS",
        ref,
        window.sessionStorage,
      );
      const plan = stored
        ? (await restorePublicationImpact(stored.planRef, requestSignal())).plan
        : await prepareInstructionPublication(agent.value);
      if (!active()) return;
      if (
        plan.kind !== "AGENT_INSTRUCTIONS" ||
        plan.sourceRef !== ref ||
        (stored && plan.sourceVersion !== stored.version)
      )
        throw new Error("Instruction publication scope mismatch");
      instructionPlan.value = plan;
      instructionAttempt.value = stored;
      instructionUnknown.value = !!stored && plan.state === "PREPARED";
      instructionKey = stored?.key ?? crypto.randomUUID();
      if (stored && plan.state !== "PREPARED") clearInstructionAttempt(ref);
      return;
    }
    markApplying(tabScope("instructions"), "published");
    const updated = await platform.instructionCommand(agent.value, action);
    if (!active()) return;
    instructions.value =
      updated.draftInstructions?.content ??
      updated.publishedInstructions?.content ??
      "";
    await platform.loadInstructionVersions(ref);
    if (!active()) return;
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

async function rollbackInstructions(
  publishedInstructionRef: string,
): Promise<void> {
  if (busy.value || !agent.value?.nextActions.includes("ROLLBACK")) return;
  const active = captureScope();
  const ref = agentRef.value;
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("instructions"), "published");
  try {
    const updated = await platform.instructionCommand(
      agent.value,
      "ROLLBACK",
      publishedInstructionRef,
    );
    if (!active()) return;
    instructions.value = updated.publishedInstructions?.content ?? "";
    await platform.loadInstructionVersions(ref);
    if (!active()) return;
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

function updateApplyState(
  state: "APPLIED" | "DRAFT" | "RUNNING" | "FAILED",
  scope: string,
  boundary: ApplyBoundary,
): void {
  setApplyState(state, scope, boundary);
}

async function toggleCapability(
  key: string,
  enabled: boolean,
  version: number,
): Promise<void> {
  if (
    busy.value ||
    !agent.value ||
    agent.value.version !== version ||
    !canManageCapabilities.value ||
    capabilityBusy.value
  )
    return;
  const active = captureScope();
  capabilityBusy.value = key;
  problem.value = undefined;
  markApplying(tabScope("access"), "next-run");
  try {
    await platform.changeAgent(agent.value, {
      action: enabled ? "GRANT_CAPABILITY" : "REVOKE_CAPABILITY",
      capabilityKey: key,
    });
    if (!active()) return;
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) capabilityBusy.value = "";
  }
}

async function launch(): Promise<void> {
  if (
    busy.value ||
    !agent.value?.nextActions.includes("LAUNCH") ||
    !task.value.trim()
  )
    return;
  const active = captureScope();
  const project = projectRef.value;
  busy.value = true;
  problem.value = undefined;
  try {
    const run = await platform.launch({
      projectRef: projectRef.value,
      targetRef: agent.value.ref,
      targetType: "AGENT",
      title: task.value.trim().slice(0, 160),
      task: task.value.trim(),
    });
    if (!active()) return;
    busy.value = false;
    await router.push(runPath(run.ref, project));
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
  } finally {
    if (active()) busy.value = false;
  }
}

async function toggle(): Promise<void> {
  if (
    busy.value ||
    !agent.value?.nextActions.includes(
      agent.value.enabled ? "DISABLE" : "ENABLE",
    )
  )
    return;
  const active = captureScope();
  busy.value = true;
  problem.value = undefined;
  markApplying(tabScope("profile"), "next-run");
  try {
    await platform.changeAgent(agent.value, {
      action: agent.value.enabled ? "DISABLE" : "ENABLE",
    });
    if (!active()) return;
    markApplied();
  } catch (error) {
    if (!active()) return;
    problem.value = asProblem(error);
    markFailed();
  } finally {
    if (active()) busy.value = false;
  }
}

watch(
  () => route.query.tab,
  (value) => {
    const tab = agentDetailTabFromQuery(value);
    if (tab !== activeTab.value) activateTab(tab);
  },
);
onMounted(() => void load());
function resetContext(): void {
  contextGeneration++;
  busy.value = false;
  loaded.value = false;
  capabilityBusy.value = "";
  problem.value = undefined;
  profileDraft.value = { name: "", purpose: "", roleDescription: "" };
  instructions.value = "";
  task.value = "";
  avatarFile.value = undefined;
  for (const snapshot of Object.values(tabApplyStates))
    snapshot.state = "APPLIED";
  activateTab(agentDetailTabFromQuery(route.query.tab));
}
watch(
  [projectRef, agentRef],
  () => {
    resetContext();
    void load();
  },
  { flush: "sync" },
);
onBeforeUnmount(() => {
  contextGeneration++;
});
</script>

<template>
  <PageFrame
    :title="agent?.name ?? $t('agents.title')"
    :subtitle="agent?.purpose"
    :eyebrow="$t('nav.agent')"
  >
    <template #actions>
      <StatusBadge v-if="agent" :state="agent.state" />
      <button
        v-if="agent?.nextActions.includes(agent.enabled ? 'DISABLE' : 'ENABLE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="toggle"
      >
        <PowerOff v-if="agent.enabled" :size="16" aria-hidden="true" />
        <Power v-else :size="16" aria-hidden="true" />
        {{ agent.enabled ? $t("common.disable") : $t("common.enable") }}
      </button>
    </template>

    <AsyncState
      :loading="platform.loading.agent"
      :problem="platform.problems.agent"
      @retry="load"
    >
      <div
        v-if="agent"
        class="agent-detail-page"
        :data-agent-version="agent.version"
      >
        <AgentApplyState
          :state="applyState"
          :scope="applyScope"
          :boundary="applyBoundary"
          :readback="applyReadback"
        />

        <div class="agent-tabs" role="tablist" :aria-label="$t('nav.agent')">
          <button
            v-for="tab in tabs"
            :id="`agent-tab-${tab.id}`"
            :key="tab.id"
            class="agent-tab"
            type="button"
            role="tab"
            :disabled="busy || Boolean(capabilityBusy)"
            :aria-selected="activeTab === tab.id"
            :aria-controls="`agent-panel-${tab.id}`"
            @click="selectTab(tab.id)"
          >
            {{ tab.label }}
            <span v-if="tab.id === 'instructions' && agent.draftInstructions">
              {{ $t("states." + agent.draftInstructions.state) }}
            </span>
          </button>
        </div>

        <section
          v-if="activeTab === 'profile'"
          id="agent-panel-profile"
          class="agent-panel agent-profile-layout"
          role="tabpanel"
          aria-labelledby="agent-tab-profile"
        >
          <AgentProfilePanel
            :model-value="profileDraft"
            :role-name="agent.roleDefinitionName ?? agent.name"
            :avatar-url="
              agent.avatar?.source === 'ARTIFACT'
                ? agent.avatar.contentPath
                : undefined
            "
            :avatar-asset="avatarAsset"
            :can-edit="canEdit"
            :busy="busy"
            :dirty="profileDirty"
            @update:model-value="updateProfile"
            @upload-avatar="avatarFile = $event"
            @remove-avatar="removeAvatar"
            @save="saveProfile"
          />
          <aside class="agent-profile-aside">
            <section
              v-if="agent.nextActions.includes('LAUNCH')"
              class="panel launch-panel"
            >
              <h2>{{ $t("runs.new") }}</h2>
              <label class="field">
                <span>{{ $t("runs.task") }}</span>
                <VoiceTextarea
                  v-model="task"
                  :disabled="busy"
                  required
                  maxlength="8000"
                />
              </label>
              <button
                class="button button--primary"
                type="button"
                :disabled="busy || !task.trim()"
                @click="launch"
              >
                <Play :size="16" aria-hidden="true" />{{ $t("common.launch") }}
              </button>
            </section>
            <section class="panel agent-summary">
              <h2>{{ $t("common.details") }}</h2>
              <dl>
                <div>
                  <dt>{{ $t("agents.runtime") }}</dt>
                  <dd>{{ agent.runtimeName }}</dd>
                </div>
                <div>
                  <dt>{{ $t("agents.provider") }}</dt>
                  <dd>{{ agent.runtimeProvider ?? $t("common.noData") }}</dd>
                </div>
                <div>
                  <dt>{{ $t("agents.model") }}</dt>
                  <dd class="mono">
                    {{ agent.runtimeModel ?? $t("common.noData") }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("agents.instructions") }}</dt>
                  <dd class="agent-summary__instruction-state">
                    <template v-if="agent.publishedInstructions">
                      <span>
                        {{
                          $t("agents.revision", {
                            revision: agent.publishedInstructions.revision,
                          })
                        }}
                      </span>
                      <StatusBadge :state="agent.publishedInstructions.state" />
                    </template>
                    <template v-else>{{ $t("common.noData") }}</template>
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("agents.capabilities") }}</dt>
                  <dd>{{ agent.capabilities.length }}</dd>
                </div>
              </dl>
            </section>
          </aside>
        </section>

        <section
          v-else-if="activeTab === 'instructions'"
          id="agent-panel-instructions"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-instructions"
        >
          <AgentInstructionsPanel
            :agent-ref="agent.ref"
            :agent-version="agent.version"
            :model-value="instructions"
            :project-ref="projectRef"
            :state="instructionState"
            :validation-messages="instructionValidationMessages"
            :can-edit="canEdit"
            :can-validate="agent.nextActions.includes('VALIDATE')"
            :can-publish="agent.nextActions.includes('PUBLISH')"
            :busy="busy"
            :dirty="instructionsDirty"
            @update:model-value="updateInstructions"
            @save="saveInstructions"
            @validate="instructionAction('VALIDATE')"
            @publish="instructionAction('PUBLISH')"
          >
            <template #history>
              <p v-if="agent.instructionBinding">
                {{
                  $t(
                    agent.instructionBinding.effective
                      ? "publicationImpact.instructionsEffective"
                      : "publicationImpact.instructionsInactive",
                  )
                }}
                <code>{{ agent.instructionBinding.revisionRef }}</code>
              </p>
              <InstructionHistory
                :versions="instructionHistory"
                :current-ref="agent.instructionBinding?.revisionRef"
                :current-effective="agent.instructionBinding?.effective"
                :can-rollback="agent.nextActions.includes('ROLLBACK')"
                :busy="busy"
                @rollback="rollbackInstructions"
              />
            </template>
          </AgentInstructionsPanel>
          <ProblemNotice
            v-if="platform.problems.instructionVersions"
            :problem="platform.problems.instructionVersions"
            compact
          />
        </section>

        <section
          v-else-if="activeTab === 'runtime'"
          id="agent-panel-runtime"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-runtime"
        >
          <AgentRuntimePanel
            :agent-ref="agent.ref"
            :can-edit="canEdit"
            @apply-state="updateApplyState"
          />
          <ProblemNotice
            v-if="platform.problems.runtimes"
            :problem="platform.problems.runtimes"
            compact
          />
        </section>

        <section
          v-else-if="activeTab === 'environment'"
          id="agent-panel-environment"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-environment"
        >
          <AgentEnvironmentPanel
            :agent-ref="agent.ref"
            :project-ref="projectRef"
            :can-edit="canEdit"
            @apply-state="updateApplyState"
          />
        </section>

        <section
          v-else
          id="agent-panel-access"
          class="agent-panel"
          role="tabpanel"
          aria-labelledby="agent-tab-access"
        >
          <AgentAccessPanel
            :project-ref="agent.projectRef"
            :agent-ref="agent.ref"
            :agent-version="agent.version"
            :integrations="agent.integrations"
            :knowledge-count="agent.knowledgeArtifactRefs.length"
            :can-manage="canManageCapabilities"
            :busy-key="capabilityBusy"
            @toggle="toggleCapability"
            @refresh="platform.loadAgent(agentRef)"
          />
          <ProblemNotice
            v-if="platform.problems.capabilities"
            :problem="platform.problems.capabilities"
            compact
          />
        </section>

        <ProblemNotice v-if="problem" :problem="problem" compact />
      </div>
    </AsyncState>
    <AvatarCropDialog
      v-if="avatarFile"
      :file="avatarFile"
      :busy="busy"
      @close="avatarFile = undefined"
      @confirm="applyAvatar"
    />
    <ModalDialog
      v-if="instructionPlan"
      :title="$t('publicationImpact.title')"
      size="lg"
      :busy="busy"
      @close="instructionPlan = undefined"
    >
      <ProblemNotice v-if="problem" :problem="problem" />
      <button
        v-if="instructionUnknown"
        class="button"
        type="button"
        :disabled="busy"
        @click="recoverInstructionPublication"
      >
        {{ $t("common.refresh") }}
      </button>
      <PublicationImpactSelection
        :plan="instructionPlan"
        :busy="busy || instructionUnknown"
        @publish="publishInstructionSelection"
      />
      <button
        v-if="instructionUnknown && instructionAttempt"
        type="button"
        class="button"
        :disabled="busy"
        @click="retryInstructionPublication"
      >
        {{ $t("publicationImpact.retryOriginal") }}
      </button>
    </ModalDialog>
  </PageFrame>
</template>

<style scoped>
.agent-detail-page {
  display: grid;
  gap: 16px;
}
.agent-tabs {
  display: flex;
  min-width: 0;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
  scrollbar-width: thin;
}
.agent-tab {
  display: inline-flex;
  min-height: 42px;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  padding: 7px 13px;
  border: 0;
  border-bottom: 2px solid transparent;
  color: var(--muted);
  background: transparent;
  cursor: pointer;
  font-weight: 600;
}
.agent-tab:hover,
.agent-tab:focus-visible {
  color: var(--accent-strong);
  background: var(--accent-soft);
}
.agent-tab[aria-selected="true"] {
  border-bottom-color: var(--accent);
  color: var(--accent-strong);
}
.agent-tab span {
  padding: 2px 5px;
  border-radius: 4px;
  color: var(--warning);
  background: var(--warning-soft);
  font-size: 0.68rem;
  font-weight: 500;
}
.agent-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
}
.agent-profile-layout {
  grid-template-columns: minmax(0, 1fr) minmax(280px, 0.34fr);
  align-items: start;
}
.agent-profile-aside {
  display: grid;
  gap: 16px;
}
.launch-panel,
.agent-summary {
  display: grid;
  gap: 12px;
}
.launch-panel h2,
.agent-summary h2 {
  margin: 0;
  font-size: 1rem;
}
.launch-panel :deep(textarea) {
  min-height: 150px;
}
.agent-summary dl {
  display: grid;
  gap: 0;
  margin: 0;
}
.agent-summary dl div {
  display: grid;
  grid-template-columns: minmax(110px, 0.8fr) minmax(0, 1.2fr);
  gap: 10px;
  padding: 8px 0;
  border-top: 1px solid var(--hairline);
}
.agent-summary dt {
  color: var(--subtle);
}
.agent-summary dd {
  min-width: 0;
  margin: 0;
  overflow-wrap: anywhere;
}
.agent-summary__instruction-state {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.agent-role-unavailable {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--warning);
  background: var(--warning-soft);
}
@media (max-width: 940px) {
  .agent-profile-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .agent-tab {
    min-height: 40px;
    padding-inline: 10px;
  }
}
</style>
