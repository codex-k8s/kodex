<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from "vue";
import {
  FilePlus2,
  FolderOpen,
  Upload,
  RefreshCw,
  Plus,
  X,
  Maximize2,
} from "@lucide/vue";
import type {
  Artifact,
  SkillBundleFileInput,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import { uploadArtifactItem } from "@/features/files/api";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import {
  canUploadSkill,
  checkImportedArtifact,
  importFiles,
  refreshImportedArtifact,
  skillExtensions,
} from "./skill-import";
const props = defineProps<{ projectRef: string; existingPaths: string[] }>();
const emit = defineEmits<{
  close: [];
  imported: [files: SkillBundleFileInput[]];
}>();
interface ImportEntry {
  file: File;
  path: string;
  artifact?: Artifact;
  progress: number;
}
const entries = ref<ImportEntry[]>([]);
const mode = ref<"FILES" | "SOURCE">("FILES");
const source = ref("");
const filesInput = ref<HTMLInputElement>();
const directoryInput = ref<HTMLInputElement>();
const permitted = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const expanded = ref(false);
const controller = new AbortController();
let uploadController: AbortController | undefined;
const ready = computed(
  () =>
    entries.value.length > 0 &&
    entries.value.every((entry) => entry.artifact?.scanState === "CLEAN"),
);
void canUploadSkill(props.projectRef, controller.signal)
  .then((value) => {
    if (!controller.signal.aborted) permitted.value = value;
  })
  .catch((error: unknown) => {
    if (!controller.signal.aborted) problem.value = asProblem(error);
  });
function select(files: File[]): void {
  if (busy.value || !permitted.value) return;
  try {
    const added = importFiles(
      files,
      [...props.existingPaths, ...entries.value.map((entry) => entry.path)],
      entries.value.reduce((total, entry) => total + entry.file.size, 0),
    );
    entries.value.push(...added.map((entry) => ({ ...entry, progress: 0 })));
    problem.value = undefined;
  } catch (error) {
    problem.value = asProblem(error);
  }
}
function fromInput(event: Event): void {
  if (!(event.target instanceof HTMLInputElement)) return;
  select(Array.from(event.target.files ?? []));
  event.target.value = "";
}
function addSource(): void {
  select([new File([source.value], "SKILL.md", { type: "text/markdown" })]);
}
async function upload(): Promise<void> {
  if (busy.value || !permitted.value) return;
  busy.value = true;
  problem.value = undefined;
  uploadController = new AbortController();
  const signal = uploadController.signal;
  try {
    for (const entry of entries.value) {
      if (signal.aborted) break;
      if (entry.artifact) continue;
      const artifact = await uploadArtifactItem(props.projectRef, entry.file, {
        signal,
        onProgress: ({ loadedBytes, totalBytes }) => {
          entry.progress = totalBytes
            ? Math.round((100 * loadedBytes) / totalBytes)
            : 0;
        },
      });
      if (controller.signal.aborted) return;
      entry.artifact = checkImportedArtifact(artifact, props.projectRef);
      entry.progress = 100;
    }
  } catch (error) {
    if (!controller.signal.aborted && !signal.aborted)
      problem.value = asProblem(error);
  } finally {
    busy.value = false;
    uploadController = undefined;
  }
}
async function refresh(): Promise<void> {
  if (busy.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    for (const entry of entries.value) {
      if (!entry.artifact) continue;
      const artifact = await refreshImportedArtifact(
        entry.artifact,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      entry.artifact = artifact;
    }
  } catch (error) {
    if (!controller.signal.aborted) problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}
function attach(): void {
  if (!ready.value || busy.value) return;
  const files: SkillBundleFileInput[] = [];
  for (const entry of entries.value) {
    if (!entry.artifact || entry.artifact.scanState !== "CLEAN") return;
    files.push({
      path: entry.path,
      artifactRef: entry.artifact.ref,
      artifactRevision: entry.artifact.revision,
    });
  }
  emit("imported", files);
}
onBeforeUnmount(() => {
  controller.abort();
  uploadController?.abort();
  source.value = "";
  entries.value = [];
});
</script>
<template>
  <ModalDialog
    :title="$t('contextResources.importSkill')"
    :busy="busy"
    size="xl"
    @close="emit('close')"
  >
    <ProblemNotice v-if="problem" :problem="problem" />
    <div
      class="skill-import-actions"
      role="group"
      :aria-label="$t('managed.editMode')"
    >
      <button
        class="button"
        :aria-pressed="mode === 'FILES'"
        @click="mode = 'FILES'"
      >
        {{ $t("contextResources.importFiles") }}
      </button>
      <button
        class="button"
        :aria-pressed="mode === 'SOURCE'"
        @click="mode = 'SOURCE'"
      >
        SKILL.md
      </button>
    </div>
    <div v-if="mode === 'FILES'" class="skill-import-actions">
      <input
        ref="filesInput"
        type="file"
        multiple
        hidden
        :accept="skillExtensions.join(',')"
        @change="fromInput"
      />
      <input
        ref="directoryInput"
        type="file"
        multiple
        webkitdirectory
        hidden
        @change="fromInput"
      />
      <button
        class="button"
        :disabled="busy || !permitted"
        @click="filesInput?.click()"
      >
        <FilePlus2 :size="18" />{{ $t("contextResources.selectFiles") }}
      </button>
      <button
        class="button"
        :disabled="busy || !permitted"
        @click="directoryInput?.click()"
      >
        <FolderOpen :size="18" />{{ $t("contextResources.selectDirectory") }}
      </button>
    </div>
    <template v-else>
      <CodeEditor
        v-model="source"
        label="SKILL.md"
        :disabled="busy || !permitted"
      />
      <button
        class="button"
        :disabled="busy || !permitted || !source.trim()"
        @click="addSource"
      >
        <Plus :size="18" />{{ $t("contextResources.addManifest") }}
      </button>
    </template>
    <div class="skill-import-actions">
      <button
        class="button"
        :disabled="
          busy || !permitted || !entries.some((entry) => !entry.artifact)
        "
        @click="upload"
      >
        <Upload :size="18" />{{ $t("contextResources.upload") }}
      </button>
      <button
        class="icon-button"
        :disabled="busy || !entries.some((entry) => entry.artifact)"
        :title="$t('common.retry')"
        :aria-label="$t('common.retry')"
        @click="refresh"
      >
        <RefreshCw :size="18" />
      </button>
      <button
        v-if="busy && uploadController"
        class="icon-button"
        :title="$t('common.cancel')"
        :aria-label="$t('common.cancel')"
        @click="uploadController.abort()"
      >
        <X :size="18" />
      </button>
      <button
        v-if="entries.length > 6"
        class="icon-button"
        :title="$t('managed.expandFields')"
        :aria-label="$t('managed.expandFields')"
        @click="expanded = !expanded"
      >
        <Maximize2 :size="18" />
      </button>
    </div>
    <ul
      class="skill-import-list"
      :class="{ 'skill-import-list--expanded': expanded }"
    >
      <li v-for="(entry, index) in entries" :key="entry.path">
        <code>{{ entry.path }}</code>
        <StatusBadge v-if="entry.artifact" :state="entry.artifact.scanState" />
        <progress
          v-else
          :value="entry.progress"
          max="100"
          :aria-label="entry.path"
        />
        <button
          class="icon-button"
          :disabled="busy"
          :title="$t('contextResources.removeImport')"
          :aria-label="$t('contextResources.removeImport')"
          @click="entries.splice(index, 1)"
        >
          <X :size="18" />
        </button>
      </li>
    </ul>
    <button
      class="button button--primary"
      :disabled="!ready || busy"
      @click="attach"
    >
      <Plus :size="18" />{{ $t("contextResources.attachFiles") }}
    </button>
  </ModalDialog>
</template>
<style scoped>
.skill-import-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin: 12px 0;
}
.skill-import-list {
  margin: 16px 0;
  padding: 0;
  list-style: none;
  max-height: 432px;
  overflow: auto;
}
.skill-import-list--expanded {
  max-height: none;
}
.skill-import-list li {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 110px 36px;
  align-items: center;
  gap: 8px;
  min-height: 72px;
  border-bottom: 1px solid var(--border);
}
.skill-import-list code {
  overflow-wrap: anywhere;
}
.skill-import-list progress {
  width: 100%;
}
@media (max-width: 600px) {
  .skill-import-list li {
    grid-template-columns: minmax(0, 1fr) 36px;
  }
  .skill-import-list code {
    grid-column: 1 / -1;
  }
}
</style>
