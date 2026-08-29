<script setup lang="ts">
import {
  Boxes,
  CheckCircle2,
  CircleAlert,
  Cpu,
  Eye,
  KeyRound,
  Network,
  Plus,
  RotateCcw,
  ServerCog,
  ShieldCheck,
  Trash2,
} from "@lucide/vue";
import { computed, onMounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import {
  compactIdentifier,
  environmentReadiness,
  safeSecretReference,
} from "@/features/runtime/environment-capabilities";
import {
  emptySecretDescriptor,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import { useRuntimeStore } from "@/features/runtime/store";
import type {
  RoleImageArtifact,
  RoleImageArtifactTool,
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
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
const runtime = useRuntimeStore();
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
const activeSection = ref<EditorSection>("GENERAL");
const input = reactive<RuntimeEnvironmentInput>({
  name: "",
  description: "",
  imageArtifactRef: "",
  tools: [],
  values: [],
  secretDescriptors: [],
});
const selectedImage = ref<AsyncEntityOption>();
const imageArtifact = ref<RoleImageArtifact>();
const imageLoading = ref(false);
const imageProblem = ref<AppProblem>();
const validation = computed(() => validateEnvironmentInput(input));
const readiness = computed(() => environmentReadiness(input, current.value));
const secretReferences = computed(() =>
  input.secretDescriptors.map(safeSecretReference),
);
const versionDigest = computed(() =>
  current.value
    ? compactIdentifier(current.value.currentVersion.digest)
    : undefined,
);
const sections: readonly { id: EditorSection; icon: typeof Boxes }[] = [
  { id: "GENERAL", icon: ServerCog },
  { id: "IMAGE_TOOLS", icon: Boxes },
  { id: "VALUES", icon: Cpu },
  { id: "SECRETS", icon: KeyRound },
  { id: "POLICY", icon: ShieldCheck },
  { id: "READINESS", icon: CheckCircle2 },
];

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
  input.secretDescriptors = value.currentVersion.secretDescriptors.map(
    (item) => ({ ...item }),
  );
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

function addValue(): void {
  input.values.push({ name: "", value: "" });
}

function addSecret(): void {
  input.secretDescriptors.push(emptySecretDescriptor());
}

async function load(): Promise<void> {
  if (!environmentRef.value) return;
  await Promise.all([
    runtime.loadEnvironment(environmentRef.value),
    runtime.loadEnvironmentVersions(environmentRef.value),
  ]);
  sync();
  if (current.value)
    await loadImageArtifact(
      current.value.currentVersion.image.recipeRef,
      current.value.currentVersion.image.artifactRef,
    );
}

async function save(): Promise<void> {
  if (validation.value.length) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const payload: RuntimeEnvironmentInput = {
      name: input.name.trim(),
      description: input.description.trim(),
      imageArtifactRef: input.imageArtifactRef,
      tools: input.tools.map((item) => ({
        name: item.name.trim(),
        command: item.command,
        description: item.description.trim(),
        usageHint: item.usageHint.trim(),
      })),
      values: input.values.map((item) => ({
        name: item.name.trim(),
        value: item.value,
      })),
      secretDescriptors: input.secretDescriptors.map((item) => ({
        name: item.name.trim(),
        secretName: item.secretName.trim(),
        secretKey: item.secretKey.trim(),
        secretUid: item.secretUid.trim(),
        secretResourceVersion: item.secretResourceVersion.trim(),
        contentSha256: item.contentSha256.trim(),
      })),
    };
    const saved = current.value
      ? await runtime.publishEnvironment(current.value, payload)
      : await runtime.createEnvironment(projectRef.value, payload);
    if (!environmentRef.value) {
      await router.replace(
        `/projects/${encodeURIComponent(projectRef.value)}/environments/${encodeURIComponent(saved.ref)}`,
      );
    } else {
      await runtime.loadEnvironmentVersions(saved.ref);
      sync(saved);
    }
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

async function rollback(versionRef: string): Promise<void> {
  if (!current.value) return;
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

watch(environmentRef, () => void load());
onMounted(() => void load());
</script>

<template>
  <PageFrame
    :title="current?.name ?? $t('runtime.newEnvironment')"
    :subtitle="$t('runtime.environmentEditorSubtitle')"
  >
    <template #actions>
      <RouterLink
        class="button"
        :to="`/projects/${encodeURIComponent(projectRef)}/environments`"
      >
        {{ $t("common.cancel") }}
      </RouterLink>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy || validation.length > 0"
        @click="save"
      >
        {{ current ? $t("runtime.publishRevision") : $t("common.create") }}
      </button>
    </template>

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
      <nav class="environment-tabs" :aria-label="$t('runtime.editorSections')">
        <button
          v-for="section in sections"
          :key="section.id"
          class="environment-tab"
          :class="{ 'environment-tab--active': activeSection === section.id }"
          type="button"
          :aria-current="activeSection === section.id ? 'page' : undefined"
          @click="activeSection = section.id"
        >
          <component :is="section.icon" :size="16" aria-hidden="true" />
          {{ $t(`runtime.section.${section.id}`) }}
        </button>
      </nav>

      <div class="environment-editor-layout">
        <form class="panel environment-editor" @submit.prevent="save">
          <section v-if="activeSection === 'GENERAL'" class="editor-section">
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
            class="editor-section"
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
            class="editor-section"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.variables") }}</h2>
                <p>{{ $t("runtime.variablesHelp") }}</p>
              </div>
              <button class="button" type="button" @click="addValue">
                <Plus :size="15" aria-hidden="true" />
                {{ $t("runtime.addVariable") }}
              </button>
            </div>
            <div v-if="input.values.length" class="environment-fields">
              <div
                v-for="(item, index) in input.values"
                :key="index"
                class="environment-field-row"
              >
                <label class="field">
                  <span>{{ $t("runtime.variableName") }}</span>
                  <input v-model="item.name" placeholder="VAR_NAME" />
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
            class="editor-section"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.secretReferences") }}</h2>
                <p>{{ $t("runtime.secretDescriptorsHelp") }}</p>
              </div>
              <button class="button" type="button" @click="addSecret">
                <Plus :size="15" aria-hidden="true" />
                {{ $t("runtime.addSecretDescriptor") }}
              </button>
            </div>
            <div class="secret-warning" role="note">
              <ShieldCheck :size="18" aria-hidden="true" />
              {{ $t("runtime.secretValuesForbidden") }}
            </div>
            <div class="secret-lifecycle-toolbar">
              <button
                class="button"
                type="button"
                disabled
                :title="$t('runtime.secretLifecycleUnavailable')"
              >
                <Plus :size="15" aria-hidden="true" />
                {{ $t("runtime.createSecret") }}
              </button>
              <button
                class="button"
                type="button"
                disabled
                :title="$t('runtime.secretLifecycleUnavailable')"
              >
                <RotateCcw :size="15" aria-hidden="true" />
                {{ $t("runtime.rotateSecret") }}
              </button>
              <button
                class="button"
                type="button"
                disabled
                :title="$t('runtime.secretRevealUnavailable')"
              >
                <Eye :size="15" aria-hidden="true" />
                {{ $t("runtime.revealSecret") }}
              </button>
              <span>{{ $t("runtime.secretActionsUnavailable") }}</span>
            </div>
            <article
              v-for="(item, index) in input.secretDescriptors"
              :key="index"
              class="secret-descriptor"
            >
              <div class="section-header">
                <div>
                  <strong>
                    {{
                      item.name ||
                      $t("runtime.secretDescriptor", { number: index + 1 })
                    }}
                  </strong>
                  <p>
                    {{
                      secretReferences[index]?.target ||
                      $t("runtime.secretReferenceIncomplete")
                    }}
                  </p>
                </div>
                <button
                  class="icon-button icon-button--danger"
                  type="button"
                  :aria-label="$t('common.delete')"
                  @click="input.secretDescriptors.splice(index, 1)"
                >
                  <Trash2 :size="16" aria-hidden="true" />
                </button>
              </div>
              <dl class="secret-safe-meta">
                <div>
                  <dt>{{ $t("runtime.secretMaskedHint") }}</dt>
                  <dd>{{ $t("runtime.secretMaskedHintUnavailable") }}</dd>
                </div>
                <div>
                  <dt>{{ $t("runtime.secretResourceVersion") }}</dt>
                  <dd>{{ secretReferences[index]?.revision || "—" }}</dd>
                </div>
                <div>
                  <dt>UID</dt>
                  <dd>
                    <code>{{ secretReferences[index]?.uidHint }}</code>
                  </dd>
                </div>
                <div>
                  <dt>SHA-256</dt>
                  <dd>
                    <code>{{ secretReferences[index]?.digestHint }}</code>
                  </dd>
                </div>
              </dl>
              <details>
                <summary>{{ $t("runtime.editSecretReference") }}</summary>
                <div class="secret-grid">
                  <label class="field">
                    <span>{{ $t("runtime.variableName") }}</span>
                    <input v-model="item.name" placeholder="SECRET_NAME" />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.secretName") }}</span>
                    <input v-model="item.secretName" />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.secretKey") }}</span>
                    <input v-model="item.secretKey" />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.secretUid") }}</span>
                    <input v-model="item.secretUid" />
                  </label>
                  <label class="field">
                    <span>{{ $t("runtime.secretResourceVersion") }}</span>
                    <input v-model="item.secretResourceVersion" />
                  </label>
                  <label class="field field--wide">
                    <span>SHA-256</span>
                    <input
                      v-model="item.contentSha256"
                      maxlength="64"
                      class="mono-input"
                    />
                  </label>
                </div>
              </details>
            </article>
            <p v-if="!input.secretDescriptors.length" class="secondary-text">
              {{ $t("runtime.noSecretReferences") }}
            </p>
          </section>

          <section
            v-else-if="activeSection === 'POLICY'"
            class="editor-section"
          >
            <div class="section-header">
              <div>
                <h2>{{ $t("runtime.resourcesAndAccess") }}</h2>
                <p>{{ $t("runtime.resourcesAndAccessHelp") }}</p>
              </div>
            </div>
            <div class="capability-list">
              <article class="capability-row">
                <Cpu :size="20" aria-hidden="true" />
                <div>
                  <strong>{{ $t("runtime.resources") }}</strong>
                  <p>{{ $t("runtime.resourcesUnavailable") }}</p>
                </div>
                <StatusBadge
                  state="UNAVAILABLE"
                  :label="$t('common.unavailable')"
                />
              </article>
              <article class="capability-row">
                <Network :size="20" aria-hidden="true" />
                <div>
                  <strong>{{ $t("runtime.networkPolicy") }}</strong>
                  <p>{{ $t("runtime.networkPolicyUnavailable") }}</p>
                </div>
                <StatusBadge
                  state="UNAVAILABLE"
                  :label="$t('common.unavailable')"
                />
              </article>
              <article class="capability-row">
                <ShieldCheck :size="20" aria-hidden="true" />
                <div>
                  <strong>{{ $t("runtime.kubernetesRbac") }}</strong>
                  <p>{{ $t("runtime.kubernetesRbacUnavailable") }}</p>
                </div>
                <StatusBadge
                  state="UNAVAILABLE"
                  :label="$t('common.unavailable')"
                />
              </article>
            </div>
            <div class="effective-preview">
              <h3>{{ $t("runtime.effectivePolicyPreview") }}</h3>
              <p>{{ $t("runtime.effectivePolicyUnavailable") }}</p>
            </div>
          </section>

          <section v-else class="editor-section">
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
                  <dd>{{ input.secretDescriptors.length }}</dd>
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
              </dl>
              <p class="boundary-note" role="note">
                <CircleAlert :size="17" aria-hidden="true" />
                {{ $t("runtime.effectivePolicyUnavailable") }}
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
                v-if="version.ref !== current?.currentVersion.ref"
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
.environment-tabs {
  display: flex;
  overflow-x: auto;
  border-bottom: 1px solid var(--border);
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
.secret-grid,
.capability-list,
.readiness-list {
  display: grid;
  gap: 10px;
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
.secret-lifecycle-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.secret-lifecycle-toolbar > span {
  color: var(--text-secondary);
  font-size: 0.82rem;
}
.secret-descriptor {
  display: grid;
  gap: 12px;
  padding: 14px 0;
  border-bottom: 1px solid var(--border);
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
.secret-descriptor details {
  border: 1px solid var(--border);
  border-radius: 7px;
}
.secret-descriptor summary {
  padding: 10px 12px;
  cursor: pointer;
  font-weight: 600;
}
.secret-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  padding: 0 12px 12px;
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
}
@media (max-width: 700px) {
  .environment-field-row,
  .secret-grid,
  .safe-summary,
  .secret-safe-meta,
  .effective-preview dl,
  .tool-fields {
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
