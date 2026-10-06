<script setup lang="ts">
import { ServerCog } from "@lucide/vue";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";

import { loadAgentRuntime } from "@/features/agents/detail/runtime-api";
import RuntimeEnvironmentFieldListsEditor from "@/features/runtime/RuntimeEnvironmentFieldListsEditor.vue";
import RuntimeEnvironmentPolicyFields from "@/features/runtime/RuntimeEnvironmentPolicyFields.vue";
import RuntimeEnvironmentImageToolsSelector from "@/features/runtime/RuntimeEnvironmentImageToolsSelector.vue";
import RuntimeResourceManagementLinks from "@/features/runtime/RuntimeResourceManagementLinks.vue";
import RuntimeEnvironmentDraftActions from "@/features/runtime/RuntimeEnvironmentDraftActions.vue";
import { useSessionStore } from "@/features/session/store";
import { usePlatformStore } from "@/features/platform/store";
import { useRoute, useRouter } from "vue-router";
import {
  assertActiveRuntimeResourceIdentity,
  runtimeResourceOwnerBoundary,
} from "@/features/runtime/active-resource-owner";
import type { RuntimeImageCatalog } from "@/features/runtime/image-tools-selection";
import type { RuntimeSecretCatalog } from "@/features/runtime/secret-catalog";
import {
  runtimeResourceScopeKey,
  type RuntimeResourceScope,
} from "@/features/runtime/resource-scope";
import { editableAssistantEnvironment } from "@/features/assistant/environment-settings";
import {
  normalizeRuntimeEnvironmentInput,
  validateEnvironmentInput,
} from "@/features/runtime/environment-form";
import type {
  RuntimeEnvironmentInput,
  RuntimeEnvironmentSet,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";
import { asProblem, type AppProblem } from "@/shared/api/problem";
import AsyncState from "@/shared/ui/AsyncState.vue";
import ProblemNotice from "@/shared/ui/ProblemNotice.vue";
import VoiceTextarea from "@/shared/ui/VoiceTextarea.vue";
import { useServerMessage } from "@/shared/ui/server-message";
import { environmentDisplayField } from "@/features/runtime/environment-display-field";

const props = defineProps<{
  agentRef: string;
  canEdit: boolean;
  resourceScope: RuntimeResourceScope;
  imageCatalog: RuntimeImageCatalog | undefined;
  secretCatalog?: RuntimeSecretCatalog;
  returnTo?: string;
  initialDraftRef?: string;
  expectedDraftVersion?: number;
}>();
const emit = defineEmits<{ draftSaved: [draft: RuntimeEnvironmentDraft] }>();
const session = useSessionStore();
const platform = usePlatformStore();
const route = useRoute();
const router = useRouter();
const view = ref<Awaited<ReturnType<typeof loadAgentRuntime>>>();
const loading = ref(false);
const busy = ref(false);
const problem = ref<AppProblem>();
const imageAvailable = ref(false);
let generation = 0;
let controller: AbortController | undefined;
const scopeKey = computed(() => runtimeResourceScopeKey(props.resourceScope));
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
const localizeServerMessage = useServerMessage();
const nameFieldValue = environmentDisplayField(
  () => input.name,
  (value) => {
    input.name = value;
  },
  localizeServerMessage,
);
const descriptionFieldValue = environmentDisplayField(
  () => input.description,
  (value) => {
    input.description = value;
  },
  localizeServerMessage,
);
const initial = ref("");
const environment = computed(() => view.value?.environment);
const consumerNames = computed<Record<string, string>>(() => {
  const bootstrap = platform.bootstrap;
  if (
    props.resourceScope.kind !== "ORGANIZATION" ||
    bootstrap?.organizationRef !== props.resourceScope.organizationRef ||
    bootstrap.assistant.ref !== props.agentRef ||
    !bootstrap.assistant.name
  )
    return {};
  return { [bootstrap.assistant.ref]: bootstrap.assistant.name };
});
const normalized = computed(() => normalizeRuntimeEnvironmentInput(input));
const validation = computed(() => validateEnvironmentInput(normalized.value));

function sync(current: RuntimeEnvironmentSet): void {
  Object.assign(
    input,
    editableAssistantEnvironment(current, props.resourceScope),
  );
  initial.value = JSON.stringify(normalizeRuntimeEnvironmentInput(input));
}

async function load(): Promise<void> {
  generation += 1;
  controller?.abort();
  controller = new AbortController();
  const signal = controller.signal;
  const currentGeneration = generation;
  loading.value = true;
  problem.value = undefined;
  try {
    const owner = runtimeResourceOwnerBoundary(
      props.resourceScope.kind === "PROJECT"
        ? props.resourceScope.projectRef
        : props.resourceScope,
    );
    const result = await loadAgentRuntime(props.agentRef, signal);
    if (signal.aborted || currentGeneration !== generation) return;
    if (
      result.configuration.agentRef !== props.agentRef ||
      result.environmentBinding.agentRef !== props.agentRef ||
      result.environmentBinding.environmentRef !== result.environment.ref
    )
      throw new Error("Assistant runtime configuration identity mismatch");
    owner.assert(result.environment);
    sync(result.environment);
    view.value = result;
  } catch (error) {
    if (!signal.aborted && currentGeneration === generation)
      problem.value = asProblem(error);
  } finally {
    if (currentGeneration === generation) loading.value = false;
  }
}

function loadDraft(value: RuntimeEnvironmentDraft): void {
  Object.assign(input, structuredClone(value.specification));
}
function published(value: RuntimeEnvironmentSet): void {
  if (!view.value) return;
  sync(value);
  view.value = { ...view.value, environment: value };
}
async function reauthenticate(value: RuntimeEnvironmentDraft): Promise<void> {
  if (props.resourceScope.kind !== "ORGANIZATION" || !environment.value) return;
  assertActiveRuntimeResourceIdentity(
    props.resourceScope,
    value,
    props.resourceScope.organizationRef,
  );
  await session.beginOrganizationAssistantEnvironmentReauth({
    organizationRef: props.resourceScope.organizationRef,
    agentRef: props.agentRef,
    environmentRef: environment.value.ref,
    draftRef: value.ref,
    draftVersion: value.version,
  });
}
async function draftSaved(value: RuntimeEnvironmentDraft): Promise<void> {
  emit("draftSaved", value);
  if (route.path === "/organization/assistant/environment")
    await router.replace({ query: { ...route.query, draftRef: value.ref } });
  if (value.state === "PUBLISHED") await load();
}

function reset(): void {
  generation += 1;
  controller?.abort();
  view.value = undefined;
  initial.value = "";
  imageAvailable.value = false;
  busy.value = false;
  problem.value = undefined;
}
watch(
  [() => props.agentRef, scopeKey],
  () => {
    reset();
    void load();
  },
  { immediate: true },
);
onBeforeUnmount(reset);
</script>

<template>
  <AsyncState :loading="loading" :problem="problem" @retry="load">
    <div v-if="environment" class="assistant-environment-settings">
      <section class="panel assistant-environment-settings__general">
        <div class="section-header">
          <div>
            <h3>{{ $t("assistant.settings.environmentTitle") }}</h3>
            <p>{{ $t("assistant.settings.environmentHelp") }}</p>
            <span
              class="assistant-environment-settings__scope"
              :data-runtime-scope="resourceScope.kind"
              >{{
                $t(
                  resourceScope.kind === "ORGANIZATION"
                    ? "assistant.settings.systemScope"
                    : "assistant.settings.projectScope",
                )
              }}</span
            >
          </div>
          <ServerCog :size="20" aria-hidden="true" />
        </div>
        <div class="form-grid">
          <label class="field">
            <span>{{ $t("common.name") }}</span>
            <input
              v-model="nameFieldValue"
              maxlength="120"
              :disabled="busy || !canEdit"
            />
          </label>
          <label class="field field--wide">
            <span>{{ $t("common.description") }}</span>
            <VoiceTextarea
              class="assistant-environment-description"
              v-model="descriptionFieldValue"
              maxlength="1000"
              :disabled="busy || !canEdit"
            />
          </label>
        </div>
      </section>
      <section class="panel assistant-environment-settings__image">
        <div class="section-header">
          <div>
            <h3>{{ $t("runtime.imageAndTools") }}</h3>
            <p>{{ $t("runtime.imageAndToolsHelp") }}</p>
          </div>
        </div>
        <RuntimeResourceManagementLinks
          :resource-scope="resourceScope"
          :return-to="returnTo"
        />
        <RuntimeEnvironmentImageToolsSelector
          v-if="imageCatalog"
          :resource-scope="resourceScope"
          :image-artifact-ref="input.imageArtifactRef"
          :current-image="environment.currentVersion.image"
          :tools="input.tools"
          :catalog="imageCatalog"
          :disabled="busy || !canEdit"
          @update:image-artifact-ref="input.imageArtifactRef = $event"
          @update:tools="input.tools = $event"
          @availability-change="imageAvailable = $event"
        />
        <template v-else>
          <div class="field">
            <span>{{ $t("runtime.exactImage") }}</span
            ><code>{{ environment.currentVersion.image.reference }}</code>
          </div>
          <p class="secondary-text" role="status">
            {{ $t("assistant.settings.imageCatalogUnavailable") }}
          </p>
          <div
            v-if="input.tools.length"
            class="assistant-environment-settings__pinned-tools"
          >
            <strong>{{ $t("runtime.verifiedTools") }}</strong>
            <span v-for="tool in input.tools" :key="tool.command"
              >{{ tool.name }} · <code>{{ tool.command }}</code></span
            >
          </div>
        </template>
      </section>
      <RuntimeEnvironmentFieldListsEditor
        :values="input.values"
        :secret-bindings="input.secretBindings"
        :project-ref="
          resourceScope.kind === 'PROJECT' ? resourceScope.projectRef : ''
        "
        :resource-scope="resourceScope"
        :secret-catalog="secretCatalog"
        :descriptors="environment.currentVersion.secretDescriptors"
        :disabled="busy || !canEdit"
        @update:values="input.values = $event"
        @update:secret-bindings="input.secretBindings = $event"
      />
      <p
        v-if="resourceScope.kind === 'ORGANIZATION' && !secretCatalog"
        class="secondary-text"
        role="status"
      >
        {{ $t("assistant.settings.secretCatalogUnavailable") }}
      </p>
      <details class="panel assistant-environment-settings__advanced">
        <summary>
          {{ $t("common.advanced")
          }}<small>{{ $t("assistant.settings.environmentAdvanced") }}</small>
        </summary>
        <RuntimeEnvironmentPolicyFields
          :policy="input.policy"
          :disabled="busy || !canEdit"
          @update:policy="input.policy = $event"
        />
      </details>
      <ul v-if="validation.length" class="field-error" role="alert">
        <li v-for="item in validation" :key="`${item.field}:${item.message}`">
          {{ $t(item.message) }}
        </li>
      </ul>
      <ProblemNotice v-if="problem" :problem="problem" compact />
      <RuntimeEnvironmentDraftActions
        :resource-scope="
          resourceScope.kind === 'PROJECT'
            ? resourceScope.projectRef
            : resourceScope
        "
        :environment="environment"
        :consumer-names="consumerNames"
        :specification="normalized"
        :can-edit="canEdit"
        :valid="!validation.length && Boolean(imageCatalog) && imageAvailable"
        :initial-draft-ref="initialDraftRef"
        :expected-draft-version="expectedDraftVersion"
        @busy="busy = $event"
        @draft-loaded="loadDraft"
        @draft-saved="draftSaved"
        @published="published"
        @reauthenticate="reauthenticate"
      />
    </div>
  </AsyncState>
</template>

<style scoped>
:deep(textarea.assistant-environment-description) {
  min-height: 72px;
  height: 72px;
}
.assistant-environment-settings {
  display: grid;
  gap: 16px;
}
.assistant-environment-settings__general,
.assistant-environment-settings__image {
  display: grid;
  gap: 16px;
  padding: 16px;
}
.assistant-environment-settings__advanced {
  padding: 16px;
}
.assistant-environment-settings__advanced summary {
  cursor: pointer;
  font-weight: 600;
}
.assistant-environment-settings__advanced summary small {
  display: block;
  margin-top: 4px;
  font-weight: 400;
}
.assistant-environment-settings__advanced[open] summary {
  margin-bottom: 16px;
}
.assistant-environment-settings__pinned-tools {
  display: grid;
  gap: 6px;
}
.assistant-environment-settings__scope {
  display: inline-flex;
  margin-top: 8px;
  padding: 4px 8px;
  border-radius: 6px;
  background: var(--accent-soft);
  color: var(--accent);
  font-size: 0.75rem;
}
.section-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.section-header > div {
  flex: 1;
  min-width: 0;
}
.section-header > svg {
  flex-shrink: 0;
}
.section-header h3,
.section-header p {
  margin: 0;
}
.section-header p,
small {
  color: var(--muted);
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}
.field {
  display: grid;
  gap: 6px;
}
.field--wide {
  grid-column: 1 / -1;
}
.field code {
  overflow-wrap: anywhere;
}
.field-error {
  margin: 0;
  color: var(--danger);
}
.assistant-environment-settings__actions {
  display: flex;
  justify-content: flex-end;
  position: sticky;
  bottom: -28px;
  padding: 12px 0 0;
  background: var(--surface);
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
