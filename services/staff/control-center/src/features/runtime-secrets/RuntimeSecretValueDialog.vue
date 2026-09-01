<script setup lang="ts">
import { Eye, EyeOff, KeyRound, RotateCw } from "@lucide/vue";
import { computed, onBeforeUnmount, ref, watch } from "vue";

import type { AppProblem } from "@/shared/api/problem";
import ModalDialog from "@/shared/ui/ModalDialog.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

import type {
  RuntimeSecret,
  RuntimeSecretCreateInput,
  RuntimeSecretRotateInput,
  RuntimeSecretValueType,
} from "./model";
import { validateSecretValue } from "./model";

const props = defineProps<{
  busy?: boolean;
  problem?: AppProblem;
  secret?: RuntimeSecret;
}>();
const emit = defineEmits<{
  close: [];
  create: [input: RuntimeSecretCreateInput];
  rotate: [input: RuntimeSecretRotateInput];
}>();

const name = ref("");
const description = ref("");
const valueType = ref<RuntimeSecretValueType>("STRING");
const value = ref("");
const showValue = ref(false);
const submitted = ref(false);
const rotating = computed(() => Boolean(props.secret));
const validation = computed(() =>
  validateSecretValue(valueType.value, value.value),
);

function clearPlaintext(): void {
  value.value = "";
  showValue.value = false;
}

function close(): void {
  if (props.busy) return;
  clearPlaintext();
  emit("close");
}

function submit(): void {
  submitted.value = true;
  if (validation.value || (!rotating.value && !name.value.trim())) return;
  if (rotating.value) {
    emit("rotate", { valueType: valueType.value, value: value.value });
    return;
  }
  emit("create", {
    name: name.value.trim(),
    description: description.value.trim(),
    valueType: valueType.value,
    value: value.value,
  });
}

watch(
  () => props.secret,
  (secret) => {
    clearPlaintext();
    submitted.value = false;
    name.value = secret?.name ?? "";
    description.value = secret?.description ?? "";
    valueType.value = secret?.valueType ?? "STRING";
  },
  { immediate: true },
);
onBeforeUnmount(clearPlaintext);
</script>

<template>
  <ModalDialog
    :title="
      rotating
        ? $t('runtimeSecrets.rotateTitle')
        : $t('runtimeSecrets.createTitle')
    "
    :busy="busy"
    size="lg"
    @close="close"
  >
    <div class="secret-form">
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <div v-if="secret" class="secret-form__target" role="note">
        <RotateCw :size="20" aria-hidden="true" />
        <div>
          <strong>{{ secret.name }}</strong>
          <p>{{ $t("runtimeSecrets.rotateHelp") }}</p>
        </div>
      </div>

      <label v-if="!rotating" class="field">
        <span>{{ $t("common.name") }}</span>
        <input
          v-model="name"
          maxlength="120"
          autocomplete="off"
          :aria-invalid="submitted && !name.trim()"
        />
        <small v-if="submitted && !name.trim()" class="field-error">
          {{ $t("runtimeSecrets.errors.nameRequired") }}
        </small>
      </label>

      <label v-if="!rotating" class="field">
        <span>{{ $t("common.description") }}</span>
        <textarea v-model="description" maxlength="1000" rows="3" />
      </label>

      <label v-if="!rotating" class="field">
        <span>{{ $t("runtimeSecrets.valueType") }}</span>
        <select v-model="valueType">
          <option value="STRING">
            {{ $t("runtimeSecrets.types.STRING") }}
          </option>
          <option value="JSON">{{ $t("runtimeSecrets.types.JSON") }}</option>
          <option value="BINARY">
            {{ $t("runtimeSecrets.types.BINARY") }}
          </option>
        </select>
      </label>

      <div class="field">
        <span>{{ $t("runtimeSecrets.value") }}</span>
        <div class="secret-form__value">
          <input
            v-model="value"
            :type="showValue ? 'text' : 'password'"
            maxlength="699052"
            autocomplete="new-password"
            autocapitalize="off"
            spellcheck="false"
            :aria-invalid="submitted && Boolean(validation)"
          />
          <button
            class="icon-button"
            type="button"
            :aria-label="
              showValue
                ? $t('runtimeSecrets.hideEnteredValue')
                : $t('runtimeSecrets.showEnteredValue')
            "
            @click="showValue = !showValue"
          >
            <EyeOff v-if="showValue" :size="18" aria-hidden="true" />
            <Eye v-else :size="18" aria-hidden="true" />
          </button>
        </div>
        <small class="field-hint">{{ $t("runtimeSecrets.valueHelp") }}</small>
        <small v-if="submitted && validation" class="field-error">
          {{ $t(`runtimeSecrets.errors.${validation}`) }}
        </small>
      </div>
    </div>
    <template #actions>
      <button class="button" type="button" :disabled="busy" @click="close">
        {{ $t("common.cancel") }}
      </button>
      <button
        class="button button--primary"
        type="button"
        :disabled="busy"
        @click="submit"
      >
        <RotateCw v-if="rotating" :size="16" aria-hidden="true" />
        <KeyRound v-else :size="16" aria-hidden="true" />
        {{
          rotating ? $t("runtimeSecrets.rotate") : $t("runtimeSecrets.create")
        }}
      </button>
    </template>
  </ModalDialog>
</template>

<style scoped>
.secret-form {
  display: grid;
  gap: 18px;
}
.secret-form__target {
  display: grid;
  grid-template-columns: 24px minmax(0, 1fr);
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--panel);
}
.secret-form__target p {
  margin: 4px 0 0;
  color: var(--text-secondary);
}
.secret-form__value {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 42px;
  gap: 8px;
}
.secret-form__value input {
  min-width: 0;
  font-family: var(--font-mono);
}
.field-hint {
  color: var(--text-secondary);
}
</style>
