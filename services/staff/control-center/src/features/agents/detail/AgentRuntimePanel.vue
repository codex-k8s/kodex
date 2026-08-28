<script setup lang="ts">
import { LockKeyhole, Save } from "@lucide/vue";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import CodeEditorSurface from "@/features/agents/detail/CodeEditorSurface.vue";
import { agentDetailCopy } from "@/features/agents/detail/copy";
import {
  readyRuntimes,
  runtimeModels,
  runtimeProviders,
  runtimeRefForSelection,
  runtimesForSelection,
} from "@/features/agents/detail/model";
import type { RuntimeSelection } from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";

const props = defineProps<{
  modelValue: string;
  runtimes: readonly RuntimeSelection[];
  canEdit: boolean;
  busy: boolean;
  dirty: boolean;
}>();
const emit = defineEmits<{
  "update:modelValue": [value: string];
  save: [];
}>();
const { locale } = useI18n();
const copy = computed(() => agentDetailCopy(locale.value));
const availableRuntimes = computed(() => readyRuntimes(props.runtimes));
const selectedRuntime = computed(
  () =>
    availableRuntimes.value.find(
      (runtime) => runtime.ref === props.modelValue,
    ) ?? availableRuntimes.value[0],
);
const providers = computed(() => runtimeProviders(availableRuntimes.value));
const models = computed(() =>
  runtimeModels(availableRuntimes.value, selectedRuntime.value?.provider ?? ""),
);
const matchingRuntimes = computed(() =>
  runtimesForSelection(
    availableRuntimes.value,
    selectedRuntime.value?.provider ?? "",
    selectedRuntime.value?.model ?? "",
  ),
);

function eventValue(event: Event): string | undefined {
  const target = event.currentTarget;
  return target instanceof HTMLSelectElement ? target.value : undefined;
}

function chooseProvider(event: Event): void {
  const provider = eventValue(event);
  if (!provider) return;
  const next = runtimeRefForSelection(availableRuntimes.value, provider);
  if (next) emit("update:modelValue", next);
}

function chooseModel(event: Event): void {
  const model = eventValue(event);
  const provider = selectedRuntime.value?.provider;
  if (!model || !provider) return;
  const next = runtimeRefForSelection(availableRuntimes.value, provider, model);
  if (next) emit("update:modelValue", next);
}

function chooseRuntime(event: Event): void {
  const value = eventValue(event);
  if (value) emit("update:modelValue", value);
}
</script>

<template>
  <div class="runtime-layout">
    <article class="runtime-panel panel">
      <div class="runtime-panel__head">
        <div>
          <h2>{{ copy.runtime.title }}</h2>
          <p>{{ copy.runtime.catalogRef }}</p>
        </div>
        <StatusBadge
          :state="selectedRuntime?.ready ? 'READY' : 'UNAVAILABLE'"
        />
      </div>
      <div class="runtime-panel__selectors">
        <label class="field">
          <span>{{ $t("agents.provider") }}</span>
          <select
            :value="selectedRuntime?.provider"
            :disabled="!canEdit || busy || providers.length === 0"
            @change="chooseProvider"
          >
            <option
              v-for="provider in providers"
              :key="provider"
              :value="provider"
            >
              {{ provider }}
            </option>
          </select>
        </label>
        <label class="field">
          <span>{{ $t("agents.model") }}</span>
          <select
            :value="selectedRuntime?.model"
            :disabled="!canEdit || busy || models.length === 0"
            @change="chooseModel"
          >
            <option v-for="model in models" :key="model" :value="model">
              {{ model }}
            </option>
          </select>
        </label>
        <label class="field">
          <span>{{ copy.runtime.profile }}</span>
          <select
            :value="selectedRuntime?.ref"
            :disabled="!canEdit || busy || matchingRuntimes.length === 0"
            @change="chooseRuntime"
          >
            <option
              v-for="runtime in matchingRuntimes"
              :key="runtime.ref"
              :value="runtime.ref"
            >
              {{ runtime.name }} · {{ runtime.revision }}
            </option>
          </select>
        </label>
      </div>
      <dl v-if="selectedRuntime" class="runtime-panel__summary">
        <div>
          <dt>{{ $t("agents.provider") }}</dt>
          <dd>{{ selectedRuntime.provider }}</dd>
        </div>
        <div>
          <dt>{{ $t("agents.model") }}</dt>
          <dd class="mono">{{ selectedRuntime.model }}</dd>
        </div>
        <div>
          <dt>{{ $t("agents.runtimeRevision") }}</dt>
          <dd class="mono">{{ selectedRuntime.revision }}</dd>
        </div>
      </dl>
      <div v-if="canEdit" class="runtime-panel__actions">
        <span v-if="dirty">{{ $t("states.DRAFT") }}</span>
        <button
          class="button button--primary"
          type="button"
          :disabled="busy || !dirty || !selectedRuntime"
          @click="emit('save')"
        >
          <Save :size="16" aria-hidden="true" />{{ copy.runtime.save }}
        </button>
      </div>
    </article>

    <article class="overlay-panel panel">
      <div class="overlay-panel__head">
        <div>
          <h2>{{ copy.runtime.overlay }}</h2>
          <p>{{ copy.runtime.overlayHelp }}</p>
        </div>
        <StatusBadge state="UNAVAILABLE" />
      </div>
      <CodeEditorSurface
        model-value=""
        language="toml"
        :label="copy.runtime.overlay"
        :placeholder="copy.runtime.overlayPlaceholder"
        readonly
        :min-lines="13"
      />
      <div class="overlay-panel__actions">
        <span
          ><LockKeyhole :size="15" aria-hidden="true" />config.toml
          mutation</span
        >
        <button
          class="button button--primary"
          type="button"
          disabled
          :title="$t('common.unavailable')"
        >
          {{ $t("common.save") }}
        </button>
      </div>
    </article>
  </div>
</template>

<style scoped>
.runtime-layout {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(420px, 1.1fr);
  gap: 16px;
  align-items: start;
}
.runtime-panel,
.overlay-panel {
  display: grid;
  gap: 16px;
}
.runtime-panel__head,
.overlay-panel__head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.runtime-panel h2,
.runtime-panel p,
.overlay-panel h2,
.overlay-panel p {
  margin: 0;
}
.runtime-panel p,
.overlay-panel p {
  margin-top: 4px;
  color: var(--muted);
  font-size: 0.82rem;
}
.runtime-panel__selectors {
  display: grid;
  gap: 12px;
}
.runtime-panel__summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 1px;
  margin: 0;
  overflow: hidden;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--border);
}
.runtime-panel__summary div {
  min-width: 0;
  padding: 10px;
  background: var(--panel);
}
.runtime-panel__summary dt {
  color: var(--subtle);
  font-size: 0.72rem;
}
.runtime-panel__summary dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
.runtime-panel__actions,
.overlay-panel__actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.runtime-panel__actions span,
.overlay-panel__actions span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-right: auto;
  color: var(--warning);
  font-size: 0.78rem;
}
@media (max-width: 1040px) {
  .runtime-layout {
    grid-template-columns: 1fr;
  }
}
@media (max-width: 640px) {
  .runtime-panel__summary {
    grid-template-columns: 1fr;
  }
}
</style>
