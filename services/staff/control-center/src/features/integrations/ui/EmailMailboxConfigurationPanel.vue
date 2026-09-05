<script setup lang="ts">
import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from "vue";
import { useI18n } from "vue-i18n";
import type {
  EmailMailboxCredential,
  EmailMailboxCredentialKind,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { AppProblem, asProblem } from "@/shared/api/problem";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";
import CodeEditor from "@/shared/ui/CodeEditor.vue";
import CodeDiff from "@/shared/ui/CodeDiff.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import EmailMailboxFields from "./EmailMailboxFields.vue";
import { mailboxEditor } from "../email-mailbox-editor";
import {
  listMailboxCredentials,
  type MailboxAction,
} from "../email-mailbox-api";

const props = defineProps<{
  disabled?: boolean;
  connection: IntegrationConnection;
  initialConfigurationRef?: string;
  initialRevisionRef?: string;
}>();
const emit = defineEmits<{
  busy: [value: boolean];
  saved: [];
  selected: [configurationRef: string, revisionRef: string];
}>();
const { t } = useI18n();
const editor = reactive(mailboxEditor(props.connection.ref));
const credentials = ref<EmailMailboxCredential[]>([]);
const credentialKind = ref<EmailMailboxCredentialKind>("AUTH_SECRET");
const credentialCursor = ref("");
const credentialTotal = ref(0);
const credentialBusy = ref(false);
const credentialProblem = ref<ReturnType<typeof asProblem>>();
const controller = new AbortController();
const ownBusy = computed(() => editor.busy || credentialBusy.value);
const locked = computed(() => ownBusy.value || props.disabled);
const guarded = computed(
  () => locked.value || editor.uncertain || editor.dirty,
);
const actions: MailboxAction[] = [
  "CREATE_DRAFT",
  "SAVE",
  "VALIDATE",
  "PUBLISH",
  "DISCARD",
  "BIND",
  "UNBIND",
  "DETACH",
  "COPY",
];
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let deliveryTimer: ReturnType<typeof setTimeout> | undefined;
let deliveryAttempts = 0;
let disposed = false;
useUnsavedChanges(guarded, () => t("mailbox.leave"));
watch(ownBusy, (value) => emit("busy", value), { immediate: true });
function canClose(): boolean {
  if (locked.value) return false;
  return !guarded.value || window.confirm(t("mailbox.leave"));
}
defineExpose({ canClose });
function reason(action: MailboxAction): string {
  const item = (editor.view?.nextActions ?? editor.listActions).find(
    (item) => item.action === action,
  );
  return item ? t(`mailbox.reasons.${item.reason}`) : t("mailbox.unavailable");
}
function canExecute(action: MailboxAction): boolean {
  return (
    !locked.value &&
    !editor.uncertain &&
    editor.allowed(action) &&
    (!editor.dirty ||
      ["SAVE", "CREATE_DRAFT", "DISCARD", "COPY"].includes(action)) &&
    (action !== "CREATE_DRAFT" || Boolean(editor.name.trim())) &&
    (action !== "COPY" || Boolean(editor.copyName.trim()))
  );
}
async function execute(action?: MailboxAction): Promise<void> {
  if (locked.value) return;
  if (action && !canExecute(action)) return;
  if (action === "DISCARD" && !window.confirm(t("mailbox.discardConfirm")))
    return;
  await editor.execute(action);
  if (!editor.problem && !editor.uncertain && editor.view) {
    emit("saved");
    emit("selected", editor.view.configuration.ref, editor.view.revision.ref);
    scheduleDelivery();
  }
}
async function open(
  configurationRef?: string,
  revisionRef?: string,
): Promise<void> {
  if (!canClose()) return;
  await editor.open(configurationRef, revisionRef);
  if (editor.view)
    emit("selected", editor.view.configuration.ref, editor.view.revision.ref);
  scheduleDelivery();
}
function newConfiguration(): void {
  if (canClose()) editor.newConfiguration();
}
function search(): void {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    if (!disposed) void editor.catalog();
  }, 250);
}
async function loadCredentials(more = false): Promise<void> {
  if (locked.value || editor.uncertain) return;
  credentialBusy.value = true;
  credentialProblem.value = undefined;
  const kind = credentialKind.value;
  try {
    const page = await listMailboxCredentials(
      props.connection.ref,
      kind,
      controller.signal,
      more ? credentialCursor.value : undefined,
    );
    if (disposed) return;
    const previous = credentials.value.filter(
      (item) => item.kind !== kind || more,
    );
    const values = [...previous, ...page.items];
    if (
      new Set(
        values.map((item) => JSON.stringify([item.name, item.generation])),
      ).size !== values.length
    )
      throw new Error("Mailbox credential pages contain duplicate identity");
    credentials.value = values;
    credentialCursor.value = page.nextPageToken;
    credentialTotal.value = page.total;
  } catch (error) {
    if (!disposed) {
      const value = asProblem(error);
      credentialProblem.value = new AppProblem({
        status: value.status,
        code: value.code,
        kind: value.kind,
        retryable: value.retryable,
      });
    }
  } finally {
    if (!disposed) credentialBusy.value = false;
  }
}
function scheduleDelivery(): void {
  clearTimeout(deliveryTimer);
  if (
    disposed ||
    deliveryAttempts >= 5 ||
    editor.view?.publication?.state !== "PENDING"
  )
    return;
  deliveryTimer = setTimeout(() => {
    if (
      disposed ||
      locked.value ||
      editor.dirty ||
      editor.uncertain ||
      !editor.view
    )
      return;
    deliveryAttempts++;
    void editor
      .open(editor.view.configuration.ref, editor.view.revision.ref)
      .then(scheduleDelivery);
  }, 2000);
}
watch(
  () => editor.view?.publication?.ref,
  () => {
    deliveryAttempts = 0;
  },
);
onMounted(async () => {
  await editor.catalog();
  if (disposed) return;
  if (props.initialConfigurationRef || editor.total > 0)
    await editor.open(props.initialConfigurationRef, props.initialRevisionRef);
  if (controller.signal.aborted) return;
  await loadCredentials();
  scheduleDelivery();
});
onBeforeUnmount(() => {
  disposed = true;
  clearTimeout(searchTimer);
  clearTimeout(deliveryTimer);
  controller.abort();
  editor.dispose();
  emit("busy", false);
});
</script>

