<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef } from "vue";
import { useI18n } from "vue-i18n";
import { idempotencyKey } from "@/shared/api/mutation";
import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { useUnsavedChanges } from "@/shared/ui/unsaved-changes";
import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
} from "./model";
import RuntimeSecretValueDialog from "./RuntimeSecretValueDialog.vue";
import RuntimeSecretDraftImpact from "./RuntimeSecretDraftImpact.vue";
import { readRuntimeSecret } from "./api";
import {
  createSecretDraft,
  saveSecretDraft,
  readSecretDraft,
  changeSecretDraft,
  safeDraftProblem,
  type RuntimeSecretDraft,
} from "./draft-api";

const props = defineProps<{
  projectRef: string;
  secret?: RuntimeSecret;
  initialDraftRef?: string;
  initialPlanRef?: string;
}>();
const emit = defineEmits<{
  close: [];
  saved: [draft: RuntimeSecretDraft];
  published: [secret: RuntimeSecret];
  planPrepared: [draftRef: string, planRef: string];
}>();
function published(value: RuntimeSecretDraft, secret: RuntimeSecret): void {
  draft.value = value;
  emit("saved", value);
  emit("published", secret);
}
const { t } = useI18n();
function prepared(planRef: string): void {
  if (draft.value) emit("planPrepared", draft.value.ref, planRef);
}
const draft = shallowRef<RuntimeSecretDraft>();
const busy = ref(false);
const impactBusy = ref(false);
const locked = computed(() => busy.value || impactBusy.value);
const restoring = ref(Boolean(props.initialDraftRef));
const uncertain = ref(false);
const problem = shallowRef<AppProblem>();
let disposed = false;
const isActive = () => !disposed;
let controller: AbortController | undefined;
// Значение живёт только внутри открытой формы до подтверждения сохранения.
let pendingSave:
  | {
      key: string;
      input: RuntimeSecretCreateInput | RuntimeSecretRotateInput;
      secret?: { ref: string; version: number };
    }
  | undefined;
let pendingChange:
  | { key: string; draft: RuntimeSecretDraft; action: "validate" | "discard" }
  | undefined;

useUnsavedChanges(
  computed(() => locked.value || uncertain.value),
  () => t("runtimeSecrets.draft.abandon"),
);

function close(): void {
  if (locked.value) return;
  if (uncertain.value && !window.confirm(t("runtimeSecrets.draft.abandon")))
    return;
  emit("close");
}

async function save(
  input: RuntimeSecretCreateInput | RuntimeSecretRotateInput,
): Promise<void> {
  if (locked.value || draft.value) return;
  const retrying = Boolean(pendingSave);
  pendingSave ??= {
    key: idempotencyKey(),
    input: { ...input },
    ...(props.secret
      ? { secret: { ref: props.secret.ref, version: props.secret.version } }
      : {}),
  };
  const request = pendingSave;
  busy.value = true;
  problem.value = undefined;
  try {
    const result = request.secret
      ? await saveSecretDraft(
          props.projectRef,
          request.secret,
          request.input,
          request.key,
        )
      : await createSecretDraft(
          props.projectRef,
          request.input as RuntimeSecretCreateInput,
          request.key,
        );
    request.input.value = "";
    pendingSave = undefined;
    if (disposed) return;
    draft.value = result;
    uncertain.value = false;
    emit("saved", result);
  } catch (error) {
    if (!disposed) {
      problem.value = safeDraftProblem(error);
      const rejected =
        !retrying &&
        [400, 401, 403, 404, 412, 422].includes(problem.value.status);
      if (rejected) {
        request.input.value = "";
        pendingSave = undefined;
      }
      uncertain.value = !rejected;
    }
  } finally {
    if (!disposed) busy.value = false;
  }
}

async function refresh(): Promise<void> {
  if (locked.value || !draft.value) return;
  busy.value = true;
  problem.value = undefined;
  controller?.abort();
  controller = new AbortController();
  try {
    const result = await readSecretDraft(
      props.projectRef,
      draft.value.ref,
      controller.signal,
    );
    if (!disposed) draft.value = result;
  } catch (error) {
    if (!disposed) problem.value = safeDraftProblem(error);
  } finally {
    if (!disposed) busy.value = false;
  }
}

