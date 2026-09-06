<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Search, Check, LogIn } from "@lucide/vue";
import { useI18n } from "vue-i18n";
import type {
  EmailEffectReceiptView,
  EmailReconciliationOutcome,
  IntegrationConnection,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import { useSessionStore } from "@/features/session/store";
import {
  EmailAttemptMismatch,
  prepareEmailAttempt,
  forgetEmailAttempt,
} from "../email-attempt";
import {
  decideEmailEffect,
  readEmailEffect,
  validReconciliationNote,
} from "../email-effects";
const props = defineProps<{
  connection: IntegrationConnection;
  initialInvocationRef?: string;
}>();
const session = useSessionStore();
const { t } = useI18n();
const invocationRef = ref(props.initialInvocationRef ?? "");
const now = ref(Date.now());
let clock: ReturnType<typeof setInterval> | undefined;
const attemptMismatch = ref(false);
const view = ref<EmailEffectReceiptView>();
const problem = ref<AppProblem>();
const busy = ref(false);
const note = ref("");
const outcome = ref<EmailReconciliationOutcome | "">("");
const confirming = ref(false);
let controller: AbortController | undefined;
let generation = 0;
const binding = computed(() =>
  view.value
    ? {
        receiptRef: view.value.receipt.ref,
        receiptVersion: view.value.receipt.version,
        receiptDigest: view.value.receipt.externalReceiptDigest,
      }
    : undefined,
);
const confirmed = computed(
  () =>
    !!binding.value &&
    session.hasPendingEmailConfirmation(binding.value, now.value),
);
const canDecide = computed(
  () =>
    !busy.value &&
    confirmed.value &&
    view.value?.receipt.outcome === "UNKNOWN_OUTCOME" &&
    !view.value.decision &&
    !!outcome.value &&
    validReconciliationNote(note.value),
);
function clear(): void {
  controller?.abort();
  generation++;
  busy.value = false;
  view.value = undefined;
  problem.value = undefined;
  attemptMismatch.value = false;
  note.value = "";
  outcome.value = "";
  confirming.value = false;
}
watch([() => props.connection.ref, invocationRef], clear);
onMounted(() => {
  clock = setInterval(() => {
    now.value = Date.now();
  }, 1000);
  if (props.initialInvocationRef) void load();
});
onBeforeUnmount(() => {
  clear();
  clearInterval(clock);
  session.finishEmailConfirmation();
});
async function authenticate(): Promise<void> {
  if (!binding.value || !view.value || busy.value) return;
  busy.value = true;
  problem.value = undefined;
  try {
    await session.beginEmailReconciliationReauth({
      ...binding.value,
      connectionRef: props.connection.ref,
      invocationRef: view.value.receipt.invocationRef,
    });
  } catch (error) {
    problem.value = asProblem(error);
    busy.value = false;
  }
}
async function load(): Promise<void> {
  if (busy.value || !invocationRef.value.trim()) return;
  controller?.abort();
  view.value = undefined;
  problem.value = undefined;
  const current = ++generation;
  const active = new AbortController();
  controller = active;
  busy.value = true;
  try {
    const value = await readEmailEffect(
      props.connection,
      invocationRef.value.trim(),
      active.signal,
    );
    if (current === generation) {
      view.value = value;
      if (value.decision) {
        forgetEmailAttempt(value.receipt.ref, window.sessionStorage);
        session.finishEmailConfirmation();
      }
    }
  } catch (error) {
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (current === generation) busy.value = false;
  }
}
async function reconcile(): Promise<void> {
  if (!canDecide.value || !view.value || !outcome.value) return;
  const snapshot = view.value;
  const selected = outcome.value;
  const current = ++generation;
  const active = new AbortController();
  controller?.abort();
  controller = active;
  busy.value = true;
  confirming.value = false;
  problem.value = undefined;
  attemptMismatch.value = false;
  // Неизвестный результат мутации требует повторного чтения, не повторной команды.
  view.value = undefined;
  let consumed = false;
  try {
    const key = await prepareEmailAttempt(
      snapshot.receipt,
      selected,
      note.value,
      window.sessionStorage,
    );
    if (current !== generation || active.signal.aborted) return;
    if (
      !session.consumePendingEmailConfirmation({
        receiptRef: snapshot.receipt.ref,
        receiptVersion: snapshot.receipt.version,
        receiptDigest: snapshot.receipt.externalReceiptDigest,
      })
    )
      throw new Error("Fresh email confirmation is unavailable");
    consumed = true;
    const decision = await decideEmailEffect(
      snapshot,
      selected,
      note.value,
      active.signal,
      key,
    );
    if (current === generation) {
      view.value = { receipt: snapshot.receipt, decision };
      note.value = "";
      outcome.value = "";
      forgetEmailAttempt(snapshot.receipt.ref, window.sessionStorage);
    }
  } catch (error) {
    if (error instanceof EmailAttemptMismatch && current === generation) {
      view.value = snapshot;
      attemptMismatch.value = true;
    }
    if (current === generation && !active.signal.aborted)
      problem.value = asProblem(error);
  } finally {
    if (consumed) session.finishEmailConfirmation();
    if (current === generation) busy.value = false;
  }
}
</script>
<template>
  <section class="email-effect" :aria-label="t('emailEffect.title')">
    <h3>{{ t("emailEffect.title") }}</h3>
    <form class="email-search" @submit.prevent="load">
      <label
        >{{ t("emailEffect.invocation")
        }}<input v-model="invocationRef" :disabled="busy" maxlength="512"
      /></label>
      <button
        type="submit"
        class="icon-button"
        :disabled="busy || !invocationRef.trim()"
        :title="t('emailEffect.search')"
        :aria-label="t('emailEffect.search')"
      >
        <Search :size="18" />
      </button>
    </form>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="attemptMismatch" role="alert">
      {{ t("emailEffect.attemptMismatch") }}
    </p>
    <template v-if="view">
      <dl>
        <dt>{{ t("emailEffect.receipt") }}</dt>
        <dd>
          <code>{{ view.receipt.ref }} / v{{ view.receipt.version }}</code>
        </dd>
        <dt>{{ t("emailEffect.outcome") }}</dt>
        <dd>{{ t(`emailEffect.${view.receipt.outcome}`) }}</dd>
        <dt>{{ t("emailEffect.digest") }}</dt>
        <dd>
          <code>{{ view.receipt.externalReceiptDigest }}</code>
        </dd>
        <dt>{{ t("emailEffect.configuration") }}</dt>
        <dd>{{ view.receipt.configurationRevision }}</dd>
        <dt>{{ t("emailEffect.created") }}</dt>
        <dd>{{ view.receipt.createdAt }}</dd>
      </dl>
      <template v-if="view.decision">
        <h4>{{ t("emailEffect.decision") }}</h4>
        <dl>
          <dt>{{ t("emailEffect.outcome") }}</dt>
          <dd>{{ t(`emailEffect.${view.decision.outcome}`) }}</dd>
          <dt>{{ t("emailEffect.decision") }}</dt>
          <dd>
            <code>{{ view.decision.ref }} / v{{ view.decision.version }}</code>
          </dd>
          <dt>{{ t("emailEffect.actor") }}</dt>
          <dd>
            <code>{{ view.decision.actorRef }}</code>
          </dd>
          <dt>{{ t("emailEffect.created") }}</dt>
          <dd>{{ view.decision.createdAt }}</dd>
          <dt>{{ t("emailEffect.expires") }}</dt>
          <dd>{{ view.decision.expiresAt }}</dd>
        </dl>
      </template>
      <template v-else-if="view.receipt.outcome === 'UNKNOWN_OUTCOME'">
        <button
          v-if="!confirmed"
          class="button"
          type="button"
          :disabled="busy"
          @click="authenticate"
        >
          <LogIn :size="18" />{{ t("emailEffect.authenticate") }}
        </button>
        <template v-else>
          <label
            >{{ t("emailEffect.decision")
            }}<select v-model="outcome" :disabled="busy">
              <option value="" disabled>
                {{ t("emailEffect.selectOutcome") }}
              </option>
              <option value="EFFECT_CONFIRMED">
                {{ t("emailEffect.EFFECT_CONFIRMED") }}
              </option>
              <option value="NO_EFFECT_CONFIRMED">
                {{ t("emailEffect.NO_EFFECT_CONFIRMED") }}
              </option>
            </select></label
          >
          <label
            >{{ t("emailEffect.note")
            }}<VoiceTextarea
              v-model="note"
              :disabled="busy"
              maxlength="4000"
              :aria-invalid="!validReconciliationNote(note) || undefined"
          /></label>
          <span>{{ [...note].length }} / 2000</span>
          <button
            type="button"
            class="button button--primary"
            :disabled="!canDecide"
            @click="confirming = true"
          >
            <Check :size="18" />{{ t("emailEffect.record") }}
          </button>
        </template>
      </template>
    </template>
    <ModalDialog
      v-if="confirming"
      :title="t('emailEffect.decision')"
      @close="confirming = false"
    >
      <p>{{ t(`emailEffect.${outcome}`) }}</p>
      <code>{{ view?.receipt.ref }} / v{{ view?.receipt.version }}</code>
      <template #actions
        ><button type="button" class="button" @click="confirming = false">
          {{ t("common.cancel") }}</button
        ><button
          type="button"
          class="button button--primary"
          :disabled="!canDecide"
          @click="reconcile"
        >
          {{ t("emailEffect.record") }}
        </button></template
      >
    </ModalDialog>
  </section>
</template>
<style scoped>
.email-effect {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.email-effect h3 {
  font-size: 16px;
}
.email-effect label {
  display: grid;
  gap: 6px;
  min-width: 0;
}
.email-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 40px;
  gap: 12px;
  align-items: end;
}
.email-effect input,
.email-effect select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.email-effect dl {
  display: grid;
  grid-template-columns: minmax(100px, 1fr) minmax(0, 2fr);
  gap: 8px;
  margin: 0;
}
.email-effect dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.email-effect code {
  overflow-wrap: anywhere;
}
@media (max-width: 600px) {
  .email-effect dl {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
