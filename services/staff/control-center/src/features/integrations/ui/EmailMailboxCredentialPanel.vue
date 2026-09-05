<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { KeyRound, Save } from "@lucide/vue";
import type {
  EmailMailboxCredential,
  EmailMailboxCredentialKind,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import {
  MailboxCredentialMismatch,
  mailboxCredentialLimits,
  prepareMailboxCredential,
  saveMailboxCredential,
  recoverMailboxCredential,
  validMailboxCredential,
  type MailboxCredentialAttempt,
} from "../email-credentials";
import {
  pendingMailboxCredential,
  rememberMailboxCredential,
  forgetMailboxCredential,
} from "../email-credential-recovery";

const props = defineProps<{
  connection: IntegrationConnection;
  disabled?: boolean;
}>();
const emit = defineEmits<{ saved: []; busy: [value: boolean] }>();
const kind = ref<EmailMailboxCredentialKind>("AUTH_SECRET");
const value = ref("");
const busy = ref(false);
const mismatch = ref(false);
const pending = ref<MailboxCredentialAttempt>();
const receipt = ref<EmailMailboxCredential>();
const problem = ref<AppProblem>();
let controller: AbortController | undefined;
let generation = 0;
const allowed = computed(
  () =>
    props.connection.definitionKey === "email" &&
    props.connection.nextActions.includes("CONFIGURE_CREDENTIAL"),
);
const canSave = computed(
  () =>
    allowed.value &&
    !busy.value &&
    !props.disabled &&
    validMailboxCredential(kind.value, value.value) &&
    (!receipt.value ||
      props.connection.version >= receipt.value.connectionVersion),
);

function clear(): void {
  controller?.abort();
  generation++;
  value.value = "";
  pending.value = undefined;
  receipt.value = undefined;
  problem.value = undefined;
  mismatch.value = false;
  busy.value = false;
  emit("busy", false);
}
function restore(): void {
  clear();
  try {
    const attempt = pendingMailboxCredential(
      props.connection.ref,
      window.sessionStorage,
    );
    if (attempt) {
      pending.value = attempt;
      kind.value = attempt.kind;
    }
  } catch (error) {
    problem.value = asProblem(error);
  }
}
watch(() => props.connection.ref, restore);
onMounted(restore);
watch(
  allowed,
  (enabled) => {
    if (!enabled) restore();
  },
  { flush: "sync" },
);
watch(kind, () => {
  value.value = "";
  mismatch.value = false;
});
onBeforeUnmount(clear);

async function save(): Promise<void> {
  if (!canSave.value) return;
  const current = ++generation;
  const active = new AbortController();
  const previousAttempt = pending.value;
  controller = active;
  let secret = value.value;
  value.value = "";
  busy.value = true;
  emit("busy", true);
  problem.value = undefined;
  mismatch.value = false;
  receipt.value = undefined;
  try {
    const attempt = await prepareMailboxCredential(
      props.connection,
      kind.value,
      secret,
      previousAttempt,
    );
    if (current !== generation || active.signal.aborted) return;
    rememberMailboxCredential(attempt, window.sessionStorage);
    pending.value = attempt;
    const result = await saveMailboxCredential(attempt, secret, active.signal);
    if (current !== generation) return;
    forgetMailboxCredential(attempt, window.sessionStorage);
    receipt.value = result;
    pending.value = undefined;
    emit("saved");
  } catch (error) {
    if (current === generation && !active.signal.aborted) {
      if (error instanceof MailboxCredentialMismatch) mismatch.value = true;
      else {
        const failure = asProblem(error);
        problem.value = new AppProblem({
          status: failure.status,
          code: failure.code,
          kind: failure.kind,
          retryable: false,
          correlationId: failure.correlationId,
        });
        if (!previousAttempt && [400, 413, 422].includes(failure.status)) {
          if (pending.value)
            forgetMailboxCredential(pending.value, window.sessionStorage);
          pending.value = undefined;
        }
      }
    }
  } finally {
    secret = "";
    if (current === generation) {
      busy.value = false;
      emit("busy", false);
    }
  }
}

async function recover(): Promise<void> {
  if (busy.value || props.disabled || !pending.value) return;
  const attempt = pending.value;
  const current = ++generation;
  controller?.abort();
  const active = new AbortController();
  controller = active;
  busy.value = true;
  emit("busy", true);
  problem.value = undefined;
  try {
    const result = await recoverMailboxCredential(attempt, active.signal);
    if (current !== generation) return;
    forgetMailboxCredential(attempt, window.sessionStorage);
    receipt.value = result;
    pending.value = undefined;
    value.value = "";
    emit("saved");
  } catch (error) {
    if (current === generation && !active.signal.aborted) {
      const failure = asProblem(error);
      problem.value = new AppProblem({
        status: failure.status,
        code: failure.code,
        kind: failure.kind,
        retryable: false,
      });
    }
  } finally {
    if (current === generation) {
      busy.value = false;
      emit("busy", false);
    }
  }
}
</script>

<template>
  <section
    class="mailbox-credential"
    :aria-label="$t('mailboxCredential.title')"
  >
    <h3><KeyRound :size="18" />{{ $t("mailboxCredential.title") }}</h3>
    <form
      v-if="allowed"
      class="form-grid"
      autocomplete="off"
      @submit.prevent="save"
    >
      <label class="field">
        <span>{{ $t("mailboxCredential.kind") }}</span>
        <select v-model="kind" :disabled="busy || disabled || !!pending">
          <option
            v-for="(_, entry) in mailboxCredentialLimits"
            :key="entry"
            :value="entry"
          >
            {{ $t(`mailboxCredential.kinds.${entry}`) }}
          </option>
        </select>
      </label>
      <label class="field field--wide">
        <span>{{ $t("mailboxCredential.value") }}</span>
        <textarea
          v-if="kind === 'CA_CERTIFICATE'"
          v-model="value"
          :disabled="busy || disabled"
          rows="6"
          autocomplete="off"
          autocapitalize="off"
          :spellcheck="false"
          data-sensitive="true"
        />
        <input
          v-else
          v-model="value"
          type="password"
          :disabled="busy || disabled"
          autocomplete="new-password"
          autocapitalize="off"
          :spellcheck="false"
        />
      </label>
      <button class="button button--primary" type="submit" :disabled="!canSave">
        <Save :size="16" />{{
          $t(pending ? "mailboxCredential.retry" : "common.save")
        }}
      </button>
    </form>
    <p v-else>{{ $t("mailboxCredential.unavailable") }}</p>
    <p v-if="mismatch" role="alert">{{ $t("mailboxCredential.mismatch") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" compact />
    <p v-if="pending" role="status">{{ $t("mailboxCredential.pending") }}</p>
    <button
      v-if="pending"
      class="button"
      :disabled="busy || disabled"
      @click="recover"
    >
      {{ $t("mailboxCredential.recover") }}
    </button>
    <dl v-if="receipt" class="mailbox-credential__receipt" aria-live="polite">
      <dt>{{ $t("mailboxCredential.name") }}</dt>
      <dd>
        <code>{{ receipt.name }}</code>
      </dd>
      <dt>{{ $t("mailboxCredential.generation") }}</dt>
      <dd>{{ receipt.generation }}</dd>
      <dt>
        {{
          $t("identity.connectionVersion", {
            version: receipt.connectionVersion,
          })
        }}
      </dt>
      <dd>{{ $t("mailboxCredential.saved") }}</dd>
    </dl>
  </section>
</template>

<style scoped>
.mailbox-credential {
  min-width: 0;
  padding-block: 16px;
  border-block: 1px solid var(--border);
}
.mailbox-credential h3 {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 1rem;
}
.mailbox-credential textarea {
  width: 100%;
  min-width: 0;
  resize: vertical;
  font-family: var(--font-mono);
}
.mailbox-credential__receipt {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 8px;
  overflow-wrap: anywhere;
}
.mailbox-credential__receipt dd {
  margin: 0;
}
</style>
