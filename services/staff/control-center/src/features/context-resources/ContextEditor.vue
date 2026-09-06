<script setup lang="ts">
import {
  Archive,
  Check,
  History,
  RotateCcw,
  Save,
  Send,
  ShieldCheck,
  Trash2,
  Upload,
} from "@lucide/vue";
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  Artifact,
  SkillBundle,
  SkillBundleSpecification,
  KodexMemoryRecord,
  MemoryRecordSpecification,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncEntityPicker from "@/shared/ui/AsyncEntityPicker.vue";
import type {
  AsyncEntityOption,
  AsyncEntityOptionPage,
} from "@/shared/ui/async-entity-picker";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";
import * as api from "./api";
import SkillImportDialog from "./SkillImportDialog.vue";
import SkillManifestFiles from "./SkillManifestFiles.vue";
import ContextBindingPanel from "./ContextBindingPanel.vue";
import { memoryContentAvailable } from "./retention";
import { validSkillSpecification } from "./skill-import";
import { artifacts, projects, runs, sourceRun } from "./selectors";
const props = defineProps<{
  kind: api.ContextKind;
  resourceRef?: string;
  projectRef?: string;
  agentRef?: string;
}>();
const emit = defineEmits<{ created: [ref: string, projectRef: string] }>();
const { t } = useI18n();
const skill = ref<SkillBundle>();
const memory = ref<KodexMemoryRecord>();
const memoryClock = ref(Date.now());
const expiredReads = new Set<string>();
const project = ref(props.projectRef ?? "");
const specification = ref<SkillBundleSpecification>({
  name: "",
  description: "",
  files: [],
});
const memoryInput = ref<MemoryRecordSpecification>({
  title: "",
  summary: "",
  retentionUntil: "",
});
const item = computed(() =>
  props.kind === "skills" ? skill.value : memory.value,
);
const revision = computed(() =>
  props.kind === "skills"
    ? (skill.value?.draftRevision ?? skill.value?.currentRevision)
    : memory.value?.currentRevision,
);
const fingerprint = () =>
  JSON.stringify(
    props.kind === "skills" ? specification.value : memoryInput.value,
  );
