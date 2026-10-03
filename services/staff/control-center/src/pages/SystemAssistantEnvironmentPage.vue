<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { useRoute } from "vue-router";
import { usePlatformStore } from "@/features/platform/store";
import AssistantEnvironmentSettingsPanel from "@/features/assistant/components/AssistantEnvironmentSettingsPanel.vue";
import { organizationRuntimeResourceScope } from "@/features/runtime/resource-scope";
import { createRuntimeResourceCatalogs } from "@/features/runtime/resource-catalog-api";
import { parseOrganizationAssistantEnvironmentIntent } from "@/features/session/reauth";
import {
  getSystemAssistant,
  getAgentRuntimeConfiguration,
} from "@/shared/api/generated/openapi/sdk.gen";
import { unwrap, asProblem, type AppProblem } from "@/shared/api/problem";
import PageFrame from "@/shared/ui/PageFrame.vue";
import AsyncState from "@/shared/ui/AsyncState.vue";
const platform = usePlatformStore(),
  route = useRoute();
const scope = computed(() =>
  organizationRuntimeResourceScope(platform.bootstrap),
);
const catalogs = computed(() =>
  scope.value
    ? createRuntimeResourceCatalogs(scope.value.organizationRef)
    : undefined,
);
const assistant = ref<Awaited<ReturnType<typeof getSystemAssistant>>["data"]>();
const problem = ref<AppProblem>(),
  loading = ref(false);
const expectedDraftVersion = ref<number>();
const draftRef = computed(() =>
  typeof route.query.draftRef === "string" &&
  /^[A-Za-z0-9_-]{8,128}$/.test(route.query.draftRef)
    ? route.query.draftRef
    : undefined,
);
let controller = new AbortController();
async function load(): Promise<void> {
  controller.abort();
  controller = new AbortController();
  const active = controller;
  if (!scope.value) return;
  loading.value = true;
  problem.value = undefined;
  try {
    const value = (await unwrap(getSystemAssistant({ signal: active.signal })))
      .data;
    if (active.signal.aborted) return;
    const metadata = window.sessionStorage.getItem(
      "kodex.organization-assistant.environment-resume",
    );
    if (metadata) {
      window.sessionStorage.removeItem(
        "kodex.organization-assistant.environment-resume",
      );
      const intent = parseOrganizationAssistantEnvironmentIntent(
        JSON.parse(metadata),
      );
      const runtime = (
        await unwrap(
          getAgentRuntimeConfiguration({
            path: { agentRef: value.ref },
            signal: active.signal,
          }),
        )
      ).data;
      if (
        intent.organizationRef !== scope.value.organizationRef ||
        intent.agentRef !== value.ref ||
        intent.draftRef !== draftRef.value ||
        intent.environmentRef !== runtime.environment.ref ||
        runtime.configuration.agentRef !== value.ref
      )
        throw new Error(
          "Assistant environment reauthentication scope mismatch",
        );
      expectedDraftVersion.value = intent.draftVersion;
    }
    assistant.value = value;
  } catch (error) {
    if (!active.signal.aborted) problem.value = asProblem(error);
  } finally {
    if (!active.signal.aborted) loading.value = false;
  }
}
watch(
  () => scope.value?.organizationRef,
  () => void load(),
  { immediate: true },
);
onBeforeUnmount(() => controller.abort());
</script>
<template>
  <PageFrame
    :title="$t('assistant.resources.environment')"
    :subtitle="$t('assistant.resources.organizationHelp')"
  >
    <AsyncState :loading="loading" :problem="problem" @retry="load">
      <AssistantEnvironmentSettingsPanel
        v-if="assistant && scope && catalogs"
        :agent-ref="assistant.ref"
        :can-edit="assistant.nextActions.includes('EDIT')"
        :resource-scope="scope"
        :image-catalog="catalogs.images"
        :secret-catalog="catalogs.secrets"
        :initial-draft-ref="draftRef"
        :expected-draft-version="expectedDraftVersion"
        :return-to="route.fullPath"
      />
    </AsyncState>
  </PageFrame>
</template>
