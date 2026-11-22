<script setup lang="ts">
import { onBeforeUnmount, onMounted, toRef, useId } from "vue";
import { useI18n } from "vue-i18n";
import type { KubernetesMaterialContext } from "../kubernetes-resource-material-api";
import {
  kubernetesMaterialInspectCommand,
  kubernetesMaterialProtectCommand,
  kubernetesMaterialUploadCommand,
  useKubernetesResourceMaterial,
} from "../kubernetes-resource-material";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
const props = withDefaults(
  defineProps<{ context: KubernetesMaterialContext; disabled?: boolean }>(),
  { disabled: false },
);
const { t } = useI18n(),
  prefix = useId();
const model = useKubernetesResourceMaterial(
  toRef(props, "context"),
  toRef(props, "disabled"),
);
const {
  destination,
  contentSHA256,
  contentBytes,
  selectorKind,
  selector,
  material,
  busy,
  problem,
  uncertain,
  hasAdmission,
  usable,
  canBegin,
  canCancel,
} = model;
onMounted(() => window.addEventListener("pagehide", model.suspend));
onBeforeUnmount(() => window.removeEventListener("pagehide", model.suspend));
</script>
<template>
  <section
    class="kubernetes-resource-material"
    :aria-labelledby="`${prefix}-title`"
  >
    <h4 :id="`${prefix}-title`">{{ t("kubernetesResourceMaterial.title") }}</h4>
    <p>{{ t("kubernetesResourceMaterial.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="alert">
      {{ t("kubernetesResourceMaterial.unknown") }}
    </p>
    <form @submit.prevent="model.begin()">
      <fieldset :disabled="busy || !usable || uncertain">
        <legend>{{ t("kubernetesResourceMaterial.destination") }}</legend>
        <label :for="`${prefix}-operation`">{{
          t("kubernetesResourceMaterial.operation")
        }}</label>
        <select :id="`${prefix}-operation`" v-model="destination.operation">
          <option value="kubernetes.create">create</option>
          <option value="kubernetes.replace">replace</option>
          <option value="kubernetes.apply">apply</option>
          <option value="kubernetes.edit">edit</option>
          <option value="kubernetes.patch">patch</option>
        </select>
        <label :for="`${prefix}-namespace`">{{
          t("kubernetesResourceMaterial.namespace")
        }}</label>
        <input
          :id="`${prefix}-namespace`"
          v-model="destination.namespace"
          maxlength="63"
          autocomplete="off"
        />
        <label :for="`${prefix}-name`">{{
          t("kubernetesResourceMaterial.name")
        }}</label>
        <input
          :id="`${prefix}-name`"
          v-model="destination.name"
          maxlength="253"
          autocomplete="off"
        />
        <label :for="`${prefix}-generate`">{{
          t("kubernetesResourceMaterial.generateName")
        }}</label>
        <input
          :id="`${prefix}-generate`"
          v-model="destination.generateName"
          maxlength="253"
          autocomplete="off"
          :disabled="destination.operation !== 'kubernetes.create'"
        />
        <label :for="`${prefix}-type`">{{
          t("kubernetesResourceMaterial.secretType")
        }}</label>
        <input
          :id="`${prefix}-type`"
          v-model="destination.secretType"
          maxlength="253"
          autocomplete="off"
        />
        <label :for="`${prefix}-patch`">{{
          t("kubernetesResourceMaterial.patchType")
        }}</label>
        <select
          :id="`${prefix}-patch`"
          v-model="destination.patchType"
          :disabled="destination.operation !== 'kubernetes.patch'"
        >
          <option value="">
            {{ t("kubernetesResourceMaterial.notPatch") }}
          </option>
          <option value="JSON">JSON</option>
          <option value="MERGE">MERGE</option>
          <option value="STRATEGIC">STRATEGIC</option>
        </select>
        <p>{{ t("kubernetesResourceMaterial.inspect") }}</p>
        <pre><code>{{ kubernetesMaterialInspectCommand }}</code></pre>
        <p>{{ t("kubernetesResourceMaterial.patchInstructions") }}</p>
        <label :for="`${prefix}-digest`">{{
          t("kubernetesResourceMaterial.digest")
        }}</label>
        <input
          :id="`${prefix}-digest`"
          v-model="contentSHA256"
          maxlength="64"
          spellcheck="false"
          autocomplete="off"
        />
        <label :for="`${prefix}-bytes`">{{
          t("kubernetesResourceMaterial.bytes")
        }}</label>
        <input
          :id="`${prefix}-bytes`"
          v-model="contentBytes"
          inputmode="numeric"
          maxlength="7"
          autocomplete="off"
        />
      </fieldset>
      <button type="submit" class="button" :disabled="!canBegin">
        {{ t("kubernetesResourceMaterial.begin") }}
      </button>
    </form>
    <template v-if="hasAdmission">
      <p>{{ t("kubernetesResourceMaterial.protect") }}</p>
      <button
        type="button"
        class="button"
        :disabled="busy || !usable"
        @click="model.download()"
      >
        {{ t("kubernetesResourceMaterial.download") }}
      </button>
      <pre><code>{{ kubernetesMaterialProtectCommand }}</code></pre>
      <pre><code>{{ kubernetesMaterialUploadCommand }}</code></pre>
    </template>
    <template v-if="material">
      <p role="status">
        {{ t(`kubernetesResourceMaterial.states.${material.state}`) }}
      </p>
      <p>
        {{
          t("kubernetesResourceMaterial.reference", {
            ref: material.materialRef,
          })
        }}
      </p>
      <p>
        {{ material.operation }} · {{ material.namespace }} /
        {{ material.name || material.generateName }} · {{ material.secretType }}
      </p>
      <p>
        {{
          t("kubernetesResourceMaterial.expiry", { value: material.expiresAt })
        }}
      </p>
      <p v-if="material.reasonCode" role="alert">
        {{ t(`kubernetesResourceMaterial.reasons.${material.reasonCode}`) }}
      </p>
      <button
        v-if="canCancel"
        type="button"
        class="button"
        @click="model.cancel()"
      >
        {{ t("kubernetesResourceMaterial.cancel") }}
      </button>
    </template>
    <form @submit.prevent="model.refresh()">
      <p>{{ t("kubernetesResourceMaterial.recovery") }}</p>
      <label :for="`${prefix}-selector-kind`">{{
        t("kubernetesResourceMaterial.selector")
      }}</label>
      <select
        :id="`${prefix}-selector-kind`"
        v-model="selectorKind"
        :disabled="busy || !usable || uncertain"
      >
        <option value="MATERIAL">
          {{ t("kubernetesResourceMaterial.materialRef") }}
        </option>
        <option value="BEGIN_KEY">
          {{ t("kubernetesResourceMaterial.beginKey") }}
        </option>
      </select>
      <label :for="`${prefix}-selector`">{{
        t("kubernetesResourceMaterial.key")
      }}</label>
      <input
        :id="`${prefix}-selector`"
        v-model="selector"
        maxlength="128"
        autocomplete="off"
        :disabled="busy || !usable || uncertain"
      />
      <button
        type="submit"
        class="button"
        :disabled="busy || !usable || !selector"
      >
        {{ t("kubernetesResourceMaterial.refresh") }}
      </button>
    </form>
  </section>
</template>
<style scoped>
.kubernetes-resource-material,
form,
fieldset {
  display: grid;
  gap: 0.75rem;
}
.kubernetes-resource-material {
  border-top: 1px solid var(--border);
  padding-top: 1rem;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
input,
select {
  min-width: 0;
  width: 100%;
}
</style>
