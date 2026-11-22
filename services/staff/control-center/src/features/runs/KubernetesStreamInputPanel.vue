<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import KubernetesPortForwardPanel from "./KubernetesPortForwardPanel.vue";
import { useKubernetesStreamInput } from "./kubernetes-stream-input";
const props = defineProps<{ runRef: string; nodeRef: string }>();
const { t } = useI18n();
const model = useKubernetesStreamInput(
  computed(() => ({ runRef: props.runRef, nodeRef: props.nodeRef })),
);
const {
  items,
  selected,
  nextPageToken,
  draft,
  columns,
  rows,
  busy,
  writing,
  uncertain,
  problem,
  canSend,
  canResize,
  canStop,
} = model;
const draftTooLarge = computed(
  () =>
    !!selected.value &&
    new TextEncoder().encode(draft.value).length >
      selected.value.maximumFrameBytes,
);
</script>
<template>
  <section
    class="kubernetes-stream-input"
    data-testid="kubernetes-stream-input"
    :aria-label="t('kubernetesStreamInput.title')"
  >
    <h3>{{ t("kubernetesStreamInput.title") }}</h3>
    <p>{{ t("kubernetesStreamInput.description") }}</p>
    <ProblemNotice v-if="problem" :problem="problem" />
    <p v-if="uncertain" role="status">
      {{ t("kubernetesStreamInput.uncertain") }}
    </p>
    <div class="kubernetes-stream-input__actions">
      <button
        type="button"
        class="button button--ghost"
        :disabled="busy || writing"
        @click="selected ? model.refresh() : model.discover()"
      >
        {{ t("kubernetesStreamInput.refresh") }}
      </button>
      <button
        v-if="!selected && nextPageToken"
        type="button"
        class="button button--ghost"
        :disabled="busy || writing"
        @click="model.discover(nextPageToken)"
      >
        {{ t("kubernetesStreamInput.next") }}
      </button>
    </div>
    <p v-if="!items.length && !selected && !busy">
      {{ t("kubernetesStreamInput.empty") }}
    </p>
    <ul
      v-if="items.length && !selected"
      class="kubernetes-stream-input__streams"
    >
      <li v-for="item in items" :key="item.streamRef">
        <button
          type="button"
          class="button button--ghost"
          :disabled="busy || writing"
          @click="model.choose(item)"
        >
          {{ t("kubernetesStreamInput.choose") }}: {{ item.operation }} ·
          {{ item.targetRef }} · {{ item.resource.group || "core" }}/{{
            item.resource.version
          }}/{{ item.resource.resource }} · {{ item.namespace }}
          {{ item.podName }} {{ item.container }}
        </button>
      </li>
    </ul>
    <template v-if="selected">
      <button
        type="button"
        class="button button--ghost"
        :disabled="busy || writing"
        @click="model.unselect()"
      >
        {{ t("kubernetesStreamInput.change") }}
      </button>
      <dl>
        <div>
          <dt>{{ t("kubernetesStreamInput.target") }}</dt>
          <dd>
            {{ selected.targetRef }} · {{ selected.targetRevision }} ·
            {{ selected.connectionRef }}
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesStreamInput.source") }}</dt>
          <dd>
            {{ selected.operation }} ·
            {{ selected.resource.group || "core" }}/{{
              selected.resource.version
            }}/{{ selected.resource.resource }} · {{ selected.podName }}
            {{ selected.container }}
          </dd>
        </div>
        <div>
          <dt>{{ t("kubernetesStreamInput.scope") }}</dt>
          <dd>{{ selected.namespace }}</dd>
        </div>
        <div>
          <dt>{{ t("kubernetesStreamInput.state") }}</dt>
          <dd>{{ t(`kubernetesStreamInput.states.${selected.state}`) }}</dd>
        </div>
        <div v-if="selected.pending">
          <dt>{{ t("kubernetesStreamInput.pending") }}</dt>
          <dd>
            {{
              t(`kubernetesStreamInput.submissions.${selected.pending.state}`)
            }}
          </dd>
        </div>
      </dl>
      <template v-if="selected.inputChannels.includes('STDIN')">
        <label
          >{{ t("kubernetesStreamInput.input")
          }}<textarea
            v-model="draft"
            autocomplete="off"
            spellcheck="false"
            :disabled="!canSend"
            maxlength="32768"
          />
        </label>
        <p v-if="draftTooLarge" role="alert">
          {{ t("kubernetesStreamInput.tooLarge") }}
        </p>
        <div class="kubernetes-stream-input__actions">
          <button
            type="button"
            class="button"
            :disabled="!canSend || !draft || draftTooLarge"
            @click="model.send()"
          >
            {{ t("kubernetesStreamInput.send") }}
          </button>
          <button
            type="button"
            class="button button--ghost"
            :disabled="!canSend"
            @click="model.eof()"
          >
            {{ t("kubernetesStreamInput.eof") }}
          </button>
        </div>
      </template>
      <fieldset
        v-if="selected.inputChannels.includes('TERMINAL_RESIZE')"
        :disabled="busy || writing || uncertain"
      >
        <legend>{{ t("kubernetesStreamInput.resize") }}</legend>
        <label
          >{{ t("kubernetesStreamInput.columns")
          }}<input v-model.number="columns" type="number" min="1" max="1024"
        /></label>
        <label
          >{{ t("kubernetesStreamInput.rows")
          }}<input v-model.number="rows" type="number" min="1" max="512"
        /></label>
        <button
          type="button"
          class="button button--ghost"
          :disabled="!canResize"
          @click="model.resize()"
        >
          {{ t("kubernetesStreamInput.applySize") }}
        </button>
      </fieldset>
      <p v-if="selected.inputChannels.includes('FILE_CONTENT')">
        {{ t("kubernetesStreamInput.privateSource") }}
      </p>
      <KubernetesPortForwardPanel
        v-if="selected.operation === 'kubernetes.port-forward'"
        :key="selected.streamRef"
        :stream="selected"
      />
      <p v-if="!selected.inputChannels.length">
        {{ t("kubernetesStreamInput.readonly") }}
      </p>
      <p>
        {{
          t("kubernetesStreamInput.stagingQuota", {
            bytes: selected.maximumStagedBytes,
            objects: selected.maximumStagedObjects,
          })
        }}
      </p>
      <p>{{ t("kubernetesStreamInput.deliveryNote") }}</p>
      <button
        type="button"
        class="button button--danger"
        :disabled="!canStop"
        @click="model.stop()"
      >
        {{ t("kubernetesStreamInput.stop") }}
      </button>
      <p>{{ t("kubernetesStreamInput.stopNote") }}</p>
    </template>
  </section>
</template>
<style scoped>
.kubernetes-stream-input {
  display: grid;
  gap: 8px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  min-width: 0;
}
.kubernetes-stream-input h3,
.kubernetes-stream-input p {
  margin: 0;
}
.kubernetes-stream-input p {
  color: var(--muted);
  font-size: 0.85rem;
}
.kubernetes-stream-input__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
.kubernetes-stream-input__streams {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 6px;
}
.kubernetes-stream-input__streams button {
  white-space: normal;
  text-align: left;
  overflow-wrap: anywhere;
}
.kubernetes-stream-input label {
  display: grid;
  gap: 4px;
}
.kubernetes-stream-input textarea {
  min-height: 80px;
  resize: vertical;
}
.kubernetes-stream-input fieldset {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  border: 1px solid var(--border);
}
.kubernetes-stream-input dl {
  margin: 0;
  display: grid;
  gap: 4px;
}
.kubernetes-stream-input dl > div {
  display: grid;
  grid-template-columns: 100px minmax(0, 1fr);
  gap: 6px;
}
.kubernetes-stream-input dd {
  margin: 0;
  overflow-wrap: anywhere;
}
</style>