async function change(action: "validate" | "discard"): Promise<void> {
  if (
    locked.value ||
    !draft.value ||
    (pendingChange && pendingChange.action !== action)
  )
    return;
  busy.value = true;
  problem.value = undefined;
  const retrying = Boolean(pendingChange);
  try {
    if (!pendingChange) {
      controller?.abort();
      controller = new AbortController();
      const current = await readSecretDraft(
        props.projectRef,
        draft.value.ref,
        controller.signal,
      );
      if (disposed) return;
      draft.value = current;
      pendingChange = { key: idempotencyKey(), draft: current, action };
    }
    const result = await changeSecretDraft(
      pendingChange.draft,
      action,
      pendingChange.key,
    );
    if (disposed) return;
    pendingChange = undefined;
    uncertain.value = false;
    draft.value = result;
    emit("saved", result);
  } catch (error) {
    if (!disposed) {
      problem.value = safeDraftProblem(error);
      if (
        !retrying &&
        [400, 401, 403, 404, 412, 422].includes(problem.value.status)
      )
        pendingChange = undefined;
      uncertain.value = Boolean(pendingChange);
    }
  } finally {
    if (!disposed) busy.value = false;
  }
}

async function restore(): Promise<void> {
  if (!props.initialDraftRef || locked.value) return;
  busy.value = true;
  problem.value = undefined;
  controller?.abort();
  controller = new AbortController();
  try {
    const result = await readSecretDraft(
      props.projectRef,
      props.initialDraftRef,
      controller.signal,
    );
    if (!disposed) {
      draft.value = result;
      restoring.value = false;
      if (result.state === "PUBLISHED") {
        const secret = await readRuntimeSecret(
          result.secretRef,
          props.projectRef,
          controller.signal,
        );
        if (isActive()) emit("published", secret);
      }
    }
  } catch (error) {
    if (!disposed) problem.value = safeDraftProblem(error);
  } finally {
    if (!disposed) busy.value = false;
  }
}
onMounted(() => void restore());
onBeforeUnmount(() => {
  disposed = true;
  controller?.abort();
  if (pendingSave) pendingSave.input.value = "";
  pendingSave = undefined;
  pendingChange = undefined;
});
</script>

<template>
  <RuntimeSecretValueDialog
    v-if="!draft && !restoring"
    :secret="secret"
    :busy="locked"
    :locked="uncertain"
    :problem="problem"
    :submit-label="
      t(uncertain ? 'runtimeSecrets.draft.retry' : 'runtimeSecrets.draft.save')
    "
    @create="save"
    @rotate="save"
    @close="close"
  >
    <p role="note">
      {{
        t(
          uncertain
            ? "runtimeSecrets.draft.unknown"
            : "runtimeSecrets.draft.help",
        )
      }}
    </p>
  </RuntimeSecretValueDialog>
  <ModalDialog
    v-else-if="restoring"
    :title="t('runtimeSecrets.draft.title')"
    :busy="locked"
    @close="close"
  >
    <ProblemNotice v-if="problem" :problem="problem" @retry="restore" />
    <p v-else>{{ t("common.loading") }}</p>
  </ModalDialog>
  <ModalDialog
    v-else-if="draft"
    :title="t('runtimeSecrets.draft.title')"
    :busy="locked"
    @close="close"
  >
    <div class="secret-draft">
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <p v-if="uncertain" role="status">
        {{ t("runtimeSecrets.draft.unknown") }}
      </p>
      <strong>{{ draft.name }}</strong>
      <dl>
        <dt>{{ t("runtimeSecrets.draft.state") }}</dt>
        <dd>{{ draft.state }}</dd>
        <dt>{{ t("runtimeSecrets.draft.reference") }}</dt>
        <dd>{{ draft.ref }}</dd>
        <dt>{{ t("runtimeSecrets.revision") }}</dt>
        <dd>{{ draft.version }}</dd>
        <dt>{{ t("runtimeSecrets.draft.expires") }}</dt>
        <dd>{{ draft.expiresAt }}</dd>
      </dl>
      <p>{{ t("runtimeSecrets.draft.savedHelp") }}</p>
      <RuntimeSecretDraftImpact
        v-if="['VALID', 'PUBLISHED'].includes(draft.state)"
        :draft="draft"
        :initial-plan-ref="initialPlanRef"
        @prepared="prepared"
        @working="impactBusy = $event"
        @uncertain="uncertain = $event"
        @published="published"
      />
    </div>
    <template #actions>
      <button class="button" :disabled="locked" @click="refresh">
        {{ t("common.refresh") }}
      </button>
      <button
        v-if="['DRAFT', 'VALID'].includes(draft.state)"
        class="button"
        :disabled="locked || (uncertain && pendingChange?.action !== 'discard')"
        @click="change('discard')"
      >
        {{ t("runtimeSecrets.draft.discard") }}
      </button>
      <button
        v-if="draft.state === 'DRAFT' || pendingChange?.action === 'validate'"
        class="button button--primary"
        :disabled="
          locked || (uncertain && pendingChange?.action !== 'validate')
        "
        @click="change('validate')"
      >
        {{ t("runtimeSecrets.draft.validate") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.secret-draft {
  display: grid;
  gap: 12px;
  min-width: 0;
  overflow-wrap: anywhere;
}
.secret-draft dl {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 8px 16px;
}
.secret-draft dd {
  margin: 0;
}
</style>
