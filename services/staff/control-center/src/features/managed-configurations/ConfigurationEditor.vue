<script setup lang="ts">
import {
  CheckCheck,
  Copy,
  GitFork,
  GitCompareArrows,
  History,
  RefreshCw,
  Save,
  Send,
  Trash2,
} from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useI18n } from "vue-i18n";
import type {
  ManagedConfiguration,
  ManagedConfigurationImpact,
  ManagedConfigurationResult,
  ManagedConfigurationRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import CodeDiff from "@/shared/ui/CodeDiff.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";
import * as api from "./api";
import ConfigurationFields from "./ConfigurationFields.vue";
import GitSourcePanel from "./GitSourcePanel.vue";
import {
  normalizedConfigurationDocument,
  parseConfigurationDocument,
  serializeConfigurationDocument,
} from "./document";
import { packageDiagnostics } from "./integration-package";
import {
  canPublish,
  canChangeDraft,
  canValidate,
  consumerKey,
  selectedConsumers,
} from "./model";

const props = defineProps<{
  kind: api.ConfigurationKind;
  configurationRef?: string;
  projectRef?: string;
}>();
const emit = defineEmits<{ created: [configuration: ManagedConfiguration] }>();
const { t } = useI18n();
const configuration = ref<ManagedConfiguration>();
const revision = ref<ManagedConfigurationRevision>();
const revisions = ref<ManagedConfigurationRevision[]>([]);
const pageToken = ref<string>();
const historyCursors = new Set<string>();
const name = ref("");
const content = ref("");
const format = ref<ManagedConfigurationRevision["contentFormat"]>(
  props.kind === "PROMPT_TEMPLATE" ? "TEXT" : "JSON",
);
const busy = ref(false);
const sourceBusy = ref(false);
const problem = ref<AppProblem>();
const historyOpen = ref(false);
const diffOpen = ref(false);
const compareRef = ref("");
const comparison = computed(() => {
  const baseline =
    revisions.value.find((item) => item.ref === compareRef.value) ??
    revision.value;
  if (!baseline) return undefined;
  try {
    return {
      original:
        baseline.contentFormat === "JSON" || baseline.contentFormat === "YAML"
          ? normalizedConfigurationDocument(
              baseline.content,
              baseline.contentFormat,
            )
          : baseline.content,
      modified:
        format.value === "JSON" || format.value === "YAML"
          ? normalizedConfigurationDocument(content.value, format.value)
          : content.value,
    };
  } catch {
    return undefined;
  }
});
const impactValue = ref<ManagedConfigurationImpact>();
const impactOpen = ref(false);
const impactQuery = ref("");
const impactLoading = ref(false);
const impactProblem = ref<AppProblem>();
let impactGeneration = 0;
let impactController: AbortController | undefined;
let impactTimer: ReturnType<typeof setTimeout> | undefined;
const impactCursors = new Set<string>();
function closeImpact(): void {
  clearTimeout(impactTimer);
  impactController?.abort();
  impactGeneration += 1;
  impactOpen.value = false;
  impactValue.value = undefined;
  impactLoading.value = false;
}
const selected = ref<string[]>([]);
const sourceAction = ref<"copy" | "detach">();
const discardOpen = ref(false);
const copyName = ref("");
const editorMode = ref<"FORM" | "SOURCE">(
  props.kind === "PROMPT_TEMPLATE" ? "SOURCE" : "FORM",
);
const controller = new AbortController();
let disposed = false;
onBeforeUnmount(() => {
  closeImpact();
  disposed = true;
  controller.abort();
  content.value = "";
});
const gitOwned = computed(() => configuration.value?.managedBy === "GIT");
const dirty = computed(
  () =>
    content.value !== (revision.value?.content ?? "") ||
    format.value !==
      (revision.value?.contentFormat ??
        (props.kind === "PROMPT_TEMPLATE" ? "TEXT" : "JSON")) ||
    name.value !== (configuration.value?.name ?? ""),
);
useUnsavedChanges(dirty, () => t("managed.discard"));
const language = computed(() =>
  format.value === "TEXT"
    ? undefined
    : (format.value.toLowerCase() as "json" | "yaml" | "toml"),
);
const localDiagnostics = computed(() => {
  if (props.kind !== "INTEGRATION_DEFINITION" || !content.value.trim())
    return [];
  if (format.value !== "JSON" && format.value !== "YAML")
    return [t("managed.invalidDocument")];
  try {
    return packageDiagnostics(
      parseConfigurationDocument(content.value, format.value),
    );
  } catch {
    return [t("managed.invalidDocument")];
  }
});
const canSave = computed(
  () =>
    !busy.value &&
    !sourceBusy.value &&
    dirty.value &&
    !gitOwned.value &&
    name.value.trim() &&
    ((configuration.value &&
      revision.value &&
      canChangeDraft(configuration.value, revision.value)) ||
      !!content.value.trim()) &&
    !content.value.includes("\0") &&
    new TextEncoder().encode(content.value).length <= 256 * 1024,
);
function changeFormat(event: Event): void {
  if (
    !(event.target instanceof HTMLSelectElement) ||
    busy.value ||
    gitOwned.value
  )
    return;
  const next = event.target.value;
  if (next !== "JSON" && next !== "YAML" && next !== "TEXT" && next !== "TOML")
    return;
  if (
    (next === "JSON" || next === "YAML") &&
    (format.value === "JSON" || format.value === "YAML") &&
    content.value.trim()
  ) {
    try {
      content.value = serializeConfigurationDocument(
        parseConfigurationDocument(content.value, format.value),
        next,
      );
    } catch {
      problem.value = asProblem(new Error("Invalid configuration document"));
      event.target.value = format.value;
      return;
    }
  }
  format.value = next;
}

function choose(item: ManagedConfigurationRevision): void {
  if (dirty.value && revision.value && !window.confirm(t("managed.discard")))
    return;
  revision.value = item;
  content.value = item.content;
  format.value = item.contentFormat;
  closeImpact();
  selected.value = [];
  historyOpen.value = false;
}
function accept(result: ManagedConfigurationResult): void {
  if (disposed) return;
  configuration.value = result.configuration;
  revision.value = result.revision;
  name.value = result.configuration.name;
  content.value = result.revision.content;
  format.value = result.revision.contentFormat;
  revisions.value = [
    result.revision,
    ...revisions.value.filter((item) => item.ref !== result.revision.ref),
  ];
  closeImpact();
  selected.value = [];
}
async function perform(work: () => Promise<void>): Promise<void> {
  if (busy.value) return;
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
async function load(more = false): Promise<void> {
  const ref = configuration.value?.ref ?? props.configurationRef;
  if (!ref || (more && !pageToken.value)) return;
  if (
    !more &&
    dirty.value &&
    revision.value &&
    !window.confirm(t("managed.discard"))
  )
    return;
  await perform(async () => {
    const result = await api.history(
      ref,
      controller.signal,
      more ? pageToken.value : undefined,
    );
    if (disposed) return;
    if (
      result.configuration.kind !== props.kind ||
      result.configuration.ref !== ref ||
      (props.projectRef &&
        result.configuration.projectRef !== props.projectRef) ||
      (more && result.nextPageToken && historyCursors.has(result.nextPageToken))
    )
      throw new Error("Invalid configuration history");
    const combined = more
      ? [...revisions.value, ...result.items]
      : result.items;
    if (new Set(combined.map((item) => item.ref)).size !== combined.length)
      throw new Error("Duplicate configuration revision");
    if (!more) {
      historyCursors.clear();
      configuration.value = result.configuration;
      name.value = result.configuration.name;
    }
    if (result.nextPageToken) historyCursors.add(result.nextPageToken);
    revisions.value = combined;
    pageToken.value = result.nextPageToken;
    if (!more) {
      revision.value = result.items[0] ?? result.configuration.currentRevision;
      content.value = revision.value?.content ?? "";
      format.value = revision.value?.contentFormat ?? format.value;
      closeImpact();
    }
  });
}
async function save(): Promise<void> {
  if (!canSave.value) return;
  await perform(async () => {
    const wasNew = !configuration.value;
    const current = configuration.value;
    const target = revision.value;
    const replacing = current && target && canChangeDraft(current, target);
    const result = replacing
      ? await api.changeDraft(current, target, {
          contentFormat: format.value,
          content: content.value,
        })
      : await api.createDraft(
          props.kind,
          {
            configurationRef: configuration.value?.ref,
            projectRef: configuration.value?.projectRef ?? props.projectRef,
            name: name.value.trim(),
            contentFormat: format.value,
            content: content.value,
          },
          configuration.value?.version,
        );
    accept(result);
    if (replacing) await refreshHistory(result);
    if (wasNew && !disposed) emit("created", result.configuration);
  });
}
async function refreshHistory(
  selectedResult: ManagedConfigurationResult,
): Promise<void> {
  revisions.value = [selectedResult.revision];
  pageToken.value = undefined;
  historyCursors.clear();
  const result = await api.history(
    selectedResult.configuration.ref,
    controller.signal,
  );
  if (disposed) return;
  if (
    result.configuration.ref !== selectedResult.configuration.ref ||
    result.configuration.kind !== props.kind ||
    result.configuration.version < selectedResult.configuration.version
  )
    throw new Error("Managed draft history mismatch");
  configuration.value = result.configuration;
  revisions.value = result.items;
  pageToken.value = result.nextPageToken;
  if (result.nextPageToken) historyCursors.add(result.nextPageToken);
  const exact = result.items.find(
    (item) => item.ref === selectedResult.revision.ref,
  );
  if (exact) {
    revision.value = exact;
    content.value = exact.content;
    format.value = exact.contentFormat;
  }
}
async function discardDraft(): Promise<void> {
  const current = configuration.value;
  const target = revision.value;
  if (!current || !target || !canChangeDraft(current, target)) return;
  discardOpen.value = false;
  await perform(async () => {
    const result = await api.changeDraft(current, target);
    accept(result);
    await refreshHistory(result);
  });
}
async function transition(action: "validate" | "publish"): Promise<void> {
  const current = configuration.value,
    target = revision.value;
  if (
    !current ||
    !target ||
    dirty.value ||
    !(action === "validate"
      ? canValidate(current, target)
      : canPublish(current, target))
  )
    return;
  await perform(async () =>
    accept(await api.transition(action, current, target)),
  );
}
async function showImpact(more = false): Promise<void> {
  const current = configuration.value,
    target = revision.value;
  if (
    !current ||
    !target ||
    dirty.value ||
    busy.value ||
    (more && (impactLoading.value || !impactValue.value?.nextPageToken))
  )
    return;
  clearTimeout(impactTimer);
  impactController?.abort();
  const active = new AbortController();
  impactController = active;
  const generation = ++impactGeneration;
  const previous = more ? impactValue.value : undefined;
  impactOpen.value = true;
  impactLoading.value = true;
  impactProblem.value = undefined;
  problem.value = undefined;
  if (!more) {
    impactValue.value = undefined;
    selected.value = [];
    impactCursors.clear();
  }
  try {
    const result = await api.impact(
      current,
      target,
      active.signal,
      impactQuery.value,
      previous?.nextPageToken,
    );
    if (disposed || generation !== impactGeneration) return;
    if (previous?.nextPageToken) impactCursors.add(previous.nextPageToken);
    const consumers = [...(previous?.consumers ?? []), ...result.consumers];
    if (
      result.configurationRef !== current.ref ||
      result.targetRevisionRef !== target.ref ||
      !result.digest ||
      !Number.isSafeInteger(result.total) ||
      result.total < consumers.length ||
      (result.nextPageToken && impactCursors.has(result.nextPageToken)) ||
      (previous &&
        (previous.digest !== result.digest ||
          previous.total !== result.total)) ||
      new Set(consumers.map(consumerKey)).size !== consumers.length ||
      consumers.some(
        (item) =>
          !item.ref ||
          !item.revisionRef ||
          !Number.isSafeInteger(item.version) ||
          item.version < 1,
      )
    )
      throw new Error("Invalid configuration impact");
    impactValue.value = { ...result, consumers };
  } catch (error) {
    if (!active.signal.aborted && generation === impactGeneration)
      impactProblem.value = asProblem(error);
  } finally {
    if (generation === impactGeneration) impactLoading.value = false;
  }
}
watch(
  impactQuery,
  () => {
    if (!impactOpen.value) return;
    clearTimeout(impactTimer);
    impactController?.abort();
    impactGeneration += 1;
    impactValue.value = undefined;
    selected.value = [];
    impactProblem.value = undefined;
    impactLoading.value = true;
    impactTimer = setTimeout(() => void showImpact(), 500);
  },
  { flush: "sync" },
);
async function rebind(): Promise<void> {
  const current = configuration.value,
    target = revision.value,
    impact = impactValue.value;
  if (
    !current ||
    !target ||
    target.state !== "PUBLISHED" ||
    !impact ||
    !selected.value.length ||
    problem.value ||
    impactLoading.value ||
    impactProblem.value ||
    busy.value
  )
    return;
  await perform(async () =>
    accept(
      await api.rebind(current, target, {
        impactDigest: impact.digest,
        consumers: selectedConsumers(impact.consumers, selected.value),
      }),
    ),
  );
}
async function changeSource(): Promise<void> {
  const current = configuration.value;
  if (!current || !gitOwned.value || !sourceAction.value) return;
  await perform(async () => {
    if (sourceAction.value === "copy") {
      const result = await api.copy(current, copyName.value.trim());
      if (!disposed) emit("created", result.configuration);
      accept(result);
    } else {
      const result = await api.detach(current);
      if (!disposed) configuration.value = result.configuration;
    }
    sourceAction.value = undefined;
  });
  if (!problem.value) await load();
}
watch(
  () => props.configurationRef,
  () => {
    void load();
  },
  { immediate: true },
);
</script>

<template>
  <section class="configuration-editor" :aria-busy="busy">
    <ProblemNotice v-if="problem" :problem="problem" />
    <header class="configuration-editor__toolbar">
      <StatusBadge v-if="revision" :state="revision.state" />
      <span v-if="revision">{{
        $t("managed.revision", { revision: revision.revision })
      }}</span>
      <span v-if="configuration">{{ configuration.managedBy }}</span>
      <button
        v-if="configuration"
        class="icon-button"
        :title="$t('common.refresh')"
        :aria-label="$t('common.refresh')"
        :disabled="busy || sourceBusy"
        @click="load()"
      >
        <RefreshCw :size="18" />
      </button>
      <button
        v-if="configuration"
        class="button"
        :disabled="busy || sourceBusy"
        @click="historyOpen = true"
      >
        <History :size="18" />{{ $t("managed.history") }}
      </button>
      <template v-if="gitOwned">
        <button
          class="button"
          :disabled="busy || sourceBusy"
          @click="sourceAction = 'detach'"
        >
          <GitFork :size="18" />{{ $t("managed.detach") }}
        </button>
        <button
          class="button"
          :disabled="busy || sourceBusy"
          @click="
            copyName = name;
            sourceAction = 'copy';
          "
        >
          <Copy :size="18" />{{ $t("managed.copy") }}
        </button>
      </template>
    </header>
    <p v-if="gitOwned" class="muted">{{ $t("managed.gitOwned") }}</p>
    <dl v-if="configuration" class="configuration-editor__source">
      <dt>{{ $t("managed.source") }}</dt>
      <dd>{{ configuration.source }}</dd>
      <dt>{{ $t("managed.sourceRevision") }}</dt>
      <dd>{{ configuration.sourceRevision }}</dd>
    </dl>
    <GitSourcePanel
      v-if="
        configuration &&
        (kind === 'ROLE_IMAGE' || kind === 'INTEGRATION_DEFINITION')
      "
      :key="configuration.ref"
      :configuration="configuration"
      :disabled="busy || dirty"
      @busy="sourceBusy = $event"
      @changed="load()"
    />
    <div class="configuration-editor__fields">
      <label
        >{{ $t("common.name")
        }}<input
          v-model="name"
          maxlength="160"
          :disabled="busy || !!configuration"
      /></label>
      <label
        >{{ $t("managed.format")
        }}<select
          :value="format"
          @change="changeFormat"
          :disabled="
            busy ||
            sourceBusy ||
            gitOwned ||
            kind === 'SYSTEM_STT' ||
            kind === 'PROMPT_TEMPLATE'
          "
        >
          <option v-if="kind === 'PROMPT_TEMPLATE'" value="TEXT">Text</option>
          <option value="JSON">JSON</option>
          <option value="YAML">YAML</option>
          <option value="TOML">TOML</option>
        </select></label
      >
    </div>
    <div
      v-if="kind !== 'PROMPT_TEMPLATE' && format !== 'TOML'"
      class="configuration-editor__toolbar"
      role="group"
      :aria-label="$t('managed.editMode')"
    >
      <button
        class="button"
        :aria-pressed="editorMode === 'FORM'"
        @click="editorMode = 'FORM'"
      >
        {{ $t("managed.form") }}</button
      ><button
        class="button"
        :aria-pressed="editorMode === 'SOURCE'"
        @click="editorMode = 'SOURCE'"
      >
        {{ $t("managed.source") }}
      </button>
    </div>
    <ConfigurationFields
      v-if="
        editorMode === 'FORM' &&
        (format === 'JSON' || format === 'YAML') &&
        kind !== 'PROMPT_TEMPLATE'
      "
      v-model="content"
      :kind="kind"
      :name="name"
      :format="format"
      :disabled="busy || sourceBusy || gitOwned"
      :initialize-stt="kind === 'SYSTEM_STT' && !configurationRef"
    />
    <CodeEditor
      v-else
      v-model="content"
      :label="$t('managed.content')"
      :language="language"
      :readonly="gitOwned"
      :disabled="busy || sourceBusy"
    />
    <ul
      v-if="localDiagnostics.length"
      class="configuration-editor__diagnostics"
      role="status"
      :aria-label="$t('managed.localValidation')"
    >
      <li v-for="diagnostic in localDiagnostics" :key="diagnostic">
        {{ diagnostic }}
      </li>
    </ul>
    <ul
      v-if="revision?.validationDiagnostics.length"
      class="configuration-editor__diagnostics"
      role="alert"
    >
      <li
        v-for="(diagnostic, index) in revision.validationDiagnostics"
        :key="index"
      >
        {{ diagnostic }}
      </li>
    </ul>
    <footer class="configuration-editor__toolbar">
      <button
        v-if="revision"
        class="button"
        :disabled="busy || sourceBusy"
        @click="diffOpen = true"
      >
        <GitCompareArrows :size="18" />{{ $t("managed.diff") }}
      </button>
      <button class="button" :disabled="!canSave" @click="save">
        <Save :size="18" />{{ $t("managed.saveDraft") }}
      </button>
      <button
        v-if="
          configuration && revision && canChangeDraft(configuration, revision)
        "
        class="button"
        :disabled="busy || sourceBusy"
        @click="discardOpen = true"
      >
        <Trash2 :size="18" />{{ $t("managed.discardDraft") }}
      </button>
      <button
        v-if="configuration && revision"
        class="button"
        :disabled="
          busy || sourceBusy || dirty || !canValidate(configuration, revision)
        "
        @click="transition('validate')"
      >
        <CheckCheck :size="18" />{{ $t("managed.validate") }}
      </button>
      <button
        v-if="configuration && revision"
        class="button button--primary"
        :disabled="
          busy || sourceBusy || dirty || !canPublish(configuration, revision)
        "
        @click="transition('publish')"
      >
        <Send :size="18" />{{ $t("managed.publish") }}
      </button>
      <button
        v-if="configuration && revision"
        class="button"
        :disabled="busy || sourceBusy || dirty"
        @click="showImpact()"
      >
        {{ $t("managed.impact") }}
      </button>
    </footer>
    <ModalDialog
      v-if="discardOpen"
      :title="$t('managed.discardDraft')"
      @close="discardOpen = false"
    >
      <p>{{ $t("managed.discardDraftConfirm") }}</p>
      <code>{{ revision?.ref }}</code>
      <template #actions
        ><button class="button" @click="discardOpen = false">
          {{ $t("common.cancel") }}</button
        ><button
          class="button"
          :disabled="busy || sourceBusy"
          @click="discardDraft"
        >
          {{ $t("managed.discardDraft") }}
        </button></template
      >
    </ModalDialog>
    <ModalDialog
      v-if="historyOpen"
      :title="$t('managed.history')"
      size="lg"
      :busy="busy"
      @close="historyOpen = false"
    >
      <div class="configuration-editor__history">
        <button
          v-for="item in revisions"
          :key="item.ref"
          class="configuration-editor__revision"
          @click="choose(item)"
        >
          <span>{{ $t("managed.revision", { revision: item.revision }) }}</span
          ><StatusBadge :state="item.state" /><time>{{ item.createdAt }}</time>
        </button>
      </div>
      <button
        v-if="pageToken"
        class="button"
        :disabled="busy || sourceBusy"
        @click="load(true)"
      >
        {{ $t("managed.more") }}
      </button>
    </ModalDialog>
    <ModalDialog
      v-if="impactOpen"
      :title="$t('managed.impact')"
      size="lg"
      :busy="busy"
      @close="closeImpact"
    >
      <input
        v-model="impactQuery"
        type="search"
        :aria-label="$t('common.search')"
        :placeholder="$t('common.search')"
        :disabled="busy || sourceBusy"
      />
      <ProblemNotice
        v-if="impactProblem"
        :problem="impactProblem"
        @retry="showImpact()"
      />
      <ProblemNotice v-if="problem" :problem="problem" @retry="showImpact()" />
      <p v-if="impactLoading" role="status">{{ $t("common.loading") }}</p>
      <p v-if="impactValue">
        {{ $t("impact.total", { count: impactValue.total }) }}
      </p>
      <p v-if="impactValue && !impactValue.consumers.length">
        {{ $t("managed.noConsumers") }}
      </p>
      <div class="configuration-editor__history">
        <label
          v-for="consumer in impactValue?.consumers ?? []"
          :key="consumerKey(consumer)"
          class="configuration-editor__consumer"
          ><input
            v-model="selected"
            type="checkbox"
            :value="consumerKey(consumer)"
            :disabled="
              busy ||
              impactLoading ||
              !!impactProblem ||
              revision?.state !== 'PUBLISHED' ||
              (!selected.includes(consumerKey(consumer)) &&
                selected.length >= 100)
            "
          /><span>{{ $t(`managed.consumers.${consumer.kind}`) }}</span
          ><code>{{ consumer.ref }}</code
          ><span>v{{ consumer.version }}</span></label
        >
      </div>
      <button
        v-if="impactValue?.nextPageToken"
        class="button"
        :disabled="busy || impactLoading"
        @click="showImpact(true)"
      >
        {{ $t("impact.more") }}
      </button>
      <button
        class="button button--primary"
        :disabled="
          busy ||
          sourceBusy ||
          impactLoading ||
          !!impactProblem ||
          !!problem ||
          !selected.length ||
          revision?.state !== 'PUBLISHED'
        "
        @click="rebind"
      >
        {{ $t("managed.rebind") }}
      </button>
    </ModalDialog>
    <ModalDialog
      v-if="diffOpen"
      :title="$t('managed.diff')"
      size="xl"
      @close="diffOpen = false"
    >
      <label class="configuration-editor__comparison">
        {{ $t("managed.compareRevision") }}
        <select v-model="compareRef">
          <option value="">{{ $t("managed.currentRevision") }}</option>
          <option v-for="item in revisions" :key="item.ref" :value="item.ref">
            v{{ item.revision }} · {{ item.state }}
          </option>
        </select>
      </label>
      <CodeDiff
        v-if="comparison"
        :original="comparison.original"
        :modified="comparison.modified"
        :label="$t('managed.diff')"
      />
      <p v-else role="alert">{{ $t("managed.invalidDocument") }}</p>
      <button
        v-if="pageToken"
        class="button"
        :disabled="busy || sourceBusy"
        @click="load(true)"
      >
        {{ $t("managed.more") }}
      </button>
    </ModalDialog>
    <ModalDialog
      v-if="sourceAction"
      :title="$t(`managed.${sourceAction}`)"
      :busy="busy"
      @close="sourceAction = undefined"
    >
      <label v-if="sourceAction === 'copy'"
        >{{ $t("common.name") }}<input v-model="copyName" maxlength="160"
      /></label>
      <p v-else>{{ $t("managed.detachConfirm") }}</p>
      <button
        class="button button--primary"
        :disabled="
          busy || sourceBusy || (sourceAction === 'copy' && !copyName.trim())
        "
        @click="changeSource"
      >
        {{ $t(`managed.${sourceAction}`) }}
      </button>
    </ModalDialog>
  </section>
</template>
<style scoped>
.configuration-editor {
  display: grid;
  gap: 16px;
  min-width: 0;
}
.configuration-editor__toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.configuration-editor__fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 140px;
  gap: 16px;
}
.configuration-editor__fields label {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.configuration-editor__source {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 4px 12px;
  font-size: 12px;
}
.configuration-editor__source dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.configuration-editor__history {
  max-height: 420px;
  overflow: auto;
  margin-bottom: 16px;
}
.configuration-editor__revision,
.configuration-editor__consumer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  min-height: 56px;
  width: 100%;
  border: 0;
  border-bottom: 1px solid var(--border);
  padding: 12px;
  background: var(--surface);
  text-align: left;
}
.configuration-editor__consumer code {
  overflow-wrap: anywhere;
  min-width: 0;
}
.configuration-editor__diagnostics {
  overflow-wrap: anywhere;
  color: var(--danger);
  max-height: 180px;
  overflow: auto;
}
.configuration-editor__diagnostics li {
  min-height: 30px;
}
.configuration-editor__comparison {
  display: grid;
  gap: 8px;
  margin-bottom: 16px;
}
.configuration-editor__comparison select {
  min-width: 0;
  max-width: 100%;
}
@media (max-width: 600px) {
  .configuration-editor__fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
