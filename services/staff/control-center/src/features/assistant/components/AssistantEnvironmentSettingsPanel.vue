<script setup lang="ts">
import { Save, ServerCog } from "@lucide/vue";
import { computed, onMounted, reactive, ref } from "vue";

import { loadAgentRuntime, saveRuntimeEnvironment } from "@/features/agents/detail/runtime-api";
import RuntimeEnvironmentFieldListsEditor from "@/features/runtime/RuntimeEnvironmentFieldListsEditor.vue";
import RuntimeEnvironmentPolicyFields from "@/features/runtime/RuntimeEnvironmentPolicyFields.vue";
import {
  editableRuntimeEnvironmentPolicy,
  normalizeRuntimeEnvironmentInput,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";

const props = defineProps<{ agentRef: string; canEdit: boolean }>();
const view = ref<Awaited<ReturnType<typeof loadAgentRuntime>>>();
const loading = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const input = reactive<RuntimeEnvironmentInput>({
  name: "",
  description: "",
  imageArtifactRef: "",
  tools: [],
  values: [],
  secretBindings: [],
  policy: {
    resources: {
      cpuRequestMilli: 500,
      cpuLimitMilli: 2000,
      memoryRequestMib: 512,
      memoryLimitMib: 2048,
      ephemeralStorageRequestMib: 512,
      ephemeralStorageLimitMib: 4096,
    },
    volumes: [],
    networkDestinations: ["DNS", "PROVIDER_PROXY", "RUNTIME_CALLBACK"],
    webAccess: { mode: "NONE", rules: [] },
    kubernetesAccess: "NONE",
  },
});
const initial = ref("");
const environment = computed(() => view.value?.environment);
const normalized = computed(() => normalizeRuntimeEnvironmentInput(input));
const fingerprint = computed(() => JSON.stringify(normalized.value));
const validation = computed(() =>
  validateEnvironmentInput({
    ...normalized.value,
    imageArtifactRef: "imgart_system_assistant",
  }).filter((item) => item.field !== "imageArtifactRef"),
);
const dirty = computed(() => Boolean(initial.value) && fingerprint.value !== initial.value);

function sync(current: RuntimeEnvironmentSet): void {
  input.name = current.name;
  input.description = current.description;
  input.imageArtifactRef = "";
  input.tools = [];
  input.values = current.currentVersion.values.map((item) => ({ ...item }));
  input.secretBindings = [];
  input.policy = editableRuntimeEnvironmentPolicy(current.currentVersion.policy);
  initial.value = JSON.stringify(normalizeRuntimeEnvironmentInput(input));
}

async function load(): Promise<void> {
  loading.value = true;
  problem.value = undefined;
  try {
    const result = await loadAgentRuntime(props.agentRef);
    if (result.environment.projectRef)
      throw new Error("System assistant environment must be organization scoped");
    view.value = result;
    sync(result.environment);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    loading.value = false;
  }
}

async function save(): Promise<void> {
  const current = environment.value;
  if (!current || busy.value || !props.canEdit || !dirty.value || validation.value.length) return;
  busy.value = true;
  problem.value = undefined;
  try {
    const saved = await saveRuntimeEnvironment(current, normalized.value);
    if (!view.value) return;
    view.value = { ...view.value, environment: saved };
    sync(saved);
  } catch (error) {
    problem.value = asProblem(error);
  } finally {
    busy.value = false;
  }
}

onMounted(() => void load());
</script>

<template>
  <AsyncState :loading="loading" :problem="problem" @retry="load">
    <div v-if="environment" class="assistant-environment-settings">
      <section class="panel assistant-environment-settings__general">
        <div class="section-header">
          <div>
            <h3>{{ $t("assistant.settings.environmentTitle") }}</h3>
            <p>{{ $t("assistant.settings.environmentHelp") }}</p>
          </div>
          <ServerCog :size="20" aria-hidden="true" />
        </div>
        <div class="form-grid">
          <label class="field">
            <span>{{ $t("common.name") }}</span>
            <input v-model="input.name" maxlength="120" :disabled="busy || !canEdit" />
          </label>
          <label class="field field--wide">
            <span>{{ $t("common.description") }}</span>
            <VoiceTextarea v-model="input.description" maxlength="1000" :disabled="busy || !canEdit" />
          </label>
          <div class="field field--wide">
            <span>{{ $t("assistant.settings.platformImage") }}</span>
            <code>{{ environment.currentVersion.image.reference }}</code>
            <small>{{ $t("assistant.settings.platformImageHelp") }}</small>
          </div>
        </div>
      </section>
      <RuntimeEnvironmentFieldListsEditor
        :values="input.values"
        :secret-bindings="[]"
        project-ref=""
        mode="VALUES"
        :disabled="busy || !canEdit"
        @update:values="input.values = $event"
      />
      <RuntimeEnvironmentPolicyFields
        :policy="input.policy"
        :disabled="busy || !canEdit"
        @update:policy="input.policy = $event"
      />
      <ul v-if="validation.length" class="field-error" role="alert">
        <li v-for="item in validation" :key="`${item.field}:${item.message}`">{{ $t(item.message) }}</li>
      </ul>
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <div class="assistant-environment-settings__actions">
        <button class="button button--primary" type="button" :disabled="busy || !canEdit || !dirty || !!validation.length" @click="save">
          <Save :size="16" aria-hidden="true" />
          {{ $t("common.save") }}
        </button>
      </div>
    </div>
  </AsyncState>
</template>

<style scoped>
.assistant-environment-settings { display: grid; gap: 16px; }
.assistant-environment-settings__general { display: grid; gap: 16px; padding: 16px; }
.section-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
.section-header h3, .section-header p { margin: 0; }
.section-header p, small { color: var(--muted); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
.field { display: grid; gap: 6px; }
.field--wide { grid-column: 1 / -1; }
.field code { overflow-wrap: anywhere; }
.field-error { margin: 0; color: var(--danger); }
.assistant-environment-settings__actions { display: flex; justify-content: flex-end; position: sticky; bottom: -28px; padding: 12px 0 0; background: var(--surface); }
</style>