const saved = ref(fingerprint());
const dirty = computed(() => fingerprint() !== saved.value);
useUnsavedChanges(dirty, () => t("managed.discard"));
const busy = ref(false);
const loading = ref(false);
const problem = ref<AppProblem>();
const historyOpen = ref(false);
const importOpen = ref(false);
const revisions = ref<api.ContextRevision[]>([]);
const historyCursor = ref("");
const historyLoading = ref(false);
const historyProblem = ref<AppProblem>();
const openedHistory = ref(new Set<string>());
const action = ref<"archive" | "restore" | "purge" | "review" | "discard">();
const decision = ref<"APPROVE" | "REJECT">("APPROVE");
const comment = ref("");
const artifactValues = new Map<string, Artifact>();
const controller = new AbortController();
const selectedRun = ref<AsyncEntityOption>();
watch(
  () => [project.value, memoryInput.value.sourceRunRef] as const,
  async ([projectRef, reference], _previous, onCleanup) => {
    selectedRun.value = undefined;
    if (!projectRef || !reference) return;
    const request = new AbortController();
    onCleanup(() => request.abort());
    try {
      const option = await sourceRun(projectRef, reference, request.signal);
      if (!request.signal.aborted) selectedRun.value = option;
    } catch {
      /* Недоступный источник не заменяется другим запуском. */
    }
  },
);
let disposed = false;
const editable = computed(
  () =>
    !busy.value &&
    !loading.value &&
    (!props.resourceRef || !!item.value) &&
    (!item.value || item.value.state === "ACTIVE") &&
    (!memory.value ||
      memoryContentAvailable(memory.value.currentRevision, memoryClock.value)),
);
const draft = computed(() => skill.value?.draftRevision);
const retention = computed({
  get: () => {
    if (!memoryInput.value.retentionUntil) return "";
    const date = new Date(memoryInput.value.retentionUntil);
    if (!Number.isFinite(date.getTime())) return "";
    const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000);
    return local.toISOString().slice(0, 16);
  },
  set: (value: string) => {
    const date = new Date(value);
    memoryInput.value.retentionUntil = Number.isFinite(date.getTime())
      ? date.toISOString()
      : "";
  },
});
function acceptSkill(value: SkillBundle): void {
  if (disposed) return;
  if (props.projectRef && value.projectRef !== props.projectRef)
    throw new Error("Skill project scope mismatch");
  skill.value = value;
  project.value = value.projectRef;
  const current = value.draftRevision ?? value.currentRevision;
  specification.value = current
    ? {
        name: current.name,
        description: current.description,
        files: current.files.map((file) => ({
          path: file.path,
          artifactRef: file.artifactRef,
          artifactRevision: file.artifactRevision,
        })),
      }
    : { name: "", description: "", files: [] };
  saved.value = fingerprint();
  revisions.value = [];
  openedHistory.value.clear();
}
function acceptMemory(value: KodexMemoryRecord): void {
  if (disposed) return;
  if (props.projectRef && value.projectRef !== props.projectRef)
    throw new Error("Memory project scope mismatch");
  memory.value = value;
  project.value = value.projectRef;
  memoryInput.value = {
    title: value.currentRevision.title,
    summary: value.currentRevision.summary,
    retentionUntil: value.currentRevision.retentionUntil,
    sourceRunRef: value.currentRevision.provenance.sourceRef,
  };
  saved.value = fingerprint();
  revisions.value = [];
  openedHistory.value.clear();
}
async function perform(work: () => Promise<void>): Promise<void> {
  if (busy.value || controller.signal.aborted) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await work();
  } catch (error) {
    if (!disposed) problem.value = asProblem(error);
  } finally {
    if (!disposed) busy.value = false;
  }
}
async function load(): Promise<void> {
  if (!props.resourceRef || busy.value || loading.value) return;
  loading.value = true;
  problem.value = undefined;
  try {
    if (props.kind === "skills")
      acceptSkill(await api.readSkill(props.resourceRef, controller.signal));
    else
      acceptMemory(await api.readMemory(props.resourceRef, controller.signal));
  } catch (error) {
    if (!disposed) problem.value = asProblem(error);
  } finally {
    if (!disposed) loading.value = false;
  }
}
async function save(): Promise<void> {
  if (!editable.value || !project.value || !dirty.value) return;
  await perform(async () => {
    const wasNew = !item.value;
    if (props.kind === "skills")
      acceptSkill(
        await api.saveSkill(project.value, specification.value, skill.value),
      );
    else
      acceptMemory(
        await api.saveMemory(
          project.value,
          memoryInput.value,
          memory.value,
          props.agentRef,
        ),
      );
    const reference = skill.value?.ref ?? memory.value?.ref;
    if (!disposed && wasNew && reference)
      emit("created", reference, project.value);
  });
}
async function transition(
  next: "validate" | "publish" | "discard",
): Promise<void> {
  if (!skill.value || !draft.value || dirty.value) return;
  const current = skill.value;
  const target = draft.value;
  await perform(async () => {
    acceptSkill(await api.transitionSkill(current, next, target));
    action.value = undefined;
  });
}
async function confirmAction(): Promise<void> {
  const current = item.value;
  const selected = action.value;
  if (!current || !selected || dirty.value) return;
  if (selected === "discard") {
    await transition("discard");
    return;
  }
  await perform(async () => {
    if (selected === "review") {
      if (!skill.value || !draft.value) return;
      acceptSkill(
        await api.reviewSkill(
          skill.value,
          draft.value,
          decision.value,
          comment.value,
        ),
      );
    } else {
      const result = await api.lifecycle(props.kind, current, selected);
      if (props.kind === "skills") acceptSkill(result as SkillBundle);
      else acceptMemory(result as KodexMemoryRecord);
    }
    action.value = undefined;
    comment.value = "";
  });
}
async function loadHistory(more = false): Promise<void> {
  if (!item.value || historyLoading.value || (more && !historyCursor.value))
    return;
  historyOpen.value = true;
  historyLoading.value = true;
  historyProblem.value = undefined;
  const version = item.value.version;
  try {
    const page = await api.history(
      props.kind,
      item.value.ref,
      more ? historyCursor.value : undefined,
      controller.signal,
    );
    if (disposed || item.value.version !== version) return;
    const next = more ? [...revisions.value, ...page.items] : page.items;
    if (
      memory.value &&
      ["EXPIRED", "PURGED"].includes(memory.value.state) &&
      next.some((entry) => "redacted" in entry && !entry.redacted)
    )
      throw new Error("Expired memory history is not redacted");
    if (
      new Set(next.map((value) => value.ref)).size !== next.length ||
      next.length > page.total
    )
      throw new Error("Invalid context history cursor sequence");
    revisions.value = next;
    historyCursor.value = page.nextPageToken;
  } catch (error) {
    if (!disposed) historyProblem.value = asProblem(error);
  } finally {
    if (!disposed) historyLoading.value = false;
  }
}
async function loadArtifacts(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  if (!project.value) return { items: [] };
  const page = await artifacts(project.value, query, cursor, signal);
  for (const artifact of page.items)
    artifactValues.set(
      `${artifact.ref}:${String(artifact.revision)}`,
      artifact,
    );
  return {
    items: page.items.map((artifact) => ({
      ref: `${artifact.ref}:${String(artifact.revision)}`,
      title: artifact.fileName,
      description: `${artifact.mediaType} · ${String(artifact.sizeBytes)} B`,
      meta: `${artifact.scanState} · r${String(artifact.revision)}`,
      disabled: artifact.scanState !== "CLEAN",
    })),
    nextPageToken: page.nextPageToken,
  };
}
async function loadRuns(
  query: string,
  cursor: string | undefined,
  signal: AbortSignal,
): Promise<AsyncEntityOptionPage> {
  return project.value
    ? runs(project.value, query, cursor, signal)
    : { items: [] };
}
function addArtifact(value: unknown): void {
  const artifact =
    typeof value === "string" ? artifactValues.get(value) : undefined;
  if (
    !editable.value ||
    !artifact ||
    artifact.projectRef !== project.value ||
    specification.value.files.length >= 128
  )
    return;
  specification.value.files.push({
    path: artifact.fileName,
    artifactRef: artifact.ref,
    artifactRevision: artifact.revision,
  });
}
function chooseProject(value: unknown): void {
  if (item.value || typeof value !== "string" || !editable.value) return;
  if (specification.value.files.length && !window.confirm(t("managed.discard")))
    return;
  project.value = value;
  memoryInput.value.sourceRunRef = undefined;
  specification.value.files = [];
  artifactValues.clear();
}
void load();
function expireContent(): void {
  memoryClock.value = Date.now();
  for (const entry of revisions.value) {
    if ("summary" in entry && !memoryContentAvailable(entry, memoryClock.value))
      entry.summary = "";
  }
  const record = memory.value;
  if (
    !record ||
    memoryContentAvailable(record.currentRevision, memoryClock.value)
  )
    return;
  record.currentRevision.summary = "";
  memoryInput.value.summary = "";
  saved.value = fingerprint();
  if (
    record.state === "ACTIVE" &&
    !busy.value &&
    !loading.value &&
    !expiredReads.has(record.currentRevision.ref)
  ) {
    expiredReads.add(record.currentRevision.ref);
    void load();
  }
}
let retentionTimer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  if (props.kind !== "memory") return;
  retentionTimer = setInterval(expireContent, 1000);
  document.addEventListener("visibilitychange", expireContent);
});
onBeforeUnmount(() => {
  clearInterval(retentionTimer);
  document.removeEventListener("visibilitychange", expireContent);
  disposed = true;
  controller.abort();
  memoryInput.value.summary = "";
  revisions.value = [];
  artifactValues.clear();
});
</script>
<template>
  <section class="context-editor">
    <ProblemNotice v-if="problem" :problem="problem" @retry="load()" />
    <p v-if="loading" role="status">{{ $t("common.loading") }}</p>
    <header class="context-actions">
      <StatusBadge v-if="item" :state="item.state" /><span v-if="item"
        >v{{ item.version }}</span
      >
      <button
        class="button button--primary"
        :disabled="
          !editable ||
          !dirty ||
          !project ||
          (kind === 'skills' && !validSkillSpecification(specification))
        "
        @click="save()"
      >
        <Save :size="18" />{{ $t("common.save") }}
      </button>
      <button
        v-if="item"
        class="icon-button"
        :disabled="busy"
        :title="$t('managed.history')"
        :aria-label="$t('managed.history')"
        @click="loadHistory()"
      >
        <History :size="18" />
      </button>
      <button
        v-if="item?.state === 'ACTIVE'"
        class="icon-button"
        :disabled="busy || dirty"
        :title="$t('contextResources.archive')"
        :aria-label="$t('contextResources.archive')"
        @click="action = 'archive'"
      >
        <Archive :size="18" />
      </button>
      <button
        v-if="item?.state === 'ARCHIVED'"
        class="icon-button"
        :disabled="busy || dirty"
        :title="$t('contextResources.restore')"
        :aria-label="$t('contextResources.restore')"
        @click="action = 'restore'"
      >
        <RotateCcw :size="18" />
      </button>
      <button
        v-if="item?.state === 'ARCHIVED'"
        class="icon-button"
        :disabled="busy || dirty"
        :title="$t('contextResources.purge')"
        :aria-label="$t('contextResources.purge')"
        @click="action = 'purge'"
      >
        <Trash2 :size="18" />
      </button>
    </header>
    <AsyncEntityPicker
      v-if="!item"
      :model-value="project || null"
      :load-page="projects"
      :disabled="!editable"
      :trigger-label="$t('contextResources.project')"
      @update:model-value="chooseProject"
    />
    <RouterLink
      v-else
      :to="`/projects/${encodeURIComponent(item.projectRef)}`"
      >{{ item.projectRef }}</RouterLink
    >
    <fieldset :disabled="!editable" class="context-form">
      <template v-if="kind === 'skills'">
        <label
          >{{ $t("common.name")
          }}<input
            v-model="specification.name"
            maxlength="320"
            required
            :aria-label="$t('common.name')"
          /><span
            >{{ Array.from(specification.name).length }} / 160</span
          ></label
        >
        <label
          >{{ $t("common.description")
          }}<VoiceTextarea
            v-model="specification.description"
            maxlength="4000"
            :disabled="!editable"
            rows="3"
          /><span
            >{{ Array.from(specification.description).length }} / 2000</span
          ></label
        >
        <button
          class="button"
          type="button"
          :disabled="!editable || !project || specification.files.length >= 128"
          @click="importOpen = true"
        >
          <Upload :size="18" />{{ $t("contextResources.importSkill") }}
        </button>
        <AsyncEntityPicker
          :model-value="null"
          :load-page="loadArtifacts"
          :disabled="!editable || !project || specification.files.length >= 128"
          :trigger-label="$t('contextResources.addFile')"
          @update:model-value="addArtifact"
        />
      </template>
      <template v-else>
        <label
          >{{ $t("common.name")
          }}<input v-model="memoryInput.title" maxlength="320"
        /></label>
        <CodeEditor
          v-if="
            !memory ||
            memoryContentAvailable(memory.currentRevision, memoryClock)
          "
          v-model="memoryInput.summary"
          :label="$t('contextResources.summary')"
          :disabled="!editable"
        />
        <p v-else>{{ $t("contextResources.redacted") }}</p>
        <label
          >{{ $t("contextResources.sourceRun") }}
          <AsyncEntityPicker
            :model-value="memoryInput.sourceRunRef ?? ''"
            :selected="selectedRun"
            :load-page="loadRuns"
            :disabled="!editable || !project"
            :trigger-label="$t('contextResources.sourceRun')"
            @update:model-value="
              memoryInput.sourceRunRef =
                typeof $event === 'string' && $event ? $event : undefined
            "
          />
        </label>
        <label
          >{{ $t("contextResources.retention")
          }}<input v-model="retention" type="datetime-local" required
        /></label>
      </template>
    </fieldset>
    <SkillManifestFiles
      v-if="kind === 'skills'"
      v-model="specification.files"
      :disabled="!editable"
    />
    <ContextBindingPanel
      v-if="item?.currentRevision"
      :kind="kind"
      :resource-ref="item.ref"
      :project-ref="item.projectRef"
      :revision-ref="item.currentRevision.ref"
      :digest="item.currentRevision.digest"
      :agent-ref="agentRef"
      :owner-agent-ref="memory?.agentRef"
      :disabled="busy || dirty"
      :eligible="
        item.state === 'ACTIVE' &&
        (kind === 'skills'
          ? skill?.currentRevision?.state === 'PUBLISHED' &&
            skill.currentRevision.scanState === 'CLEAN'
          : !!memory &&
            memoryContentAvailable(memory.currentRevision, memoryClock))
      "
    />
    <template v-if="draft">
      <div class="context-actions">
        <StatusBadge :state="draft.state" /><StatusBadge
          :state="draft.scanState"
        />
        <button
          class="button"
          :disabled="
            !editable ||
            dirty ||
            !['DRAFT', 'INVALID', 'REJECTED'].includes(draft.state)
          "
          @click="transition('validate')"
        >
          <Check :size="18" />{{ $t("contextResources.validate") }}
        </button>
        <button
          class="button"
          :disabled="
            !editable ||
            dirty ||
            draft.state !== 'VALIDATED' ||
            draft.scanState !== 'CLEAN'
          "
          @click="action = 'review'"
        >
          <ShieldCheck :size="18" />{{ $t("contextResources.review") }}
        </button>
        <button
          class="button"
          :disabled="
            !editable ||
            dirty ||
            draft.state !== 'APPROVED' ||
            draft.scanState !== 'CLEAN'
          "
          @click="transition('publish')"
        >
          <Send :size="18" />{{ $t("contextResources.publish") }}
        </button>
        <button
          class="button"
          :disabled="
            busy || dirty || ['PUBLISHED', 'DISCARDED'].includes(draft.state)
          "
          @click="action = 'discard'"
        >
          <Trash2 :size="18" />{{ $t("contextResources.discard") }}
        </button>
      </div>
      <ul v-if="draft.diagnostics.length">
        <li v-for="(diagnostic, index) in draft.diagnostics" :key="index">
          {{ diagnostic }}
        </li>
      </ul>
    </template>
    <dl v-if="revision" class="context-provenance">
      <dt>{{ $t("contextResources.revision") }}</dt>
      <dd>{{ revision.ref }} / {{ revision.revision }}</dd>
      <dt>Digest</dt>
      <dd>{{ revision.digest }}</dd>
      <dt>{{ $t("managed.source") }}</dt>
      <dd>
        {{ revision.provenance.sourceKind }} /
        {{ revision.provenance.sourceRef }}
      </dd>
      <dt>{{ $t("contextResources.actor") }}</dt>
      <dd>{{ revision.provenance.actorRef }}</dd>
      <dt>{{ $t("contextResources.createdAt") }}</dt>
      <dd>{{ revision.provenance.createdAt }}</dd>
      <template v-if="'scanState' in revision">
        <dt>{{ $t("contextResources.scan") }}</dt>
        <dd>
          <StatusBadge :state="revision.scanState" /> {{ revision.scanEngine }}
        </dd>
        <dt>{{ $t("contextResources.scanDigest") }}</dt>
        <dd>{{ revision.scanDigest }}</dd>
        <dt>{{ $t("contextResources.scannedAt") }}</dt>
        <dd>{{ revision.scannedAt }}</dd>
        <dt>{{ $t("contextResources.reviewedBy") }}</dt>
        <dd>{{ revision.reviewedBy }}</dd>
        <dt>{{ $t("contextResources.reviewedAt") }}</dt>
        <dd>{{ revision.reviewedAt }}</dd>
      </template>
    </dl>
  </section>
  <ModalDialog
    v-if="action"
    :title="$t(`contextResources.${action}`)"
    :busy="busy"
    @close="action = undefined"
  >
    <p>{{ item?.ref }}</p>
    <p v-if="action === 'purge'">{{ $t("contextResources.purgeConfirm") }}</p>
    <template v-if="action === 'review'"
      ><select
        v-model="decision"
        :disabled="busy"
        :aria-label="$t('contextResources.decision')"
      >
        <option value="APPROVE">{{ $t("contextResources.approve") }}</option>
        <option value="REJECT">
          {{ $t("contextResources.reject") }}
        </option></select
      ><label
        >{{ $t("contextResources.comment")
        }}<VoiceTextarea v-model="comment" :disabled="busy" rows="3" /></label
    ></template>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <template #actions
      ><button class="button" :disabled="busy" @click="action = undefined">
        {{ $t("common.cancel") }}</button
      ><button
        class="button button--primary"
        :disabled="busy"
        @click="confirmAction()"
      >
        {{ $t(`contextResources.${action}`) }}
      </button></template
    >
  </ModalDialog>
  <ModalDialog
    v-if="historyOpen"
    :title="$t('managed.history')"
    size="xl"
    @close="historyOpen = false"
  >
    <ProblemNotice
      v-if="historyProblem"
      :problem="historyProblem"
      @retry="loadHistory()"
    />
    <p v-if="historyLoading" role="status">{{ $t("common.loading") }}</p>
    <div class="context-history">
      <details
        v-for="entry in revisions"
        :key="entry.ref"
        @toggle="
          ($event.target as HTMLDetailsElement).open
            ? openedHistory.add(entry.ref)
            : openedHistory.delete(entry.ref)
        "
      >
        <summary>
          {{ entry.revision }} /
          {{ "title" in entry ? entry.title : entry.name }}
        </summary>
        <code>{{ entry.ref }} / {{ entry.digest }}</code
        ><template v-if="'summary' in entry"
          ><CodeEditor
            v-if="
              memoryContentAvailable(entry, memoryClock) &&
              openedHistory.has(entry.ref)
            "
            :model-value="entry.summary"
            :label="$t('contextResources.summary')"
            readonly
          />
          <p v-else-if="!memoryContentAvailable(entry, memoryClock)">
            {{ $t("contextResources.redacted") }}
          </p></template
        ><template v-else
          ><StatusBadge :state="entry.state" /><StatusBadge
            :state="entry.scanState"
          />
          <p v-for="file in entry.files" :key="file.path">
            <code
              >{{ file.path }} / {{ file.artifactRef }} / r{{
                file.artifactRevision
              }}
              / {{ file.digest }}</code
            >
          </p></template
        >
      </details>
    </div>
    <button
      v-if="historyCursor"
      class="button"
      :disabled="historyLoading"
      @click="loadHistory(true)"
    >
      {{ $t("impact.more") }}
    </button>
  </ModalDialog>
  <SkillImportDialog
    v-if="importOpen && project"
    :project-ref="project"
    :existing-paths="specification.files.map((file) => file.path)"
    @close="importOpen = false"
    @imported="
      specification.files.push(...$event);
      importOpen = false;
    "
  />
</template>
<style scoped>
.context-editor {
  min-width: 0;
  display: grid;
  gap: 20px;
}
.context-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}
.context-form {
  border: 0;
  padding: 0;
  margin: 0;
  min-width: 0;
  display: grid;
  gap: 16px;
}
.context-form label {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.context-provenance {
  display: grid;
  grid-template-columns: 160px minmax(0, 1fr);
  gap: 10px;
}
.context-provenance dd {
  margin: 0;
}
code,
dd,
li {
  overflow-wrap: anywhere;
}
.context-history {
  max-height: 432px;
  overflow: auto;
}
.context-history details {
  min-height: 72px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border);
}
.context-history summary {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .context-provenance {
    grid-template-columns: 1fr;
  }
  .context-provenance dd {
    margin-bottom: 10px;
  }
}
</style>
