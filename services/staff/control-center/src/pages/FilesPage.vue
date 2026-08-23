<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import { useRoute } from "vue-router";

import { usePlatformStore } from "@/features/platform/store";
import type { Artifact } from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import PageFrame from "@/shared/ui/PageFrame.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

type FileTab = "FILES" | "KNOWLEDGE" | "RESULTS";
type FileKind = "ALL" | "TEXT" | "DOCUMENT" | "IMAGE";
type FileSource = "ALL" | "CONTROL_CENTER" | "AGENT_RESULT";

const maximumUploadBytes = 16 << 20;
const maximumTextPreviewBytes = 256 << 10;
const platform = usePlatformStore();
const route = useRoute();
const { locale, t } = useI18n();
const projectRef = computed(() => String(route.params.projectRef));
const project = computed(() => platform.projects[projectRef.value]);
const canUpload = computed(() =>
  project.value?.nextActions.includes("UPLOAD_ARTIFACT"),
);
const fileInput = ref<HTMLInputElement>();
const activeTab = ref<FileTab>("FILES");
const search = ref("");
const kind = ref<FileKind>("ALL");
const scanState = ref("ALL");
const source = ref<FileSource>("ALL");
const selectedRef = ref("");
const uploadBusy = ref(false);
const bindingBusy = ref("");
const contentBusy = ref(false);
const operationProblem = ref<AppProblem>();
const validationMessage = ref("");
const previewText = ref("");
const previewImage = ref("");
const previewUnavailable = ref(false);

const projectArtifacts = computed(() =>
  Object.values(platform.artifacts)
    .filter((artifact) => artifact.projectRef === projectRef.value)
    .sort((left, right) => right.createdAt.localeCompare(left.createdAt)),
);
const agents = computed(() =>
  Object.values(platform.agents)
    .filter(
      (agent) =>
        agent.projectRef === projectRef.value &&
        !agent.system &&
        agent.state !== "ARCHIVED",
    )
    .sort((left, right) => left.name.localeCompare(right.name)),
);
const filteredArtifacts = computed(() => {
  const normalizedSearch = search.value.trim().toLocaleLowerCase(locale.value);
  return projectArtifacts.value.filter((artifact) => {
    if (activeTab.value === "KNOWLEDGE" && artifact.agentBindings.length === 0)
      return false;
    if (
      activeTab.value === "RESULTS" &&
      !["AGENT_RESULT", "INTEGRATION_RESULT"].includes(artifact.source)
    )
      return false;
    if (
      normalizedSearch &&
      !artifact.fileName
        .toLocaleLowerCase(locale.value)
        .includes(normalizedSearch)
    )
      return false;
    if (scanState.value !== "ALL" && artifact.scanState !== scanState.value)
      return false;
    if (source.value !== "ALL" && artifact.source !== source.value)
      return false;
    return kind.value === "ALL" || artifactKind(artifact) === kind.value;
  });
});
const selectedArtifact = computed(() =>
  filteredArtifacts.value.find(
    (artifact) => artifact.ref === selectedRef.value,
  ),
);

watch(
  filteredArtifacts,
  (artifacts) => {
    if (!artifacts.some((artifact) => artifact.ref === selectedRef.value))
      selectArtifact(artifacts[0]?.ref ?? "");
  },
  { immediate: true },
);

function artifactKind(artifact: Artifact): Exclude<FileKind, "ALL"> {
  if (artifact.mediaType.startsWith("image/")) return "IMAGE";
  if (
    artifact.mediaType === "application/pdf" ||
    artifact.mediaType.includes("officedocument")
  )
    return "DOCUMENT";
  return "TEXT";
}

function clearPreview(): void {
  previewText.value = "";
  previewUnavailable.value = false;
  if (previewImage.value) URL.revokeObjectURL(previewImage.value);
  previewImage.value = "";
}

function selectArtifact(ref: string): void {
  if (selectedRef.value === ref) return;
  selectedRef.value = ref;
  clearPreview();
  operationProblem.value = undefined;
  validationMessage.value = "";
}

