<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import type { KubernetesStreamInputView } from "@/shared/api/generated/openapi/types.gen";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import { useKubernetesPortForward } from "./kubernetes-port-forward";
const props = defineProps<{ stream: KubernetesStreamInputView }>();
const { t } = useI18n();
const model = useKubernetesPortForward(computed(() => props.stream));
const {
  items,
  selected,
  profile,
  next,
  slot,
  draft,
  busy,
  uncertain,
  problem,
  canOpen,
  canSend,
  canClose,
} = model;
</script>
<template>
  <section
    data-testid="kubernetes-port-forward"
    class="kubernetes-port-forward"
  >
    <h4>{{ t("kubernetesPortForward.title") }}</h4>
    <p>{{ t("kubernetesPortForward.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="status">
      {{ t("kubernetesPortForward.uncertain") }}
    </p>
    <button
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh()"
    >
      {{ t("kubernetesPortForward.refresh") }}
    </button>
    <template v-if="profile">
      <p>
        {{
          t("kubernetesPortForward.capacity", {
            active: profile.activeConnections,
            max: profile.maximumActiveConnections,
            seconds: profile.timeoutSeconds,
          })
        }}
      </p>
      <p v-if="profile.admissionReasonCode" role="status">
        {{ t(`kubernetesPortForward.reasons.${profile.admissionReasonCode}`) }}
      </p>
      <label
        >{{ t("kubernetesPortForward.mapping") }}
        <select v-model.number="slot" :disabled="busy">
          <option disabled :value="undefined">
            {{ t("kubernetesPortForward.chooseMapping") }}
          </option>
          <option
            v-for="(mapping, index) in profile.ports"
            :key="index"
            :value="index"
          >
            {{ mapping.port }} → {{ mapping.targetPort }}
          </option>
        </select>
      </label>
      <button
        type="button"
        class="button"
        :disabled="!canOpen"
        @click="model.open()"
      >
        {{ t("kubernetesPortForward.open") }}
      </button>
    </template>
    <ul>
      <li v-for="item in items" :key="item.connectionRef">
        <button
          type="button"
          class="button button--ghost"
          :disabled="busy"
          @click="model.choose(item)"
        >
          {{ item.connectionRef }} ·
          {{ t(`kubernetesPortForward.states.${item.state}`) }} ·
          {{ t("kubernetesPortForward.slot", { slot: item.slot + 1 }) }}
        </button>
      </li>
    </ul>
    <button
      v-if="next"
      type="button"
      class="button button--ghost"
      :disabled="busy"
      @click="model.refresh(next)"
    >
      {{ t("kubernetesPortForward.next") }}
    </button>
    <template v-if="selected">
      <p>
        {{ selected.connectionRef }} · {{ selected.generation }} ·
        {{ t(`kubernetesPortForward.states.${selected.state}`) }}
      </p>
      <p>
        {{
          selected.nativeJoined
            ? t("kubernetesPortForward.joined")
            : t("kubernetesPortForward.joinUnknown")
        }}
      </p>
      <p v-if="selected.localWriteClosed">
        {{ t("kubernetesPortForward.writeClosed") }}
      </p>
      <p v-if="selected.readEndObserved">
        {{ t("kubernetesPortForward.readEnded") }}
      </p>
      <label
        >{{ t("kubernetesPortForward.data")
        }}<textarea
          v-model="draft"
          :disabled="!canSend"
          autocomplete="off"
          spellcheck="false"
          maxlength="32717"
        />
      </label>
      <button
        type="button"
        class="button"
        :disabled="!canSend || !draft"
        @click="model.send()"
      >
        {{ t("kubernetesPortForward.send") }}
      </button>
      <button
        type="button"
        class="button button--ghost"
        :disabled="!canSend"
        @click="model.send(true)"
      >
        {{ t("kubernetesPortForward.halfClose") }}
      </button>
      <button
        type="button"
        class="button button--danger"
        :disabled="!canClose"
        @click="model.closeConnection()"
      >
        {{ t("kubernetesPortForward.close") }}
      </button>
    </template>
  </section>
</template>
<style scoped>
.kubernetes-port-forward {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  min-width: 0;
}
.kubernetes-port-forward p {
  margin: 0;
  color: var(--muted);
}
.kubernetes-port-forward textarea {
  width: 100%;
  min-height: 72px;
}
</style>
