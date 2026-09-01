<script setup lang="ts">
import { Eye, EyeOff, LogIn, ShieldAlert } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";

import { revealRuntimeSecret } from "./api";
import type { RuntimeSecret, RuntimeSecretValueType } from "./model";
import { canRuntimeSecretAction, maskedSecretHint } from "./model";
import { executeRuntimeSecretReveal } from "./reveal-flow";
import { useSessionStore } from "@/features/session/store";
import type { AppProblem } from "@/shared/api/problem";
import { asProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const plaintextLifetimeMs = 60_000;

const props = defineProps<{ secret: RuntimeSecret }>();
const emit = defineEmits<{ close: [] }>();
const session = useSessionStore();
const busy = ref(false);
const problem = ref<AppProblem>();
const value = ref("");
const valueType = ref<RuntimeSecretValueType>();
const revealed = ref(false);
let clearTimer: ReturnType<typeof setTimeout> | undefined;
const revealPending = computed(() =>
  session.hasPendingRuntimeSecretReveal(
    props.secret.projectRef,
    props.secret.ref,
  ),
);
const canReveal = computed(() =>
  canRuntimeSecretAction(props.secret, "REVEAL"),
);

function clearPlaintext(): void {
  if (clearTimer) clearTimeout(clearTimer);
  clearTimer = undefined;
  value.value = "";
  valueType.value = undefined;
  revealed.value = false;
}

function close(): void {
  if (busy.value) return;
  clearPlaintext();
  emit("close");
}

async function reveal(): Promise<void> {
  if (busy.value || !canReveal.value) return;
  clearPlaintext();
  problem.value = undefined;
  busy.value = true;
  try {
    const flow = await executeRuntimeSecretReveal({
      projectRef: props.secret.projectRef,
      secretRef: props.secret.ref,
      session,
      reveal: revealRuntimeSecret,
    });
    if (flow.kind === "reauthentication-started") return;
    const result = flow.value;
    value.value = result.value;
    valueType.value = result.valueType;
    result.value = "";
    revealed.value = true;
    clearTimer = setTimeout(clearPlaintext, plaintextLifetimeMs);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

watch(
  () => props.secret.ref,
  () => {
    problem.value = undefined;
    clearPlaintext();
  },
);
onBeforeUnmount(clearPlaintext);
</script>

<template>
  <ModalDialog
    :title="$t('runtimeSecrets.revealTitle')"
    :busy="busy"
    size="lg"
    @close="close"
  >
    <div class="reveal-dialog">
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <div class="reveal-dialog__warning" role="alert">
        <ShieldAlert :size="24" aria-hidden="true" />
        <div>
          <strong>{{ $t("runtimeSecrets.reauthRequired") }}</strong>
          <p>{{ $t("runtimeSecrets.reauthHelp") }}</p>
        </div>
      </div>
      <dl class="reveal-dialog__metadata">
        <div>
          <dt>{{ $t("common.name") }}</dt>
          <dd>{{ secret.name }}</dd>
        </div>
        <div>
          <dt>{{ $t("runtimeSecrets.maskedHint") }}</dt>
          <dd>
            <code>{{ maskedSecretHint(secret) }}</code>
          </dd>
        </div>
      </dl>
      <p v-if="!revealed" class="reveal-dialog__authorization" role="status">
        {{
          revealPending
            ? $t("runtimeSecrets.reauthCompleted")
            : $t("runtimeSecrets.reauthRedirectHelp")
        }}
      </p>
      <div v-else class="field reveal-dialog__value">
        <span>{{ $t("runtimeSecrets.revealedValue") }}</span>
        <textarea
          :value="value"
          readonly
          rows="8"
          autocomplete="off"
          spellcheck="false"
        />
        <small v-if="valueType">{{
          $t(`runtimeSecrets.types.${valueType}`)
        }}</small>
        <small>{{ $t("runtimeSecrets.revealEphemeral") }}</small>
      </div>
    </div>
    <template #actions>
      <button class="button" type="button" :disabled="busy" @click="close">
        {{ $t("common.close") }}
      </button>
      <button
        v-if="!revealed"
        class="button button--danger"
        type="button"
        :disabled="busy || !canReveal"
        @click="reveal"
      >
        <Eye v-if="revealPending" :size="16" aria-hidden="true" />
        <LogIn v-else :size="16" aria-hidden="true" />
        {{
          revealPending
            ? $t("runtimeSecrets.reveal")
            : $t("runtimeSecrets.reauthenticate")
        }}
      </button>
      <button v-else class="button" type="button" @click="clearPlaintext">
        <EyeOff :size="16" aria-hidden="true" />
        {{ $t("runtimeSecrets.hideValue") }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.reveal-dialog {
  display: grid;
  gap: 18px;
}
.reveal-dialog__warning {
  display: grid;
  grid-template-columns: 28px minmax(0, 1fr);
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--warning);
  border-radius: 6px;
  background: var(--warning-soft);
}
.reveal-dialog__warning p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.reveal-dialog__metadata {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
}
.reveal-dialog__metadata > div {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
}
.reveal-dialog__metadata dt,
.reveal-dialog__value small {
  color: var(--text-secondary);
}
.reveal-dialog__metadata dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.reveal-dialog__authorization {
  margin: 0;
  color: var(--text-secondary);
}
.reveal-dialog__value textarea {
  font-family: var(--font-mono);
  white-space: pre;
}
@media (max-width: 620px) {
  .reveal-dialog__metadata {
    grid-template-columns: 1fr;
  }
}
</style>