function formatBytes(value: number): string {
  const units = ["BYTE", "KILOBYTE", "MEGABYTE", "GIGABYTE"] as const;
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  const formatted = new Intl.NumberFormat(locale.value, {
    maximumFractionDigits: unit === 0 ? 0 : 1,
  }).format(size);
  const unitKey = units[unit] ?? "BYTE";
  return t(`files.unit.${unitKey}`, { value: formatted });
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

function fileType(artifact: Artifact): string {
  const extension = artifact.fileName.split(".").pop();
  return extension && extension !== artifact.fileName
    ? extension.toLocaleUpperCase(locale.value)
    : artifact.mediaType;
}

function bindingNames(artifact: Artifact): string {
  const names = artifact.agentBindings
    .map((ref) => platform.agents[ref]?.name)
    .filter((name): name is string => Boolean(name));
  return names.length > 0 ? names.join(", ") : "";
}

async function load(): Promise<void> {
  await Promise.all([
    platform.loadProject(projectRef.value),
    platform.loadArtifacts(projectRef.value),
    platform.loadAgents(projectRef.value),
  ]);
  const requested =
    typeof route.query.artifactRef === "string"
      ? route.query.artifactRef
      : undefined;
  if (
    requested &&
    platform.artifacts[requested]?.projectRef === projectRef.value
  )
    selectArtifact(requested);
}

async function upload(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = "";
  if (!file || !canUpload.value) return;
  operationProblem.value = undefined;
  validationMessage.value = "";
  if (file.size > maximumUploadBytes) {
    validationMessage.value = "files.uploadTooLarge";
    return;
  }
  uploadBusy.value = true;
  try {
    const artifact = await platform.uploadProjectArtifact(
      projectRef.value,
      file,
    );
    activeTab.value = "FILES";
    selectedRef.value = artifact.ref;
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    uploadBusy.value = false;
  }
}

async function changeBinding(
  artifact: Artifact,
  agentRef: string,
  enabled: boolean,
): Promise<void> {
  if (!artifact.nextActions.includes("BIND")) return;
  bindingBusy.value = `${artifact.ref}:${agentRef}`;
  operationProblem.value = undefined;
  try {
    await platform.changeArtifactAgentBinding(artifact, agentRef, enabled);
  } catch (error) {
    operationProblem.value = asProblem(error);
  } finally {
    bindingBusy.value = "";
  }
}

async function content(
  artifact: Artifact,
  purpose: "DOWNLOAD" | "PREVIEW",
): Promise<Blob | undefined> {
  contentBusy.value = true;
  operationProblem.value = undefined;
  try {
    return await platform.downloadArtifactContent(artifact.ref, purpose);
  } catch (error) {
    operationProblem.value = asProblem(error);
    return undefined;
  } finally {
    contentBusy.value = false;
  }
}

async function showPreview(artifact: Artifact): Promise<void> {
  clearPreview();
  if (!artifact.previewAvailable) {
    previewUnavailable.value = true;
    return;
  }
  const body = await content(artifact, "PREVIEW");
  if (!body) return;
  if (
    artifact.mediaType.startsWith("text/") ||
    artifact.mediaType === "application/json"
  ) {
    if (body.size > maximumTextPreviewBytes) {
      previewUnavailable.value = true;
      return;
    }
    previewText.value = await body.text();
    return;
  }
  if (artifact.mediaType.startsWith("image/")) {
    previewImage.value = URL.createObjectURL(body);
    return;
  }
  previewUnavailable.value = true;
}

async function download(artifact: Artifact): Promise<void> {
  const body = await content(artifact, "DOWNLOAD");
  if (!body) return;
  const url = URL.createObjectURL(body);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = artifact.fileName;
  anchor.hidden = true;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
}

onMounted(() => void load());
onBeforeUnmount(clearPreview);
</script>

<template>
  <PageFrame :title="$t('files.title')" :subtitle="$t('files.subtitle')">
    <template #actions>
      <input
        ref="fileInput"
        class="sr-only"
        type="file"
        accept=".txt,.md,.markdown,.csv,.json,.pdf,.png,.jpg,.jpeg,.gif,.webp,.docx,.xlsx,.pptx"
        :aria-label="$t('common.upload')"
        @change="upload"
      />
      <button
        v-if="canUpload"
        class="button button--primary"
        type="button"
        :disabled="uploadBusy"
        @click="fileInput?.click()"
      >
        {{ uploadBusy ? $t("files.uploading") : $t("common.upload") }}
      </button>
    </template>

    <ProblemNotice
      v-if="operationProblem"
      class="files-operation-problem"
      :problem="operationProblem"
      compact
    />
    <p v-if="validationMessage" class="field-error" role="alert">
      {{ $t(validationMessage) }}
    </p>

    <AsyncState
      :loading="platform.loading.artifacts"
      :problem="platform.problems.artifacts"
      :empty="projectArtifacts.length === 0"
      :empty-title="$t('files.emptyTitle')"
      :empty-text="$t('files.emptyText')"
      @retry="platform.loadArtifacts(projectRef)"
    >
      <template #empty-action>
        <button
          v-if="canUpload"
          class="button button--primary"
          type="button"
          :disabled="uploadBusy"
          @click="fileInput?.click()"
        >
          {{ $t("common.upload") }}
        </button>
      </template>

      <div class="files-tabs" role="tablist" :aria-label="$t('files.tabs')">
        <button
          v-for="tab in ['FILES', 'KNOWLEDGE', 'RESULTS'] as FileTab[]"
          :key="tab"
          class="files-tab"
          :class="{ 'files-tab--active': activeTab === tab }"
          type="button"
          role="tab"
          :aria-selected="activeTab === tab"
          @click="activeTab = tab"
        >
          {{ $t(`files.tab.${tab}`) }}
        </button>
      </div>

      <div class="files-toolbar" role="search">
        <label class="field files-search">
          <span class="sr-only">{{ $t("files.search") }}</span>
          <input
            v-model.trim="search"
            type="search"
            :placeholder="$t('files.search')"
          />
        </label>
        <label class="field">
          <span class="sr-only">{{ $t("files.typeFilter") }}</span>
          <select v-model="kind" :aria-label="$t('files.typeFilter')">
            <option value="ALL">{{ $t("files.kind.ALL") }}</option>
            <option value="TEXT">{{ $t("files.kind.TEXT") }}</option>
            <option value="DOCUMENT">{{ $t("files.kind.DOCUMENT") }}</option>
            <option value="IMAGE">{{ $t("files.kind.IMAGE") }}</option>
          </select>
        </label>
        <label class="field">
          <span class="sr-only">{{ $t("files.stateFilter") }}</span>
          <select v-model="scanState" :aria-label="$t('files.stateFilter')">
            <option value="ALL">{{ $t("files.allStates") }}</option>
            <option value="PENDING">{{ $t("states.PENDING") }}</option>
            <option value="SCANNING">{{ $t("states.SCANNING") }}</option>
            <option value="CLEAN">{{ $t("states.CLEAN") }}</option>
            <option value="QUARANTINED">
              {{ $t("states.QUARANTINED") }}
            </option>
            <option value="FAILED">{{ $t("states.FAILED") }}</option>
          </select>
        </label>
        <label class="field">
          <span class="sr-only">{{ $t("files.sourceFilter") }}</span>
          <select v-model="source" :aria-label="$t('files.sourceFilter')">
            <option value="ALL">{{ $t("files.allSources") }}</option>
            <option value="CONTROL_CENTER">
              {{ $t("files.source.CONTROL_CENTER") }}
            </option>
            <option value="AGENT_RESULT">
              {{ $t("files.source.AGENT_RESULT") }}
            </option>
          </select>
        </label>
      </div>

      <section
        v-if="filteredArtifacts.length === 0"
        class="empty-state files-filter-empty"
      >
        <h2>{{ $t("files.noMatches") }}</h2>
        <p>{{ $t("files.noMatchesText") }}</p>
      </section>

      <div v-else class="files-workspace">
        <section class="files-list-panel" :aria-label="$t('files.list')">
          <div class="files-list-header desktop-only" aria-hidden="true">
            <span>{{ $t("files.file") }}</span>
            <span>{{ $t("files.usedBy") }}</span>
            <span>{{ $t("files.revision") }}</span>
            <span>{{ $t("common.status") }}</span>
          </div>
          <button
            v-for="artifact in filteredArtifacts"
            :key="artifact.ref"
            class="file-row"
            :class="{ 'file-row--selected': selectedRef === artifact.ref }"
            type="button"
            :aria-pressed="selectedRef === artifact.ref"
            @click="selectArtifact(artifact.ref)"
          >
            <span class="file-row__identity">
              <strong>{{ artifact.fileName }}</strong>
              <small>
                {{ fileType(artifact) }} ·
                {{ formatBytes(artifact.sizeBytes) }} ·
                {{ $t(`files.source.${artifact.source}`) }}
              </small>
            </span>
            <span class="file-row__binding">
              {{ bindingNames(artifact) || $t("files.notBound") }}
            </span>
            <span class="file-row__revision">{{
              $t("files.revisionValue", { revision: artifact.revision })
            }}</span>
            <StatusBadge :state="artifact.scanState" />
          </button>
        </section>

        <aside
          v-if="selectedArtifact"
          class="files-detail-panel"
          :aria-label="$t('files.details')"
        >
          <header class="files-detail-header">
            <div>
              <p class="eyebrow">{{ $t("files.selectedFile") }}</p>
              <h2>{{ selectedArtifact.fileName }}</h2>
            </div>
            <StatusBadge :state="selectedArtifact.scanState" />
          </header>

          <dl class="metadata files-metadata">
            <div>
              <dt>{{ $t("files.size") }}</dt>
              <dd>{{ formatBytes(selectedArtifact.sizeBytes) }}</dd>
            </div>
            <div>
              <dt>{{ $t("files.revision") }}</dt>
              <dd>
                {{
                  $t("files.revisionValue", {
                    revision: selectedArtifact.revision,
                  })
                }}
              </dd>
            </div>
            <div>
              <dt>{{ $t("common.source") }}</dt>
              <dd>{{ $t(`files.source.${selectedArtifact.source}`) }}</dd>
            </div>
            <div>
              <dt>{{ $t("files.addedAt") }}</dt>
              <dd>{{ formatDate(selectedArtifact.createdAt) }}</dd>
            </div>
          </dl>

          <section class="files-preview">
            <div class="section-header">
              <h3>{{ $t("files.preview") }}</h3>
              <button
                v-if="
                  selectedArtifact.previewAvailable &&
                  selectedArtifact.nextActions.includes('DOWNLOAD') &&
                  !previewText &&
                  !previewImage
                "
                class="button button--ghost"
                type="button"
                :disabled="contentBusy"
                @click="showPreview(selectedArtifact)"
              >
                {{ $t("files.openPreview") }}
              </button>
            </div>
            <pre v-if="previewText" tabindex="0">{{ previewText }}</pre>
            <img
              v-else-if="previewImage"
              :src="previewImage"
              :alt="selectedArtifact.fileName"
            />
            <p
              v-else-if="
                previewUnavailable || !selectedArtifact.previewAvailable
              "
            >
              {{ $t("files.previewUnavailable") }}
            </p>
            <p v-else>{{ $t("files.previewReady") }}</p>
          </section>

          <section class="files-bindings">
            <h3>{{ $t("files.binding") }}</h3>
            <p>{{ $t("files.bindingHint") }}</p>
            <p v-if="agents.length === 0" class="muted-text">
              {{ $t("files.noAgents") }}
            </p>
            <label
              v-for="agent in agents"
              :key="agent.ref"
              class="binding-option"
            >
              <input
                type="checkbox"
                :checked="selectedArtifact.agentBindings.includes(agent.ref)"
                :disabled="
                  !selectedArtifact.nextActions.includes('BIND') ||
                  bindingBusy === `${selectedArtifact.ref}:${agent.ref}`
                "
                @change="
                  changeBinding(
                    selectedArtifact,
                    agent.ref,
                    ($event.target as HTMLInputElement).checked,
                  )
                "
              />
              <span>
                <strong>{{ agent.name }}</strong>
                <small>{{ agent.purpose }}</small>
              </span>
            </label>
          </section>

          <div class="files-detail-actions">
            <button
              v-if="selectedArtifact.nextActions.includes('DOWNLOAD')"
              class="button button--primary"
              type="button"
              :disabled="contentBusy"
              @click="download(selectedArtifact)"
            >
              {{ $t("common.download") }}
            </button>
          </div>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>