<template>
  <section
    class="mailbox-panel"
    :aria-label="t('mailbox.title')"
    :aria-busy="locked"
  >
    <h3>{{ t("mailbox.title") }}</h3>
    <ProblemNotice v-if="editor.problem" :problem="editor.problem" compact />
    <p v-if="editor.uncertain" role="status">{{ t("mailbox.uncertain") }}</p>
    <button
      v-if="editor.uncertain"
      class="button"
      :disabled="locked"
      @click="execute()"
    >
      {{ t("mailbox.retryExact") }}
    </button>
    <details>
      <summary>{{ t("mailbox.catalog") }} · {{ editor.total }}</summary>
      <label class="field"
        ><span>{{ t("mailbox.search") }}</span
        ><input
          v-model="editor.query"
          type="search"
          :disabled="locked || editor.uncertain"
          @input="search"
      /></label>
      <button
        v-for="item in editor.list"
        :key="item.configuration.ref"
        class="button mailbox-panel__item"
        :disabled="locked || editor.uncertain"
        @click="open(item.configuration.ref, item.revision.ref)"
      >
        {{ item.configuration.name }} · {{ item.revision.revision }}
        <StatusBadge :state="item.revision.state" />
      </button>
      <button
        v-if="editor.nextPageToken"
        class="button"
        :disabled="locked || editor.uncertain"
        @click="editor.catalog(true)"
      >
        {{ t("common.loadMore") }}
      </button>
      <button
        class="button"
        :disabled="
          locked ||
          editor.uncertain ||
          !editor.listActions.some(
            (item) => item.action === 'CREATE_DRAFT' && item.enabled,
          )
        "
        @click="newConfiguration"
      >
        {{ t("mailbox.newConfiguration") }}
      </button>
    </details>
    <div v-if="editor.view" class="mailbox-panel__metadata">
      <StatusBadge :state="editor.view.revision.state" />
      <span
        >{{ editor.view.configuration.managedBy }} ·
        {{ editor.view.configuration.source }} ·
        {{ editor.view.configuration.sourceRevision }}</span
      >
      <code
        >{{ editor.view.configuration.ref }} / {{ editor.view.revision.ref }} ·
        {{ editor.view.revision.digest }}</code
      >
      <p>
        {{ t("mailbox.boundRevision") }}:
        <code>{{ editor.view.boundRevisionRef || "—" }}</code>
      </p>
      <div v-if="editor.view.publication">
        <p>
          {{ t("mailbox.delivery") }}:
          <StatusBadge :state="editor.view.publication.state" />
        </p>
        <p>
          {{ t("mailbox.deliveryRevision") }}:
          <code>{{
            editor.view.publication.configurationRevisionRef || "—"
          }}</code>
        </p>
        <p v-if="editor.view.publication.failureCode">
          {{ t(`mailbox.failure.${editor.view.publication.failureCode}`) }}
        </p>
        <p>{{ t("mailbox.deliveryHelp") }}</p>
      </div>
      <button
        class="button"
        :disabled="locked || editor.uncertain"
        @click="open(editor.view.configuration.ref, editor.view.revision.ref)"
      >
        {{ t("vfs.refresh") }}
      </button>
      <button
        v-if="editor.view.revision.parentRevisionRef"
        class="button"
        :disabled="locked || editor.uncertain"
        @click="
          open(
            editor.view.configuration.ref,
            editor.view.revision.parentRevisionRef,
          )
        "
      >
        {{ t("mailbox.previousRevision") }}
      </button>
    </div>
    <label class="field"
      ><span>{{ t("common.name") }}</span
      ><input
        v-model="editor.name"
        :disabled="
          locked || editor.uncertain || Boolean(editor.view) || !editor.writable
        "
    /></label>
    <div class="mailbox-panel__actions">
      <button
        class="button"
        :disabled="locked || editor.uncertain || editor.mode === 'FORM'"
        @click="editor.preview('FORM')"
      >
        {{ t("mailbox.form") }}
      </button>
      <button
        class="button"
        :disabled="locked || editor.uncertain || editor.mode === 'YAML'"
        @click="editor.preview('YAML')"
      >
        YAML
      </button>
      <button
        class="button"
        :disabled="locked || editor.uncertain"
        @click="editor.preview()"
      >
        {{ t("mailbox.preview") }}
      </button>
    </div>
    <EmailMailboxFields
      v-if="editor.mode === 'FORM'"
      :value="editor.specification"
      :credentials="credentials"
      :disabled="locked || editor.uncertain || !editor.writable"
      @change="editor.specification = $event"
    />
    <CodeEditor
      v-else
      v-model="editor.yaml"
      :label="t('mailbox.yaml')"
      language="yaml"
      :disabled="locked || editor.uncertain"
      :readonly="!editor.writable"
    />
    <details>
      <summary>{{ t("mailbox.credentialCatalog") }}</summary>
      <ProblemNotice
        v-if="credentialProblem"
        :problem="credentialProblem"
        compact
      />
      <label class="field"
        ><span>{{ t("mailboxCredential.kind") }}</span
        ><select
          v-model="credentialKind"
          :disabled="locked || editor.uncertain"
          @change="loadCredentials()"
        >
          <option
            v-for="kind in [
              'AUTH_SECRET',
              'USERNAME',
              'CA_CERTIFICATE',
            ] as const"
            :key="kind"
            :value="kind"
          >
            {{ t(`mailboxCredential.kinds.${kind}`) }}
          </option>
        </select></label
      >
      <p>{{ t("mailbox.credentialTotal", { count: credentialTotal }) }}</p>
      <button
        class="button"
        :disabled="locked || editor.uncertain"
        @click="loadCredentials()"
      >
        {{ t("vfs.refresh") }}
      </button>
      <button
        v-if="credentialCursor"
        class="button"
        :disabled="locked || editor.uncertain"
        @click="loadCredentials(true)"
      >
        {{ t("common.loadMore") }}
      </button>
    </details>
    <ul v-if="editor.diagnostics.length" class="mailbox-panel__diagnostics">
      <li v-for="(item, index) in editor.diagnostics" :key="index">
        <code
          >{{ item.code }} · {{ item.path }} · {{ item.line }}:{{
            item.column
          }}</code
        >
        {{ item.message }}
      </li>
    </ul>
    <CodeDiff
      v-if="
        editor.previewSource && editor.previewSource === editor.fingerprint()
      "
      :original="editor.view?.revision.content ?? ''"
      :modified="editor.previewYaml"
      :label="t('mailbox.diff')"
    />
    <p>{{ t("mailbox.authorityHelp") }}</p>
    <label v-if="editor.allowed('COPY')" class="field"
      ><span>{{ t("mailbox.copyName") }}</span
      ><input v-model="editor.copyName" :disabled="locked || editor.uncertain"
    /></label>
    <div class="mailbox-panel__actions">
      <button
        v-for="action in actions"
        :key="action"
        class="button"
        :disabled="!canExecute(action)"
        :title="reason(action)"
        @click="execute(action)"
      >
        {{ t(`mailbox.actions.${action}`) }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.mailbox-panel {
  display: grid;
  gap: 14px;
  min-width: 0;
  padding-block: 16px;
  border-top: 1px solid var(--border);
}
.mailbox-panel__metadata {
  display: grid;
  gap: 8px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.mailbox-panel__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.mailbox-panel__item {
  display: block;
  width: 100%;
  margin-block: 8px;
  text-align: start;
  overflow-wrap: anywhere;
}
.mailbox-panel__diagnostics {
  padding-inline-start: 20px;
  overflow-wrap: anywhere;
}
h3,
p {
  margin: 0;
}
</style>
