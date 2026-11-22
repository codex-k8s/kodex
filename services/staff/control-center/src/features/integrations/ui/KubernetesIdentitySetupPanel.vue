<script setup lang="ts">
import { onBeforeUnmount, onMounted, toRef, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { KubernetesSetupContext } from "../kubernetes-identity-setup-api";
import {
  kubernetesIdentityInspectCommand,
  kubernetesIdentityProtectCommand,
  kubernetesIdentityUploadCommand,
  useKubernetesIdentitySetup,
} from "../kubernetes-identity-setup";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";

const props = withDefaults(
  defineProps<{ context: KubernetesSetupContext; disabled?: boolean }>(),
  { disabled: false },
);
const emit = defineEmits<{ changed: [] }>();
const { t } = useI18n();
const prefix = useId();
const model = useKubernetesIdentitySetup(
  toRef(props, "context"),
  toRef(props, "disabled"),
);
const {
  requirements,
  exportCommand,
  contentSHA256,
  contentBytes,
  selectorKind,
  selector,
  setup,
  busy,
  problem,
  uncertain,
  hasAdmission,
  usable,
  canBegin,
  canCancel,
  requiresConnectionRefresh,
} = model;
onMounted(() => window.addEventListener("pagehide", model.suspend));
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>

<template>
  <section
    class="kubernetes-identity-setup"
    :aria-labelledby="`${prefix}-title`"
  >
    <h4 :id="`${prefix}-title`">
      {{
        context.policy === "PROJECT_SHARED"
          ? t("kubernetesIdentitySetup.sharedTitle")
          : t("kubernetesIdentitySetup.title")
      }}
    </h4>
    <p>{{ t("kubernetesIdentitySetup.description") }}</p>
    <p>{{ t("kubernetesIdentitySetup.identityRequirement") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="alert">
      {{ t("kubernetesIdentitySetup.unknown") }}
    </p>
    <template v-if="requirements">
      <p>{{ t("kubernetesIdentitySetup.export") }}</p>
      <p>
        {{
          t("kubernetesIdentitySetup.exportContext", {
            value: requirements.canonicalContext,
          })
        }}
      </p>
      <pre><code>{{ exportCommand }}</code></pre>
    </template>
    <p v-else role="alert">
      {{ t("kubernetesIdentitySetup.requirementsUnavailable") }}
    </p>
    <form @submit.prevent="model.begin()">
      <p>{{ t("kubernetesIdentitySetup.inspect") }}</p>
      <pre><code>{{ kubernetesIdentityInspectCommand }}</code></pre>
      <label :for="`${prefix}-digest`">{{
        t("kubernetesIdentitySetup.digest")
      }}</label>
      <input
        :id="`${prefix}-digest`"
        v-model="contentSHA256"
        type="text"
        maxlength="64"
        spellcheck="false"
        autocomplete="off"
        :disabled="busy || !usable || !requirements"
      />
      <label :for="`${prefix}-bytes`">{{
        t("kubernetesIdentitySetup.bytes")
      }}</label>
      <input
        :id="`${prefix}-bytes`"
        v-model="contentBytes"
        type="text"
        inputmode="numeric"
        maxlength="7"
        autocomplete="off"
        :disabled="busy || !usable || !requirements"
      />
      <button class="button" type="submit" :disabled="!canBegin">
        {{ t("kubernetesIdentitySetup.begin") }}
      </button>
    </form>
    <template v-if="hasAdmission">
      <p>{{ t("kubernetesIdentitySetup.admission") }}</p>
      <button
        class="button"
        type="button"
        :disabled="busy || !usable"
        @click="model.download()"
      >
        {{ t("kubernetesIdentitySetup.download") }}
      </button>
      <p>{{ t("kubernetesIdentitySetup.protect") }}</p>
      <pre><code>{{ kubernetesIdentityProtectCommand }}</code></pre>
      <p>{{ t("kubernetesIdentitySetup.upload") }}</p>
      <pre><code>{{ kubernetesIdentityUploadCommand }}</code></pre>
    </template>
    <template v-if="setup">
      <p role="status">
        {{ t(`kubernetesIdentitySetup.states.${setup.state}`) }}
      </p>
      <p>
        {{
          t("kubernetesIdentitySetup.identity", {
            ref: setup.identityRef,
            revision: setup.identityRevision,
          })
        }}
      </p>
      <p>
        {{ t("kubernetesIdentitySetup.expiry", { value: setup.expiresAt }) }}
      </p>
      <p v-if="setup.reasonCode" role="alert">
        {{ t(`kubernetesIdentitySetup.reasons.${setup.reasonCode}`) }}
      </p>
      <button
        v-if="canCancel"
        class="button"
        type="button"
        @click="model.cancel()"
      >
        {{ t("kubernetesIdentitySetup.cancel") }}
      </button>
      <template v-if="requiresConnectionRefresh">
        <p v-if="setup.state === 'COMPLETED'">
          {{ t("kubernetesIdentitySetup.testRequired") }}
        </p>
        <button
          class="button"
          type="button"
          :disabled="busy || !usable"
          @click="emit('changed')"
        >
          {{ t("kubernetesIdentitySetup.reload") }}
        </button>
      </template>
    </template>
    <form @submit.prevent="model.refresh()">
      <p>{{ t("kubernetesIdentitySetup.recovery") }}</p>
      <label :for="`${prefix}-selector-kind`">{{
        t("kubernetesIdentitySetup.selector")
      }}</label>
      <select
        :id="`${prefix}-selector-kind`"
        v-model="selectorKind"
        :disabled="busy || !usable || uncertain"
      >
        <option value="SETUP">
          {{ t("kubernetesIdentitySetup.setupRef") }}
        </option>
        <option value="BEGIN_KEY">
          {{ t("kubernetesIdentitySetup.beginKey") }}
        </option>
      </select>
      <label :for="`${prefix}-selector`">{{
        t("kubernetesIdentitySetup.key")
      }}</label>
      <input
        :id="`${prefix}-selector`"
        v-model="selector"
        type="text"
        maxlength="128"
        autocomplete="off"
        :disabled="busy || !usable || uncertain"
      />
      <button
        class="button"
        type="submit"
        :disabled="busy || !usable || !selector"
      >
        {{ t("kubernetesIdentitySetup.refresh") }}
      </button>
    </form>
  </section>
</template>

<style scoped>
.kubernetes-identity-setup {
  display: grid;
  gap: 0.75rem;
  border-top: 1px solid var(--border);
  padding-top: 1rem;
}
form {
  display: grid;
  gap: 0.5rem;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
input,
select {
  max-width: 100%;
}
</style>
