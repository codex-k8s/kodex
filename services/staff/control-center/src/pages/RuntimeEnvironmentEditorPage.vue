<script setup lang="ts">
import {
  Boxes,
  CheckCircle2,
  CircleAlert,
  Cpu,
  KeyRound,
  Network,
  Plus,
  Power,
  PowerOff,
  ServerCog,
  ShieldCheck,
  Trash2,
} from "@lucide/vue";
import { computed, nextTick, onMounted, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute, useRouter } from "vue-router";

import {
  compactIdentifier,
  environmentReadiness,
  hasEnvironmentAction,
  safeSecretReference,
} from "@/features/runtime/environment-capabilities";
import {
  defaultRuntimeEnvironmentPolicy,
  editableRuntimeEnvironmentPolicy,
  editableSecretBindings,
  emptyRuntimeVolume,
  emptySecretBinding,
  mandatoryRuntimeNetworkDestinations,
  normalizeRuntimeEnvironmentInput,
  runtimeEnvironmentCollectionLimit,
  runtimeResourceBounds,
  runtimeVolumeBounds,
  setRuntimeKubernetesAccess,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import {
  consumeRuntimeEnvironmentPolicyDraft,
  createRuntimeEnvironmentPolicyDraft,
  discardRuntimeEnvironmentPolicyDraft,
  requiresRuntimeEnvironmentPolicyReauth,
  storeRuntimeEnvironmentPolicyDraft,
  type RuntimeEnvironmentPolicyDraftOperation,
} from "@/features/runtime/environment-reauth-draft";
import { useRuntimeStore } from "@/features/runtime/store";
import { loadRuntimeSecretPage } from "@/features/runtime-secrets/api";
import {
  maskedSecretHint,
  type RuntimeSecret,
} from "@/features/runtime-secrets/model";
import { consumeRuntimeEnvironmentPolicyReauthCompletion } from "@/features/session/reauth";
import { useSessionStore } from "@/features/session/store";
import type {
  RoleImageArtifact,
  RoleImageArtifactTool,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
  RuntimeKubernetesAccessKind,
  RuntimeSecretBinding,
  RuntimeSecretDescriptor,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type { AsyncEntityOption } from "@/shared/ui/async-entity-picker";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type EditorSection =
  | "GENERAL"
  | "IMAGE_TOOLS"
  | "VALUES"
  | "SECRETS"
  | "POLICY"
  | "READINESS";

const route = useRoute();
const router = useRouter();
const { t } = useI18n();
const runtime = useRuntimeStore();
const session = useSessionStore();
const projectRef = computed(() => String(route.params.projectRef));
const environmentRef = computed(() => {
  const value = route.params.environmentRef;
  return typeof value === "string" ? value : undefined;
});
const current = computed<RuntimeEnvironmentSet | undefined>(() =>
  environmentRef.value ? runtime.environments[environmentRef.value] : undefined,
);
const versions = computed(() =>
  environmentRef.value
    ? (runtime.environmentVersions[environmentRef.value] ?? [])
    : [],
);
const busy = ref(false);
const problem = ref<AppProblem>();
const reauthRestored = ref(false);
const activeSection = ref<EditorSection>("GENERAL");
const editorForm = ref<HTMLFormElement>();
const input = reactive<RuntimeEnvironmentInput>({
  name: "",
  description: "",
  imageArtifactRef: "",
  tools: [],
  values: [],
  secretBindings: [],
  policy: defaultRuntimeEnvironmentPolicy(),
});
const selectedSecrets = reactive<Record<string, AsyncEntityOption>>({});
const selectedImage = ref<AsyncEntityOption>();
const imageArtifact = ref<RoleImageArtifact>();
const imageLoading = ref(false);
const imageProblem = ref<AppProblem>();
const validation = computed(() => validateEnvironmentInput(input));
const serverReadiness = computed(() =>
  environmentRef.value
    ? runtime.environmentReadiness[environmentRef.value]
    : undefined,
);
const boundAgents = computed(() =>
  environmentRef.value
    ? (runtime.environmentAgents[environmentRef.value] ?? [])
    : [],
);
const readiness = computed(() =>
  environmentReadiness(input, current.value, serverReadiness.value),
);
const canPublish = computed(
  () => !current.value || hasEnvironmentAction(current.value, "UPDATE"),
);
const secretPickerLabels = computed(() => ({
  label: t("runtime.chooseRuntimeSecret"),
  searchPlaceholder: t("runtime.searchRuntimeSecret"),
  loading: t("runtime.secretPicker.loading"),
  loadingMore: t("runtime.secretPicker.loadingMore"),
  empty: t("runtime.secretPicker.empty"),
  error: t("runtime.secretPicker.error"),
  retry: t("common.retry"),
}));
const versionDigest = computed(() =>
  current.value
    ? compactIdentifier(current.value.currentVersion.digest)
    : undefined,
);
const publishedPolicy = computed(() => current.value?.currentVersion.policy);
const sections: readonly { id: EditorSection; icon: typeof Boxes }[] = [
  { id: "GENERAL", icon: ServerCog },
  { id: "IMAGE_TOOLS", icon: Boxes },
  { id: "VALUES", icon: Cpu },
  { id: "SECRETS", icon: KeyRound },
  { id: "POLICY", icon: ShieldCheck },
  { id: "READINESS", icon: CheckCircle2 },
];

function sectionTabId(section: EditorSection): string {
  return `environment-tab-${section.toLocaleLowerCase()}`;
}

function sectionPanelId(section: EditorSection): string {
  return `environment-panel-${section.toLocaleLowerCase()}`;
}

function openSection(section: EditorSection): void {
  activeSection.value = section;
}

async function moveSection(event: KeyboardEvent, index: number): Promise<void> {
  const last = sections.length - 1;
  let next = index;
  if (event.key === "ArrowRight") next = index === last ? 0 : index + 1;
  else if (event.key === "ArrowLeft") next = index === 0 ? last : index - 1;
  else if (event.key === "Home") next = 0;
  else if (event.key === "End") next = last;
  else return;
  event.preventDefault();
  const section = sections[next]?.id;
  if (!section) return;
  activeSection.value = section;
  await nextTick();
  document.getElementById(sectionTabId(section))?.focus();
}

function sync(value = current.value): void {
  if (!value) return;
  input.name = value.name;
  input.description = value.description;
  input.imageArtifactRef = value.currentVersion.image.artifactRef;
  input.tools = value.currentVersion.tools.map((item) => ({ ...item }));
  selectedImage.value = {
    ref: value.currentVersion.image.artifactRef,
    title: value.currentVersion.image.reference,
    description: value.currentVersion.image.recipeRef,
    meta: `generation ${String(value.currentVersion.image.recipeGeneration)}`,
  };
  input.values = value.currentVersion.values.map((item) => ({ ...item }));
  input.secretBindings = editableSecretBindings(
    value.currentVersion.secretDescriptors,
  );
  input.policy = editableRuntimeEnvironmentPolicy(value.currentVersion.policy);
}

function applyRestoredInput(value: RuntimeEnvironmentInput): void {
  input.name = value.name;
  input.description = value.description;
  input.imageArtifactRef = value.imageArtifactRef;
  input.tools = value.tools.map((item) => ({ ...item }));
  input.values = value.values.map((item) => ({ ...item }));
  input.secretBindings = value.secretBindings.map((item) => ({ ...item }));
  input.policy = {
    resources: { ...value.policy.resources },
    volumes: value.policy.volumes.map((item) => ({ ...item })),
    networkDestinations: [...value.policy.networkDestinations],
    kubernetesAccess: value.policy.kubernetesAccess,
  };

  if (selectedImage.value?.ref !== value.imageArtifactRef) {
    selectedImage.value = {
      ref: value.imageArtifactRef,
      title: value.imageArtifactRef,
      description: t("runtime.restoredImageSelection"),
    };
    imageArtifact.value = undefined;
  }
  for (const key of Object.keys(selectedSecrets))
    Reflect.deleteProperty(selectedSecrets, key);
  for (const binding of value.secretBindings) {
    if (binding.secretRef && !currentDescriptor(binding)) {
      selectedSecrets[binding.secretRef] = {
        ref: binding.secretRef,
        title: binding.secretRef,
        description: t("runtime.restoredSecretSelection"),
      };
    }
  }
}

async function loadImageArtifact(
  recipeRef: string,
  artifactRef: string,
): Promise<void> {
  imageLoading.value = true;
  imageProblem.value = undefined;
  try {
    imageArtifact.value = await runtime.loadPromotedRoleImageArtifact(
      projectRef.value,
      recipeRef,
      artifactRef,
    );
  } catch (error) {
    imageArtifact.value = undefined;
    imageProblem.value = asProblem(error);
  } finally {
    imageLoading.value = false;
  }
}

function loadImagePage(query: string, cursor?: string) {
  return runtime.searchPromotedRoleImagePage(projectRef.value, query, cursor);
}

async function loadSecretPage(query: string, cursor?: string) {
  const page = await loadRuntimeSecretPage(projectRef.value, query, cursor);
  return {
    items: page.items.map(runtimeSecretOption),
    nextPageToken: page.nextPageToken || undefined,
  };
}

function runtimeSecretOption(secret: RuntimeSecret): AsyncEntityOption {
  return {
    ref: secret.ref,
    title: secret.name,
    description: secret.description,
    meta: `${maskedSecretHint(secret)} · rev ${String(secret.currentRevision)}`,
    disabled: secret.state !== "ACTIVE",
    disabledReason:
      secret.state === "ACTIVE" ? undefined : t("runtime.secretRevoked"),
  };
}

function currentDescriptor(
  binding: RuntimeSecretBinding,
): RuntimeSecretDescriptor | undefined {
  return current.value?.currentVersion.secretDescriptors.find(
    (descriptor) =>
      descriptor.name === binding.name &&
      descriptor.secretRef === binding.secretRef,
  );
}

function safeCurrentDescriptor(binding: RuntimeSecretBinding) {
  const descriptor = currentDescriptor(binding);
  return descriptor ? safeSecretReference(descriptor) : undefined;
}

function selectedSecret(
  binding: RuntimeSecretBinding,
): AsyncEntityOption | undefined {
  if (!binding.secretRef) return undefined;
  const selected = selectedSecrets[binding.secretRef];
  if (selected) return selected;
  const descriptor = currentDescriptor(binding);
  if (!descriptor) return undefined;
  return {
    ref: descriptor.secretRef,
    title:
      [descriptor.secretName, descriptor.secretKey]
        .filter(Boolean)
        .join(" / ") || binding.name,
    description: t("runtime.currentPublishedSecret"),
    meta: `rev ${descriptor.secretResourceVersion}`,
  };
}

function selectSecret(option: AsyncEntityOption): void {
  selectedSecrets[option.ref] = option;
}

async function selectImage(option: AsyncEntityOption): Promise<void> {
  if (!("recipeRef" in option) || !("artifactRef" in option)) return;
  selectedImage.value = option;
  input.imageArtifactRef = String(option.artifactRef);
  input.tools = [];
  await loadImageArtifact(String(option.recipeRef), String(option.artifactRef));
}

function isToolSelected(tool: RoleImageArtifactTool): boolean {
  return input.tools.some((item) => item.command === tool.name);
}

function toggleTool(tool: RoleImageArtifactTool): void {
  const index = input.tools.findIndex((item) => item.command === tool.name);
  if (index >= 0) {
    input.tools.splice(index, 1);
    return;
  }
  input.tools.push({
    name: tool.name,
    command: tool.name,
    description: "",
    usageHint: "",
  });
}

function updateSelectedTool(
  command: string,
  field: "name" | "description" | "usageHint",
  event: Event,
): void {
  const target = event.currentTarget;
  if (
    !(
      target instanceof HTMLInputElement ||
      target instanceof HTMLTextAreaElement
    )
  )
    return;
  const tool = input.tools.find((item) => item.command === command);
  if (tool) tool[field] = target.value;
}

async function addValue(): Promise<void> {
  if (input.values.length >= runtimeEnvironmentCollectionLimit) return;
  openSection("VALUES");
  input.values.push({ name: "", value: "" });
  await nextTick();
  const names = editorForm.value?.querySelectorAll<HTMLInputElement>(
    "[data-environment-variable-name]",
  );
  const target = names?.item(names.length - 1);
  target?.focus();
}

async function addSecret(): Promise<void> {
  if (input.secretBindings.length >= runtimeEnvironmentCollectionLimit) return;
  openSection("SECRETS");
  input.secretBindings.push(emptySecretBinding());
  await nextTick();
  const names = editorForm.value?.querySelectorAll<HTMLInputElement>(
    "[data-environment-secret-name]",
  );
  const target = names?.item(names.length - 1);
  target?.focus();
}

function addVolume(): void {
  if (input.policy.volumes.length < runtimeVolumeBounds.maxItems)
    input.policy.volumes.push(emptyRuntimeVolume());
}

function toggleKubernetesAccess(event: Event): void {
  const target = event.currentTarget;
  if (!(target instanceof HTMLInputElement)) return;
  const access: RuntimeKubernetesAccessKind = target.checked
    ? "READ_OWN_EXECUTION"
    : "NONE";
  setRuntimeKubernetesAccess(input.policy, access);
}

function volumeMountPath(name: string): string {
  return name ? `/workspace/.kodex/volumes/${name}` : "—";
}

async function load(): Promise<void> {
  if (!environmentRef.value) return;
  await Promise.all([
    runtime.loadEnvironment(environmentRef.value),
    runtime.loadEnvironmentVersions(environmentRef.value),
  ]);
  sync();
  if (current.value)
    await Promise.all([
      loadImageArtifact(
        current.value.currentVersion.image.recipeRef,
        current.value.currentVersion.image.artifactRef,
      ),
      runtime.loadEnvironmentReadiness(current.value.ref),
      runtime.loadEnvironmentAgents(current.value.ref),
    ]);
}

function currentOperation(): RuntimeEnvironmentPolicyDraftOperation {
  return current.value ? "PUBLISH" : "CREATE";
}

function restoreAfterFreshAuthentication(): void {
  const operation = currentOperation();
  const expected = {
    ...(environmentRef.value ? { environmentRef: environmentRef.value } : {}),
    ...(current.value ? { expectedVersion: current.value.version } : {}),
    operation,
    projectRef: projectRef.value,
  };
  const completed = consumeRuntimeEnvironmentPolicyReauthCompletion(
    window.sessionStorage,
    expected,
  );
  if (!completed) return;
  const restored = consumeRuntimeEnvironmentPolicyDraft(
    window.sessionStorage,
    expected,
  );
  applyRestoredInput(restored);
  reauthRestored.value = true;
}

async function initialize(): Promise<void> {
  await load();
  try {
    restoreAfterFreshAuthentication();
  } catch (error) {
    problem.value = asProblem(error);
  }
}

async function save(): Promise<void> {
  if (validation.value.length) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const payload = normalizeRuntimeEnvironmentInput(input);
    const saved = current.value
      ? await runtime.publishEnvironment(current.value, payload)
      : await runtime.createEnvironment(projectRef.value, payload);
    reauthRestored.value = false;
    if (!environmentRef.value) {
      await router.replace(
        `/projects/${encodeURIComponent(projectRef.value)}/environments/${encodeURIComponent(saved.ref)}`,
      );
    } else {
      await runtime.loadEnvironmentVersions(saved.ref);
      sync(saved);
    }
  } catch (error) {
    const normalized = asProblem(error);
    if (!requiresRuntimeEnvironmentPolicyReauth(normalized)) {
      problem.value = normalized;
      return;
    }
    const operation = currentOperation();
    try {
      const draft = createRuntimeEnvironmentPolicyDraft({
        ...(environmentRef.value
          ? { environmentRef: environmentRef.value }
          : {}),
        ...(current.value ? { expectedVersion: current.value.version } : {}),
        form: input,
        operation,
        projectRef: projectRef.value,
      });
      storeRuntimeEnvironmentPolicyDraft(draft, window.sessionStorage);
      await session.beginRuntimeEnvironmentPolicyReauth({
        ...(environmentRef.value
          ? { environmentRef: environmentRef.value }
          : {}),
        operation,
        projectRef: projectRef.value,
      });
    } catch (reauthError) {
      discardRuntimeEnvironmentPolicyDraft(window.sessionStorage);
      problem.value = asProblem(reauthError);
    }
  } finally {
    busy.value = false;
  }
}

async function rollback(versionRef: string): Promise<void> {
  if (!current.value || !hasEnvironmentAction(current.value, "ROLLBACK"))
    return;
  busy.value = true;
  problem.value = undefined;
  try {
    const saved = await runtime.restoreEnvironment(current.value, versionRef);
    await runtime.loadEnvironmentVersions(saved.ref);
    sync(saved);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function setEnabled(enabled: boolean): Promise<void> {
  if (!current.value) return;
  const action = enabled ? "ENABLE" : "DISABLE";
  if (!hasEnvironmentAction(current.value, action)) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const saved = await runtime.setEnvironmentEnabled(current.value, enabled);
    sync(saved);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function remove(): Promise<void> {
  if (!current.value || !hasEnvironmentAction(current.value, "DELETE")) return;
  if (!window.confirm(`${t("common.delete")} «${current.value.name}»?`)) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await runtime.removeEnvironment(current.value);
    await router.replace(
      `/projects/${encodeURIComponent(projectRef.value)}/environments`,
    );
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

function onVersionScroll(event: Event): void {
  if (!environmentRef.value) return;
  const element = event.currentTarget as HTMLElement;
  const hasMore = Boolean(
    runtime.environmentVersionCursors[environmentRef.value],
  );
  const loading = Boolean(
    runtime.loading[`environment-versions:${environmentRef.value}`],
  );
  if (
    hasMore &&
    !loading &&
    element.scrollTop + element.clientHeight >= element.scrollHeight - 64
  )
    void runtime.loadEnvironmentVersions(environmentRef.value, false);
}

watch(environmentRef, () => void initialize());
onMounted(() => void initialize());
</script>

<template>
  <PageFrame
    :title="current?.name ?? $t('runtime.newEnvironment')"
    :subtitle="$t('runtime.environmentEditorSubtitle')"
  >
    <template #actions>
      <button
        v-if="current && hasEnvironmentAction(current, 'DISABLE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="setEnabled(false)"
      >
        <PowerOff :size="16" aria-hidden="true" />
        {{ $t("common.disable") }}
      </button>
      <button
        v-if="current && hasEnvironmentAction(current, 'ENABLE')"
        class="button"
        type="button"
        :disabled="busy"
        @click="setEnabled(true)"
      >
        <Power :size="16" aria-hidden="true" />
        {{ $t("common.enable") }}
      </button>
      <button
        v-if="current && hasEnvironmentAction(current, 'DELETE')"
        class="button button--danger"
        type="button"
        :disabled="busy"
        @click="remove"
      >
        <Trash2 :size="16" aria-hidden="true" />
        {{ $t("common.delete") }}
      </button>
      <RouterLink
        class="button"
        :to="`/projects/${encodeURIComponent(projectRef)}/environments`"
      >
        {{ $t("common.cancel") }}
      </RouterLink>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy || validation.length > 0 || !canPublish"
        @click="save"
      >
        {{ current ? $t("runtime.publishRevision") : $t("common.create") }}
      </button>
    </template>

    <aside v-if="reauthRestored" class="reauth-restored" role="status">
      <ShieldCheck :size="18" aria-hidden="true" />
      <div>
        <strong>{{ $t("runtime.reauthCompleted") }}</strong>
        <p>{{ $t("runtime.reauthExplicitSaveRequired") }}</p>
      </div>
    </aside>

    <AsyncState
      :loading="
        environmentRef
          ? runtime.loading[`environment:${environmentRef}`]
          : false
      "
      :problem="
        environmentRef
          ? runtime.problems[`environment:${environmentRef}`]
          : undefined
      "
      @retry="load"
    >
      <nav
        class="environment-tabs"
        role="tablist"
        :aria-label="$t('runtime.editorSections')"
      >
        <button
          v-for="(section, index) in sections"
          :id="sectionTabId(section.id)"
          :key="section.id"
          class="environment-tab"
          :class="{ 'environment-tab--active': activeSection === section.id }"
          type="button"
          role="tab"
          :aria-selected="activeSection === section.id"
          :aria-controls="sectionPanelId(section.id)"
          :tabindex="activeSection === section.id ? 0 : -1"
          @click="openSection(section.id)"
          @keydown="moveSection($event, index)"
        >
          <component :is="section.icon" :size="16" aria-hidden="true" />
          {{ $t(`runtime.section.${section.id}`) }}
        </button>
      </nav>

      <div
        class="environment-command-bar"
        role="toolbar"
        :aria-label="$t('runtime.editorActions')"
      >
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="openSection('IMAGE_TOOLS')"
        >
          <Boxes :size="15" aria-hidden="true" />
          {{ $t("runtime.section.IMAGE_TOOLS") }}
        </button>
        <button
          class="button"
          type="button"
          :disabled="
            busy || input.values.length >= runtimeEnvironmentCollectionLimit
          "
          @click="addValue"
        >
          <Plus :size="15" aria-hidden="true" />
          {{ $t("runtime.addVariable") }}
        </button>
        <button
          class="button"
          type="button"
          :disabled="
            busy ||
            input.secretBindings.length >= runtimeEnvironmentCollectionLimit
          "
          @click="addSecret"
        >
          <KeyRound :size="15" aria-hidden="true" />
          {{ $t("runtime.addSecretBinding") }}
        </button>
        <button
          class="button"
          type="button"
          :disabled="busy"
          @click="openSection('POLICY')"
        >
          <ShieldCheck :size="15" aria-hidden="true" />
          {{ $t("runtime.section.POLICY") }}
        </button>
      </div>

      <div class="environment-editor-layout">
        <form
          ref="editorForm"
          class="panel environment-editor"
          @submit.prevent="save"
        >
          <section
            v-if="activeSection === 'GENERAL'"
            :id="sectionPanelId('GENERAL')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('GENERAL')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.environmentGeneral") }}</h2>
                <p>{{ $t("runtime.environmentGeneralHelp") }}</p>
              </div>
              <StatusBadge v-if="current" :state="current.state" />
            </div>
            <label class="field">
              <span>{{ $t("common.name") }}</span>
              <input v-model="input.name" required maxlength="120" />
            </label>
            <label class="field">
              <span>{{ $t("common.description") }}</span>
              <textarea v-model="input.description" maxlength="1000" />
            </label>
            <div class="safe-summary">
              <div>
                <span>{{ $t("runtime.revision") }}</span>
                <strong>
                  {{
                    current
                      ? `rev ${String(current.currentVersion.revision)}`
                      : $t("runtime.notPublished")
                  }}
                </strong>
              </div>
              <div>
                <span>{{ $t("runtime.versionDigest") }}</span>
                <code>{{ versionDigest ?? "—" }}</code>
              </div>
              <div>
                <span>{{ $t("runtime.updatedAt") }}</span>
                <strong>
                  {{
                    current ? new Date(current.updatedAt).toLocaleString() : "—"
                  }}
                </strong>
              </div>
            </div>
          </section>

          <section
            v-else-if="activeSection === 'IMAGE_TOOLS'"
            :id="sectionPanelId('IMAGE_TOOLS')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('IMAGE_TOOLS')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.imageAndTools") }}</h2>
                <p>{{ $t("runtime.imageAndToolsHelp") }}</p>
              </div>
            </div>
            <label class="field">
              <span>{{ $t("runtime.exactImage") }}</span>
              <AsyncEntityPicker
                v-model="input.imageArtifactRef"
                :selected="selectedImage"
                :load-page="loadImagePage"
                :placeholder="$t('runtime.choosePromotedImage')"
                :search-placeholder="$t('runtime.searchPromotedImage')"
                @select="selectImage"
              />
            </label>
            <ProblemNotice v-if="imageProblem" :problem="imageProblem" />
            <article
              v-if="selectedImage"
              class="selected-image"
              :aria-busy="imageLoading"
            >
              <Boxes :size="22" aria-hidden="true" />
              <div>
                <strong>{{ selectedImage.title }}</strong>
                <p>{{ selectedImage.description }}</p>
                <code>{{ input.imageArtifactRef }}</code>
              </div>
              <StatusBadge
                :state="imageArtifact ? 'ACCEPTED' : 'PENDING'"
                :label="
                  imageArtifact
                    ? $t('runtime.promotedAndVerified')
                    : $t('common.loading')
                "
              />
            </article>

            <div class="section-header tool-heading">
              <div>
                <h3>{{ $t("runtime.verifiedTools") }}</h3>
                <p>{{ $t("runtime.verifiedToolsHelp") }}</p>
              </div>
              <span>
                {{
                  $t("runtime.selectedToolsCount", {
                    selected: input.tools.length,
                    total: imageArtifact?.tools.length ?? 0,
                  })
                }}
              </span>
            </div>
            <div v-if="imageLoading" class="secondary-text" role="status">
              {{ $t("common.loading") }}
            </div>
            <div v-else-if="imageArtifact?.tools.length" class="tool-catalog">
              <article
                v-for="tool in imageArtifact.tools"
                :key="tool.name"
                class="tool-option"
              >
                <label>
                  <input
                    type="checkbox"
                    :checked="isToolSelected(tool)"
                    @change="toggleTool(tool)"
                  />
                  <span>
                    <strong
                      ><code>{{ tool.name }}</code></strong
                    >
                    <small>{{ tool.version }}</small>
                  </span>
                </label>
                <div v-if="isToolSelected(tool)" class="tool-fields">
                  <label class="field">
                    <span>{{ $t("runtime.toolDisplayName") }}</span>
                    <input
                      :value="
                        input.tools.find((item) => item.command === tool.name)
                          ?.name
                      "
                      maxlength="160"
                      @input="updateSelectedTool(tool.name, 'name', $event)"
                    />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.toolCommand") }}</span>
                    <input :value="tool.name" readonly />
                  </label>
                  <label class="field field--wide">
                    <span>{{ $t("common.description") }}</span>
                    <textarea
                      :value="
                        input.tools.find((item) => item.command === tool.name)
                          ?.description
                      "
                      maxlength="500"
                      required
                      @input="
                        updateSelectedTool(tool.name, 'description', $event)
                      "
                    />
                  </label>
                  <label class="field field--wide">
                    <span>{{ $t("runtime.toolUsageHint") }}</span>
                    <textarea
                      :value="
                        input.tools.find((item) => item.command === tool.name)
                          ?.usageHint
                      "
                      maxlength="500"
                      @input="
                        updateSelectedTool(tool.name, 'usageHint', $event)
                      "
                    />
                  </label>
                </div>
              </article>
            </div>
            <p v-else class="secondary-text">
              {{
                input.imageArtifactRef
                  ? $t("runtime.noVerifiedTools")
                  : $t("runtime.chooseImageFirst")
              }}
            </p>
          </section>

          <section
            v-else-if="activeSection === 'VALUES'"
            :id="sectionPanelId('VALUES')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('VALUES')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.variables") }}</h2>
                <p>{{ $t("runtime.variablesHelp") }}</p>
              </div>
            </div>
            <div v-if="input.values.length" class="environment-fields">
              <div
                v-for="(item, index) in input.values"
                :key="index"
                class="environment-field-row"
              >
                <label class="field">
                  <span>{{ $t("runtime.variableName") }}</span>
                  <input
                    v-model="item.name"
                    data-environment-variable-name
                    placeholder="VAR_NAME"
                  />
                </label>
                <label class="field">
                  <span>{{ $t("runtime.nonSecretValue") }}</span>
                  <input v-model="item.value" maxlength="8192" />
                </label>
                <button
                  class="icon-button icon-button--danger"
                  type="button"
                  :aria-label="$t('common.delete')"
                  @click="input.values.splice(index, 1)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
            </div>
            <p v-else class="secondary-text">{{ $t("common.empty") }}</p>
          </section>

          <section
            v-else-if="activeSection === 'SECRETS'"
            :id="sectionPanelId('SECRETS')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('SECRETS')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.secretReferences") }}</h2>
                <p>{{ $t("runtime.secretBindingsHelp") }}</p>
              </div>
            </div>
            <div class="secret-warning" role="note">
              <ShieldCheck :size="18" aria-hidden="true" />
              {{ $t("runtime.secretValuesForbidden") }}
            </div>
            <article
              v-for="(item, index) in input.secretBindings"
              :key="index"
              class="secret-descriptor"
            >
              <div class="section-header">
                <div>
                  <strong>
                    {{
                      item.name ||
                      $t("runtime.secretBinding", { number: index + 1 })
                    }}
                  </strong>
                  <p>
                    {{
                      selectedSecret(item)?.title ||
                      $t("runtime.secretNotSelected")
                    }}
                  </p>
                </div>
                <button
                  class="icon-button icon-button--danger"
                  type="button"
                  :aria-label="$t('common.delete')"
                  @click="input.secretBindings.splice(index, 1)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
              <div class="secret-binding-fields">
                <label class="field">
                  <span>{{ $t("runtime.variableName") }}</span>
                  <input
                    v-model="item.name"
                    data-environment-secret-name
                    placeholder="SECRET_NAME"
                  />
                </label>
                <div class="field">
                  <span>{{ $t("runtime.runtimeSecret") }}</span>
                  <AsyncEntityPicker
                    v-model="item.secretRef"
                    :selected="selectedSecret(item)"
                    :load-page="loadSecretPage"
                    :labels="secretPickerLabels"
                    :placeholder="$t('runtime.chooseRuntimeSecret')"
                    :search-placeholder="$t('runtime.searchRuntimeSecret')"
                    @select="selectSecret"
                  />
                </div>
              </div>
              <dl
                v-if="currentDescriptor(item)"
                class="secret-safe-meta"
                :aria-label="$t('runtime.currentImmutableDescriptor')"
              >
                <div>
                  <dt>{{ $t("runtime.secretTarget") }}</dt>
                  <dd>{{ safeCurrentDescriptor(item)?.target }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.secretResourceVersion") }}</dt>
                  <dd>{{ safeCurrentDescriptor(item)?.revision }}</dd>
                </div>
                <div>
                  <dt>UID</dt>
                  <dd>
                    <code>{{ safeCurrentDescriptor(item)?.uidHint }}</code>
                  </dd>
                </div>
                <div>
                  <dt>SHA-256</dt>
                  <dd>
                    <code>{{ safeCurrentDescriptor(item)?.digestHint }}</code>
                  </dd>
                </div>
              </dl>
              <p v-else class="secondary-text">
                {{ $t("runtime.descriptorGeneratedOnPublish") }}
              </p>
            </article>
            <p v-if="!input.secretBindings.length" class="secondary-text">
              {{ $t("runtime.noSecretReferences") }}
            </p>
          </section>

          <section
            v-else-if="activeSection === 'POLICY'"
            :id="sectionPanelId('POLICY')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('POLICY')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.resourcesAndAccess") }}</h2>
                <p>{{ $t("runtime.resourcesAndAccessHelp") }}</p>
              </div>
              <StatusBadge state="AVAILABLE" :label="$t('common.available')" />
            </div>

            <section class="policy-group">
              <div class="section-header">
                <div>
                  <h3>{{ $t("runtime.resources") }}</h3>
                  <p>{{ $t("runtime.resourcesHelp") }}</p>
                </div>
                <Cpu :size="20" aria-hidden="true" />
              </div>
              <div class="resource-grid">
                <label class="field">
                  <span>{{ $t("runtime.cpuRequest") }}</span>
                  <input
                    v-model.number="input.policy.resources.cpuRequestMilli"
                    type="number"
                    :min="runtimeResourceBounds.cpuRequestMilli.min"
                    :max="runtimeResourceBounds.cpuRequestMilli.max"
                    step="100"
                  />
                  <small>{{ $t("runtime.cpuRequestRange") }}</small>
                </label>
                <label class="field">
                  <span>{{ $t("runtime.cpuLimit") }}</span>
                  <input
                    v-model.number="input.policy.resources.cpuLimitMilli"
                    type="number"
                    :min="runtimeResourceBounds.cpuLimitMilli.min"
                    :max="runtimeResourceBounds.cpuLimitMilli.max"
                    step="100"
                  />
                  <small>{{ $t("runtime.cpuLimitRange") }}</small>
                </label>
                <label class="field">
                  <span>{{ $t("runtime.memoryRequest") }}</span>
                  <input
                    v-model.number="input.policy.resources.memoryRequestMib"
                    type="number"
                    :min="runtimeResourceBounds.memoryRequestMib.min"
                    :max="runtimeResourceBounds.memoryRequestMib.max"
                    step="128"
                  />
                  <small>{{ $t("runtime.memoryRequestRange") }}</small>
                </label>
                <label class="field">
                  <span>{{ $t("runtime.memoryLimit") }}</span>
                  <input
                    v-model.number="input.policy.resources.memoryLimitMib"
                    type="number"
                    :min="runtimeResourceBounds.memoryLimitMib.min"
                    :max="runtimeResourceBounds.memoryLimitMib.max"
                    step="128"
                  />
                  <small>{{ $t("runtime.memoryLimitRange") }}</small>
                </label>
                <label class="field">
                  <span>{{ $t("runtime.ephemeralStorageRequest") }}</span>
                  <input
                    v-model.number="
                      input.policy.resources.ephemeralStorageRequestMib
                    "
                    type="number"
                    :min="runtimeResourceBounds.ephemeralStorageRequestMib.min"
                    :max="runtimeResourceBounds.ephemeralStorageRequestMib.max"
                    step="256"
                  />
                  <small>{{
                    $t("runtime.ephemeralStorageRequestRange")
                  }}</small>
                </label>
                <label class="field">
                  <span>{{ $t("runtime.ephemeralStorageLimit") }}</span>
                  <input
                    v-model.number="
                      input.policy.resources.ephemeralStorageLimitMib
                    "
                    type="number"
                    :min="runtimeResourceBounds.ephemeralStorageLimitMib.min"
                    :max="runtimeResourceBounds.ephemeralStorageLimitMib.max"
                    step="256"
                  />
                  <small>{{ $t("runtime.ephemeralStorageLimitRange") }}</small>
                </label>
              </div>
            </section>

            <section class="policy-group">
              <div class="section-header">
                <div>
                  <h3>{{ $t("runtime.ephemeralVolumes") }}</h3>
                  <p>{{ $t("runtime.ephemeralVolumesHelp") }}</p>
                </div>
                <button
                  class="button"
                  type="button"
                  :disabled="
                    input.policy.volumes.length >= runtimeVolumeBounds.maxItems
                  "
                  @click="addVolume"
                >
                  <Plus :size="15" aria-hidden="true" />
                  {{ $t("runtime.addVolume") }}
                </button>
              </div>
              <div v-if="input.policy.volumes.length" class="volume-list">
                <article
                  v-for="(volume, index) in input.policy.volumes"
                  :key="index"
                  class="volume-row"
                >
                  <label class="field">
                    <span>{{ $t("common.name") }}</span>
                    <input
                      v-model="volume.name"
                      placeholder="workspace-cache"
                    />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.volumeKind") }}</span>
                    <select v-model="volume.kind">
                      <option value="EPHEMERAL_DISK">
                        {{ $t("runtime.volumeKindLabel.EPHEMERAL_DISK") }}
                      </option>
                      <option value="EPHEMERAL_MEMORY">
                        {{ $t("runtime.volumeKindLabel.EPHEMERAL_MEMORY") }}
                      </option>
                    </select>
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.volumeSize") }}</span>
                    <input
                      v-model.number="volume.sizeMib"
                      type="number"
                      :min="runtimeVolumeBounds.minSizeMib"
                      :max="runtimeVolumeBounds.maxSizeMib"
                      step="16"
                    />
                  </label>
                  <div class="volume-mount">
                    <span>{{ $t("runtime.mountPath") }}</span>
                    <code>{{ volumeMountPath(volume.name) }}</code>
                  </div>
                  <button
                    class="icon-button icon-button--danger"
                    type="button"
                    :aria-label="$t('common.delete')"
                    @click="input.policy.volumes.splice(index, 1)"
                  >
                    <Trash2 :size="16" aria-hidden="true" />
                  </button>
                </article>
              </div>
              <p v-else class="secondary-text">
                {{ $t("runtime.noEphemeralVolumes") }}
              </p>
            </section>

            <section class="policy-group">
              <div class="section-header">
                <div>
                  <h3>{{ $t("runtime.networkPolicy") }}</h3>
                  <p>{{ $t("runtime.networkPolicyHelp") }}</p>
                </div>
                <Network :size="20" aria-hidden="true" />
              </div>
              <div class="destination-list">
                <article
                  v-for="destination in mandatoryRuntimeNetworkDestinations"
                  :key="destination"
                  class="destination-row"
                >
                  <div>
                    <strong>{{
                      $t(`runtime.networkDestination.${destination}`)
                    }}</strong>
                    <p>
                      {{ $t(`runtime.networkDestinationHelp.${destination}`) }}
                    </p>
                  </div>
                  <StatusBadge
                    state="REQUIRED"
                    :label="$t('runtime.mandatoryDestination')"
                  />
                </article>
                <article class="destination-row">
                  <div>
                    <strong>{{
                      $t("runtime.networkDestination.KUBERNETES_API")
                    }}</strong>
                    <p>
                      {{ $t("runtime.networkDestinationHelp.KUBERNETES_API") }}
                    </p>
                  </div>
                  <StatusBadge
                    :state="
                      input.policy.kubernetesAccess === 'READ_OWN_EXECUTION'
                        ? 'AVAILABLE'
                        : 'DISABLED'
                    "
                    :label="
                      input.policy.kubernetesAccess === 'READ_OWN_EXECUTION'
                        ? $t('runtime.scopedAccessEnabled')
                        : $t('common.disabled')
                    "
                  />
                </article>
              </div>
            </section>

            <section class="policy-group">
              <div class="section-header">
                <div>
                  <h3>{{ $t("runtime.kubernetesRbac") }}</h3>
                  <p>{{ $t("runtime.kubernetesRbacHelp") }}</p>
                </div>
                <ShieldCheck :size="20" aria-hidden="true" />
              </div>
              <label class="access-toggle">
                <input
                  type="checkbox"
                  :checked="
                    input.policy.kubernetesAccess === 'READ_OWN_EXECUTION'
                  "
                  @change="toggleKubernetesAccess"
                />
                <span>
                  <strong>{{ $t("runtime.readOwnExecution") }}</strong>
                  <small>{{ $t("runtime.readOwnExecutionHelp") }}</small>
                </span>
              </label>
              <p class="boundary-note" role="note">
                <CircleAlert :size="17" aria-hidden="true" />
                {{ $t("runtime.kubernetesAccessBoundary") }}
              </p>
            </section>

            <div class="effective-preview">
              <div class="section-header">
                <div>
                  <h3>{{ $t("runtime.effectivePolicyPreview") }}</h3>
                  <p>{{ $t("runtime.effectivePolicyPreviewHelp") }}</p>
                </div>
                <StatusBadge
                  :state="publishedPolicy ? 'PUBLISHED' : 'DRAFT'"
                  :label="
                    publishedPolicy
                      ? $t('runtime.serverCalculated')
                      : $t('runtime.afterPublish')
                  "
                />
              </div>
              <template v-if="publishedPolicy">
                <dl class="policy-summary">
                  <div>
                    <dt>{{ $t("runtime.denyByDefault") }}</dt>
                    <dd>{{ $t("common.yes") }}</dd>
                  </div>
                  <div>
                    <dt>{{ $t("runtime.kubernetesNamespace") }}</dt>
                    <dd><code>kodex-runtime</code></dd>
                  </div>
                  <div>
                    <dt>{{ $t("runtime.effectiveEgressRules") }}</dt>
                    <dd>{{ publishedPolicy.network.egress.length }}</dd>
                  </div>
                  <div>
                    <dt>{{ $t("runtime.effectiveVolumes") }}</dt>
                    <dd>{{ publishedPolicy.volumes.length }}</dd>
                  </div>
                </dl>
                <div class="digest-grid">
                  <div
                    v-for="(digest, key) in {
                      resources: publishedPolicy.resourcesDigest,
                      volumes: publishedPolicy.volumesDigest,
                      network: publishedPolicy.networkDigest,
                      rbac: publishedPolicy.rbacDigest,
                    }"
                    :key="key"
                  >
                    <span>{{ $t(`runtime.policyDigest.${key}`) }}</span>
                    <code>{{ compactIdentifier(digest) }}</code>
                  </div>
                </div>
              </template>
              <p v-else class="secondary-text">
                {{ $t("runtime.effectivePolicyAfterPublish") }}
              </p>
            </div>
          </section>

          <section
            v-else
            :id="sectionPanelId('READINESS')"
            class="editor-section"
            role="tabpanel"
            :aria-labelledby="sectionTabId('READINESS')"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.readiness") }}</h2>
                <p>{{ $t("runtime.readinessHelp") }}</p>
              </div>
            </div>
            <div class="readiness-list">
              <article
                v-for="check in readiness"
                :key="check.key"
                class="readiness-check"
              >
                <CheckCircle2
                  v-if="check.state === 'READY'"
                  :size="19"
                  class="readiness-icon readiness-icon--ready"
                  aria-hidden="true"
                />
                <CircleAlert
                  v-else
                  :size="19"
                  class="readiness-icon"
                  aria-hidden="true"
                />
                <div>
                  <strong>{{
                    $t(`runtime.readinessCheck.${check.key}`)
                  }}</strong>
                  <p>{{ $t(`runtime.readinessState.${check.state}`) }}</p>
                </div>
                <StatusBadge
                  :state="
                    check.state === 'READY'
                      ? 'READY'
                      : check.state === 'UNAVAILABLE'
                        ? 'UNAVAILABLE'
                        : 'NEEDS_ATTENTION'
                  "
                  :label="$t(`runtime.readinessState.${check.state}`)"
                />
              </article>
            </div>
            <section v-if="current" class="effective-preview">
              <h3>{{ $t("agents.title") }} · {{ boundAgents.length }}</h3>
              <div v-if="boundAgents.length" class="chip-list">
                <span v-for="agent in boundAgents" :key="agent.ref">
                  {{ agent.name }}
                </span>
              </div>
              <p v-else class="secondary-text">{{ $t("common.empty") }}</p>
              <button
                v-if="runtime.environmentAgentCursors[current.ref]"
                class="button"
                type="button"
                :disabled="runtime.loading[`environment-agents:${current.ref}`]"
                @click="runtime.loadEnvironmentAgents(current.ref, false)"
              >
                {{ $t("roleImages.loadMore") }}
              </button>
            </section>
            <section class="effective-preview">
              <h3>{{ $t("runtime.safeEffectivePreview") }}</h3>
              <p>{{ $t("runtime.safeEffectivePreviewHelp") }}</p>
              <dl>
                <div>
                  <dt>{{ $t("runtime.revision") }}</dt>
                  <dd>
                    {{
                      current
                        ? `rev ${String(current.currentVersion.revision)}`
                        : $t("runtime.notPublished")
                    }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.versionDigest") }}</dt>
                  <dd>
                    <code>{{ versionDigest ?? "—" }}</code>
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.variables") }}</dt>
                  <dd>{{ input.values.length }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.secretReferences") }}</dt>
                  <dd>{{ input.secretBindings.length }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.exactImage") }}</dt>
                  <dd>
                    <code>{{ input.imageArtifactRef || "—" }}</code>
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.verifiedTools") }}</dt>
                  <dd>{{ input.tools.length }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.resources") }}</dt>
                  <dd>
                    {{ input.policy.resources.cpuRequestMilli }}/{{
                      input.policy.resources.cpuLimitMilli
                    }}m CPU · {{ input.policy.resources.memoryRequestMib }}/{{
                      input.policy.resources.memoryLimitMib
                    }}
                    MiB
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.ephemeralVolumes") }}</dt>
                  <dd>{{ input.policy.volumes.length }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.networkPolicy") }}</dt>
                  <dd>
                    {{ $t("runtime.denyByDefault") }} ·
                    {{ input.policy.networkDestinations.length }}
                  </dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.kubernetesRbac") }}</dt>
                  <dd>{{ input.policy.kubernetesAccess }}</dd>
                </div>
              </dl>
              <p v-if="!publishedPolicy" class="boundary-note" role="note">
                <CircleAlert :size="17" aria-hidden="true" />
                {{ $t("runtime.effectivePolicyAfterPublish") }}
              </p>
            </section>
          </section>

          <ul v-if="validation.length" class="validation-list" role="alert">
            <li
              v-for="item in validation"
              :key="`${item.field}:${item.message}`"
            >
              {{ $t(item.message) }}
            </li>
          </ul>
          <ProblemNotice v-if="problem" :problem="problem" />
          <section v-if="problem?.kind === 'conflict'" class="conflict-panel">
            <p>{{ $t("runtime.environmentConflict") }}</p>
            <button class="button" type="button" @click="load">
              {{ $t("runtime.reload") }}
            </button>
          </section>
        </form>

        <aside class="panel revision-panel">
          <div class="section-header">
            <div>
              <h2>{{ $t("runtime.revisionHistory") }}</h2>
              <p>{{ $t("runtime.revisionHistoryHelp") }}</p>
            </div>
            <span v-if="current"
              >rev {{ current.currentVersion.revision }}</span
            >
          </div>
          <div
            v-if="versions.length"
            class="revision-scroll"
            :aria-busy="
              environmentRef
                ? runtime.loading[`environment-versions:${environmentRef}`]
                : false
            "
            @scroll="onVersionScroll"
          >
            <article v-for="version in versions" :key="version.ref">
              <div>
                <strong>rev {{ version.revision }}</strong>
                <small>{{
                  new Date(version.createdAt).toLocaleString()
                }}</small>
                <code>{{ compactIdentifier(version.digest) }}</code>
              </div>
              <button
                v-if="
                  version.ref !== current?.currentVersion.ref &&
                  current &&
                  hasEnvironmentAction(current, 'ROLLBACK')
                "
                class="button"
                type="button"
                :disabled="busy"
                @click="rollback(version.ref)"
              >
                {{ $t("runtime.rollback") }}
              </button>
              <StatusBadge v-else state="ACTIVE" />
            </article>
            <p
              v-if="
                environmentRef &&
                runtime.loading[`environment-versions:${environmentRef}`]
              "
              class="secondary-text revision-loading"
              role="status"
            >
              {{ $t("common.loading") }}
            </p>
          </div>
          <p v-else class="secondary-text">
            {{ $t("runtime.revisionHistoryEmpty") }}
          </p>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>

<style scoped>
.reauth-restored {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 12px 14px;
  border: 1px solid var(--success-border, #9ed5b3);
  border-radius: 7px;
  background: var(--success-soft, #edf8f1);
  color: var(--text);
}
.reauth-restored p {
  margin: 3px 0 0;
  color: var(--text-secondary);
}
.environment-tabs {
  display: flex;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
}
.environment-command-bar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 0;
}
.environment-tab {
  display: inline-flex;
  min-height: 44px;
  flex: 0 0 auto;
  align-items: center;
  gap: 7px;
  padding: 0 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}
.environment-tab--active {
  border-bottom-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}
.environment-editor-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(290px, 0.27fr);
  gap: 16px;
  align-items: start;
}
.environment-editor {
  display: grid;
  min-height: 520px;
  gap: 20px;
}
.editor-section {
  display: grid;
  align-content: start;
  gap: 16px;
}
.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
}
.section-header h2,
.section-header h3,
.section-header p,
.capability-row p,
.readiness-check p {
  margin: 0;
}
.section-header p,
.capability-row p,
.readiness-check p,
.secondary-text {
  color: var(--text-secondary);
}
.safe-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1px;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--border);
}
.safe-summary > div {
  display: grid;
  gap: 6px;
  padding: 13px;
  background: var(--surface);
}
.safe-summary span,
.secret-safe-meta dt,
.effective-preview dt {
  color: var(--text-secondary);
  font-size: 0.78rem;
}
.environment-fields,
.capability-list,
.readiness-list,
.volume-list,
.destination-list {
  display: grid;
  gap: 10px;
}
.policy-group {
  display: grid;
  gap: 14px;
  padding-bottom: 18px;
  border-bottom: 1px solid var(--hairline);
}
.policy-group:last-of-type {
  padding-bottom: 0;
  border-bottom: 0;
}
.resource-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.resource-grid .field small,
.access-toggle small,
.volume-mount span {
  color: var(--text-secondary);
}
.volume-row {
  display: grid;
  grid-template-columns:
    minmax(160px, 1fr) minmax(150px, 0.72fr) minmax(120px, 0.5fr)
    minmax(210px, 1fr) 36px;
  gap: 10px;
  align-items: end;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.volume-mount {
  display: grid;
  min-width: 0;
  gap: 7px;
  align-self: center;
}
.volume-mount code {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.destination-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.destination-row p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.access-toggle {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
  cursor: pointer;
}
.access-toggle > span {
  display: grid;
  gap: 4px;
}
.policy-summary,
.digest-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
.digest-grid > div {
  display: grid;
  min-width: 0;
  gap: 4px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--surface);
}
.digest-grid span {
  color: var(--text-secondary);
  font-size: 0.78rem;
}
.environment-field-row {
  display: grid;
  grid-template-columns: minmax(190px, 0.45fr) minmax(260px, 1fr) 36px;
  gap: 10px;
  align-items: end;
}
.capability-row,
.readiness-check {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr) auto;
  gap: 12px;
  align-items: start;
  padding: 14px 0;
  border-bottom: 1px solid var(--hairline);
}
.selected-image {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.selected-image p,
.tool-heading p {
  margin: 3px 0 0;
  color: var(--text-secondary);
}
.selected-image code {
  display: block;
  overflow: hidden;
  margin-top: 6px;
  color: var(--text-secondary);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tool-heading {
  padding-top: 6px;
  border-top: 1px solid var(--hairline);
}
.tool-heading > span {
  color: var(--text-secondary);
  font-size: 0.8rem;
}
.tool-catalog {
  display: grid;
  gap: 10px;
}
.tool-option {
  display: grid;
  gap: 12px;
  padding: 13px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--surface);
}
.tool-option > label {
  display: flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
}
.tool-option > label > span {
  display: grid;
  gap: 2px;
}
.tool-option small {
  color: var(--text-secondary);
}
.tool-fields {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding-left: 26px;
}
.tool-fields .field--wide {
  grid-column: 1 / -1;
}
.capability-row > svg,
.readiness-icon {
  color: var(--subtle);
}
.readiness-icon--ready {
  color: var(--success);
}
.boundary-note,
.secret-warning {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 11px 12px;
  border: 1px solid var(--warning);
  border-radius: 7px;
  background: var(--warning-soft);
  color: var(--warning);
}
.secret-descriptor {
  display: grid;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
}
.secret-binding-fields {
  display: grid;
  grid-template-columns: minmax(190px, 0.4fr) minmax(280px, 1fr);
  gap: 10px;
  align-items: end;
}
.secret-safe-meta,
.effective-preview dl {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 18px;
  margin: 0;
}
.secret-safe-meta > div,
.effective-preview dl > div {
  display: grid;
  gap: 4px;
}
.secret-safe-meta dd,
.effective-preview dd {
  margin: 0;
}
.mono-input,
code {
  font-family: var(--font-mono);
}
.effective-preview {
  display: grid;
  gap: 12px;
  padding-top: 4px;
}
.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.chip-list span {
  padding: 4px 7px;
  border-radius: 5px;
  background: var(--canvas);
}
.validation-list {
  padding: 12px 12px 12px 30px;
  border: 1px solid var(--danger);
  border-radius: 7px;
  background: var(--danger-soft);
  color: var(--danger);
}
.conflict-panel {
  padding: 12px;
  border: 1px solid var(--warning);
  border-radius: 7px;
  background: var(--warning-soft);
}
.revision-panel {
  display: grid;
  gap: 0;
}
.revision-scroll {
  max-height: min(560px, calc(100vh - 270px));
  overflow-y: auto;
}
.revision-scroll > article {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: center;
  padding: 12px 0;
  border-bottom: 1px solid var(--hairline);
}
.revision-loading {
  padding: 10px 0;
  text-align: center;
}
.revision-panel article > div,
.revision-panel small,
.revision-panel code {
  display: block;
}
.revision-panel small,
.revision-panel code {
  margin-top: 3px;
  color: var(--text-secondary);
}
.icon-button--danger {
  color: var(--danger);
}
@media (max-width: 1040px) {
  .environment-editor-layout {
    grid-template-columns: 1fr;
  }
  .volume-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .volume-row .icon-button {
    justify-self: end;
  }
}
@media (max-width: 700px) {
  .environment-field-row,
  .secret-binding-fields,
  .safe-summary,
  .secret-safe-meta,
  .effective-preview dl,
  .tool-fields,
  .resource-grid,
  .volume-row,
  .policy-summary,
  .digest-grid {
    grid-template-columns: 1fr;
  }
  .tool-fields .field--wide {
    grid-column: auto;
  }
  .capability-row,
  .readiness-check {
    grid-template-columns: 24px minmax(0, 1fr);
  }
  .capability-row .status-badge,
  .readiness-check .status-badge {
    grid-column: 2;
    justify-self: start;
  }
}
</style>
