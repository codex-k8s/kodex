<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import AssistantEnvironmentBindingDialog from "@/features/assistant/components/AssistantEnvironmentBindingDialog.vue";
import { loadAgentRuntime } from "@/features/agents/detail/runtime-api";
import {
  assistantEnvironmentDraftTarget,
  assistantSystemEnvironmentDraftTarget,
} from "@/features/assistant/model";
import { usePlatformStore } from "@/features/platform/store";
import { readAssistantProjectHelper } from "@/features/assistant/project-helper-readback";
import {
  assertRuntimeResourceIdentity,
  organizationRuntimeResourceScope,
  type RuntimeResourceScope,
} from "@/features/runtime/resource-scope";
import { readEnvironmentDraft } from "@/features/runtime/environment-drafts";
import type {
  AssistantPlan,
  AgentRuntimeConfigurationView,
  RuntimeEnvironmentDraft,
} from "@/shared/api/generated/openapi/types.gen";
import StatusBadge from "@/shared/ui/StatusBadge.vue";
import { useServerMessage } from "@/shared/ui/server-message";

const props = defineProps<{ plan: AssistantPlan; operationRef: string }>();
const emit = defineEmits<{ navigate: [] }>();
const platform = usePlatformStore();
const { t } = useI18n();
const serverMessage = useServerMessage();
const systemTarget = computed(() =>
  assistantSystemEnvironmentDraftTarget(props.plan, props.operationRef),
);
const organizationScope = computed(() =>
  organizationRuntimeResourceScope(platform.bootstrap),
);
const target = computed(() =>
  systemTarget.value
    ? undefined
    : assistantEnvironmentDraftTarget(
        props.plan,
        props.operationRef,
        platform.bootstrap?.organizationRef,
      ),
);
const draft = ref<RuntimeEnvironmentDraft>();
const draftTitle = computed(() =>
  t(
    draft.value?.scopeKind === "ORGANIZATION"
      ? "assistant.environmentDraft.systemTitle"
      : "assistant.environmentDraft.title",
  ),
);
const draftName = computed(() =>
  draft.value ? serverMessage(draft.value.specification.name) : "",
);
const loading = ref(false);
const problem = ref(false);
const bindingOpen = ref(false);
const boundAgentName = ref("");
const systemBinding = ref<AgentRuntimeConfigurationView>();
const systemBindingUnavailable = ref(false);
const systemBindingMessage = computed(() => {
  if (!systemTarget.value || draft.value?.state !== "PUBLISHED") return;
  if (systemBindingUnavailable.value)
    return t("assistant.environmentDraft.bindingUnavailable");
  if (!systemBinding.value)
    return t("assistant.environmentDraft.bindingChecking");
  if (
    systemBinding.value.environmentBinding.environmentRef !==
    draft.value.publishedEnvironmentRef
  )
    return t("assistant.environmentDraft.systemBindingChanged");
  return t("assistant.environmentDraft.systemBound", {
    revision: systemBinding.value.environment.currentVersion.revision,
  });
});
async function readSystemBinding(
  assistantRef: string,
  scope: Extract<RuntimeResourceScope, { kind: "ORGANIZATION" }>,
  signal: AbortSignal,
): Promise<void> {
  if (signal.aborted) return;
  try {
    const next = await loadAgentRuntime(assistantRef, signal);
    if (
      next.configuration.agentRef !== assistantRef ||
      next.environmentBinding.agentRef !== assistantRef ||
      next.environmentBinding.environmentRef !== next.environment.ref ||
      !next.environmentBinding.versionRef ||
      next.environmentBinding.versionRef !==
        next.environment.currentVersion.ref ||
      !Number.isSafeInteger(next.environment.currentVersion.revision) ||
      next.environment.currentVersion.revision < 1
    )
      throw new Error("System assistant environment binding readback mismatch");
    assertRuntimeResourceIdentity(
      scope,
      next.environment,
      scope.organizationRef,
    );
    // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- Запрос может завершиться после закрытия watcher lifetime.
    if (!signal.aborted) systemBinding.value = next;
  } catch {
    // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- Запрос может завершиться после закрытия watcher lifetime.
    if (!signal.aborted) systemBindingUnavailable.value = true;
  }
}
const destination = computed(() => {
  const exact = target.value;
  const current = draft.value;
  if (!current || current.state === "DISCARDED") return;
  if (
    systemTarget.value &&
    organizationScope.value &&
    current.scopeKind === "ORGANIZATION"
  )
    return {
      name: "system-assistant-environment",
      query: { draftRef: current.ref },
    };
  if (!exact) return;
  if (current.state === "PUBLISHED") {
    if (!current.publishedEnvironmentRef) return;
    return {
      name: "runtime-environment",
      params: {
        projectRef: exact.projectRef,
        environmentRef: current.publishedEnvironmentRef,
      },
      query: { assistantForm: "1" },
    };
  }
  if (current.environmentRef) {
    return {
      name: "runtime-environment",
      params: {
        projectRef: exact.projectRef,
        environmentRef: current.environmentRef,
      },
      query: { draftRef: exact.draftRef, assistantForm: "1" },
    };
  }
  return {
    name: "runtime-environment-new",
    params: { projectRef: exact.projectRef },
    query: { draftRef: exact.draftRef, assistantForm: "1" },
  };
});
let refresh: (() => Promise<void>) | undefined;

watch(
  () => ({
    target: target.value,
    system: systemTarget.value,
    scope: organizationScope.value,
    assistantRef: platform.assistant?.ref ?? platform.bootstrap?.assistant.ref,
  }),
  (value, _previous, onCleanup) => {
    draft.value = undefined;
    loading.value = false;
    problem.value = false;
    bindingOpen.value = false;
    boundAgentName.value = "";
    systemBinding.value = undefined;
    systemBindingUnavailable.value = false;
    const exact = value.target;
    const system = value.system;
    if (
      !exact &&
      (!system || !value.scope || system.assistantRef !== value.assistantRef)
    )
      return;
    const address = system && value.scope ? value.scope : exact?.projectRef;
    const draftRef = system ? system.draftRef : exact?.draftRef;
    if (!address || !draftRef) return;
    const controller = new AbortController();
    onCleanup(() => {
      controller.abort();
      refresh = undefined;
    });
    refresh = async () => {
      if (controller.signal.aborted) return;
      loading.value = true;
      systemBinding.value = undefined;
      systemBindingUnavailable.value = false;
      try {
        const operation = props.plan.operations.find(
          (item) => item.ref === props.operationRef,
        );
        if (operation && exact)
          await readAssistantProjectHelper(
            props.plan,
            operation,
            platform.bootstrap?.organizationRef,
            controller.signal,
          );
        const next = await readEnvironmentDraft(
          address,
          draftRef,
          controller.signal,
        );
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (controller.signal.aborted) return;
        if (system && next.environmentRef !== system.environmentRef)
          throw new Error("System assistant draft environment mismatch");
        if (
          operation?.type === "PREPARE_RUNTIME_ENVIRONMENT_REVISION" &&
          next.environmentRef !== operation.target.ref
        )
          throw new Error("Assistant revision draft environment mismatch");
        draft.value = next;
        problem.value = false;
        if (system && value.scope && next.state === "PUBLISHED")
          await readSystemBinding(
            system.assistantRef,
            value.scope,
            controller.signal,
          );
      } catch {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) problem.value = true;
      } finally {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-condition -- onCleanup может прервать запрос во время await.
        if (!controller.signal.aborted) loading.value = false;
      }
    };
    void refresh();
  },
  { immediate: true },
);
</script>

<template>
  <section
    v-if="target || systemTarget"
    class="assistant-environment-card"
    aria-live="polite"
  >
    <header>
      <strong>{{ draftTitle }}</strong>
      <StatusBadge v-if="draft" :state="draft.state" />
    </header>
    <p v-if="loading && !draft">{{ $t("common.loading") }}</p>
    <p v-if="problem" class="assistant-environment-card__problem" role="alert">
      {{ $t("assistant.environmentDraft.loadFailed") }}
    </p>
    <template v-if="draft">
      <p>{{ draftName }}</p>
      <p v-if="draft.state === 'PUBLISHED'">
        {{ $t("assistant.environmentDraft.published") }}
      </p>
      <p v-else-if="draft.state === 'INVALID'">
        {{ $t("assistant.environmentDraft.invalid") }}
      </p>
      <p v-else-if="draft.state === 'DISCARDED'">
        {{ $t("assistant.environmentDraft.discarded") }}
      </p>
      <p v-else-if="draft.specification.imageArtifactRef">
        {{ $t("assistant.environmentDraft.incompleteWithImage") }}
      </p>
      <p v-else>{{ $t("assistant.environmentDraft.incomplete") }}</p>
      <p v-if="boundAgentName">
        {{ $t("assistant.environmentDraft.bound", { agent: boundAgentName }) }}
      </p>
      <template v-if="systemBindingMessage">
        <p role="status">{{ systemBindingMessage }}</p>
        <p class="secondary-text">
          {{ $t("assistant.environmentDraft.turnBoundary") }}
        </p>
      </template>
    </template>
    <div class="assistant-environment-card__actions">
      <button
        class="button"
        type="button"
        :disabled="loading"
        @click="refresh?.()"
      >
        {{ $t("common.refresh") }}
      </button>
      <RouterLink
        v-if="destination"
        class="button button--primary"
        :to="destination"
        @click="emit('navigate')"
      >
        {{ $t("assistant.environmentDraft.continue") }}
      </RouterLink>
      <button
        v-if="
          target &&
          draft?.state === 'PUBLISHED' &&
          draft.publishedEnvironmentRef
        "
        class="button button--primary"
        type="button"
        @click="bindingOpen = true"
      >
        {{ $t("assistant.environmentDraft.bind") }}
      </button>
    </div>
    <AssistantEnvironmentBindingDialog
      v-if="bindingOpen && target && draft?.publishedEnvironmentRef"
      :project-ref="target.projectRef"
      :environment-ref="draft.publishedEnvironmentRef"
      @close="bindingOpen = false"
      @bound="
        boundAgentName = $event;
        bindingOpen = false;
      "
    />
  </section>
</template>

<style scoped>
.assistant-environment-card {
  display: grid;
  gap: 8px;
  min-width: 0;
  margin-top: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--panel);
}
.assistant-environment-card header,
.assistant-environment-card__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}
.assistant-environment-card p {
  margin: 0;
  overflow-wrap: anywhere;
}
.assistant-environment-card__problem {
  color: var(--danger);
}
</style>
