<script setup lang="ts">
import { nextTick, ref } from "vue";
import AssistantWorkspace from "../../src/features/assistant/components/AssistantWorkspace.vue";
import { useAssistantStore } from "../../src/features/assistant/store";
import AsyncEntityPicker from "../../src/shared/ui/AsyncEntityPicker.vue";
import RunsBoard from "../../src/features/workboard/components/RunsBoard.vue";
import type {
  Agent,
  AssistantConversation,
  AssistantPlan,
  ProjectAssistantProfile,
} from "../../src/shared/api/generated/openapi/types.gen";
import DismissiblePopover from "../../src/shared/ui/DismissiblePopover.vue";
import IntegrationsPage from "../../src/pages/IntegrationsPage.vue";
import { usePlatformStore } from "../../src/features/platform/store";
import { client } from "../../src/shared/api/generated/openapi/client.gen";
const lifecycleFixture =
  new URLSearchParams(location.search).get("fixture") === "popover-lifecycle";
const lifecycleOpen = ref(false);
const lifecycleMounted = ref(true);
async function race(mode: "close" | "unmount" | "reopen") {
  lifecycleMounted.value = true;
  lifecycleOpen.value = true;
  await nextTick();
  lifecycleOpen.value = false;
  if (mode === "unmount") lifecycleMounted.value = false;
  await nextTick();
  if (mode === "reopen") lifecycleOpen.value = true;
}
const integrationFixture =
  new URLSearchParams(location.search).get("fixture") ===
  "integration-terminal";
const assistantImportFixture = [
  "assistant-openapi-import",
  "assistant-openapi-create",
].includes(new URLSearchParams(location.search).get("fixture") ?? "");
const projectAssistantFixture =
  new URLSearchParams(location.search).get("fixture") === "project-assistant";
if (integrationFixture || assistantImportFixture || projectAssistantFixture) {
  const platform = usePlatformStore();
  platform.loadIntegrations = () => Promise.resolve();
  platform.loadProjects = () => Promise.resolve();
  client.setConfig({ baseUrl: location.origin });
}
const context = {
  route: "/projects/project_fixture",
  entityKind: "PROJECT",
  entityRef: "project_fixture",
  entityName: "Проект",
  allowedOperations: [],
};
const assistant = useAssistantStore();
const historyRequests = ref(0);
const pickerRequests = ref(0);
const selected = ref<string | null>(null);
function conversation(index: number): AssistantConversation {
  return {
    ref: `conversation_${String(index)}`,
    assistantScope: "SYSTEM",
    assistantRef: "assistant_fixture",
    state: "ACTIVE",
    projectRef: "project_fixture",
    version: 1,
    title: `Диалог ${String(index)}`,
    titleSource: "USER_EDITED",
    titleRevision: 1,
    context: {
      ...context,
      entityVersion: 1,
      entityName: "Длинное понятное название проекта для проверки контекста",
      allowedOperations: [],
    },
    turns:
      assistantImportFixture && index === 0
        ? [
            {
              ref: "turn_openapi_import_fixture",
              sequence: 1,
              role: "ASSISTANT",
              content:
                "Откройте [защищённую форму импорта](/configurations/INTEGRATION_DEFINITION). Не отправляйте документ в чат.",
              state: "COMPLETED",
              createdAt: "2026-09-25T00:00:00Z",
            },
          ]
        : [],
    updatedAt: `2026-09-08T10:${String(index).padStart(2, "0")}:00Z`,
  };
}
assistant.assistant = {
  ref: "assistant_fixture",
  version: 1,
  name: "Kodex",
  system: true,
  removable: false,
  corePromptRevision: "1",
  ownerInstructions: "",
  runtimeState: "READY",
  readinessSummary: "",
  nextActions: [],
};
assistant.conversations = Array.from({ length: 30 }, (_, index) =>
  conversation(index),
);
assistant.selectedRef = "conversation_0";
assistant.nextPageToken = "second";
assistant.load = () => Promise.resolve();
if (projectAssistantFixture) {
  const profile: ProjectAssistantProfile = {
    ref: "asstp_fixture",
    projectRef: "project_fixture",
    agentRef: "agent_helper_fixture",
    name: "Помощник проекта разработки платформы",
    state: "ACTIVE",
    version: 1,
    createdAt: "2026-10-04T00:00:00Z",
    updatedAt: "2026-10-04T00:00:00Z",
  };
  const agent: Agent = {
    ref: profile.agentRef,
    projectRef: profile.projectRef,
    name: profile.name,
    version: 1,
    purpose: "Помощь проекту",
    roleDescription: "Помощник",
    state: "DRAFT",
    enabled: true,
    system: false,
    runtimeRef: "runtime_fixture",
    runtimeName: "Базовый",
    runtimeReady: false,
    capabilities: [],
    integrations: [],
    knowledgeArtifactRefs: [],
    nextActions: ["EDIT"],
    updatedAt: profile.updatedAt,
  };
  const plan: AssistantPlan = {
    ref: "plan_helper_fixture",
    version: 2,
    revision: 1,
    state: "APPLIED",
    conversationRef: "conversation_0",
    projectRef: profile.projectRef,
    auditSummary: "Создать помощника",
    applied: true,
    contentDigest: "a".repeat(64),
    validationProblems: [],
    nextActions: [],
    operations: [
      {
        ref: "operation_helper_fixture",
        type: "CREATE_PROJECT_ASSISTANT",
        action: "CREATE",
        title: "Помощник проекта",
        summary: "Создать отдельную конфигурацию помощника",
        target: { kind: "PROJECT_ASSISTANT", name: profile.name },
        parameters: {
          name: profile.name,
          purpose: agent.purpose,
          instructions: "Работай в текущем проекте.",
        },
        before: {},
        after: {},
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
    receipt: {
      ref: "receipt_helper_fixture",
      planRef: "plan_helper_fixture",
      planRevision: 1,
      outcome: "APPLIED",
      operationReceipts: [
        {
          operationRef: "operation_helper_fixture",
          resourceRef: profile.ref,
          outcome: "APPLIED",
          auditRef: "audit_helper_fixture",
        },
      ],
      conflicts: [],
      auditRefs: ["audit_helper_fixture"],
      createdResourceRefs: [profile.ref],
      createdAt: profile.createdAt,
    },
  };
  assistant.setContext(context, profile.projectRef);
  assistant.assistantScope = "PROJECT";
  assistant.projectAssistant = profile;
  assistant.projectAssistantAgent = agent;
  assistant.conversations = [
    {
      ...conversation(0),
      assistantScope: "PROJECT",
      assistantRef: agent.ref,
      assistantProfileRef: profile.ref,
      turns: [
        {
          ref: "turn_helper_fixture",
          sequence: 1,
          role: "ASSISTANT",
          content: "Помощник создан. Настройте его перед запуском.",
          state: "COMPLETED",
          plan,
          createdAt: profile.createdAt,
        },
      ],
    },
  ];
}
assistant.loadMoreHistory = async () => {
  if (!assistant.nextPageToken || assistant.loadingMore) return;
  assistant.loadingMore = true;
  historyRequests.value++;
  await new Promise((resolve) => setTimeout(resolve, 20));
  assistant.conversations.push(conversation(30));
  assistant.nextPageToken = undefined;
  assistant.loadingMore = false;
};
async function loadPage(_query: string, cursor: string | undefined) {
  await Promise.resolve();
  pickerRequests.value++;
  return {
    items: Array.from({ length: 30 }, (_, index) => ({
      ref: `item_${cursor ?? "first"}_${String(index)}`,
      title: `Элемент ${String(index)}`,
      description: "Подробное назначение выбранного элемента",
    })),
    nextPageToken: cursor ? undefined : "second",
  };
}
</script>
<template>
  <IntegrationsPage v-if="integrationFixture" />
  <main v-else-if="lifecycleFixture">
    <button @click="race('close')">Race close</button>
    <button @click="race('unmount')">Race unmount</button>
    <button @click="race('reopen')">Race reopen</button>
    <DismissiblePopover
      v-if="lifecycleMounted"
      :open="lifecycleOpen"
      ariaLabel="Race"
      @update:open="lifecycleOpen = $event"
    >
      <template #trigger="{ toggle, attrs }"
        ><button v-bind="attrs" @click="toggle">Toggle race</button></template
      >
      <input aria-label="Race input" />
    </DismissiblePopover>
  </main>
  <main v-else style="padding: 24px; min-width: 0">
    <output data-history-requests>{{ historyRequests }}</output>
    <output data-picker-requests>{{ pickerRequests }}</output>
    <AsyncEntityPicker
      :model-value="selected"
      :load-page="loadPage"
      trigger-label="Выбор"
      placeholder="Выбор"
      search-placeholder="Поиск"
      @update:model-value="
        selected = typeof $event === 'string' ? $event : null
      "
    />
    <RunsBoard :runs="[]" />
    <AssistantWorkspace
      :context="context"
      project-ref="project_fixture"
      :live="projectAssistantFixture"
    />
  </main>
</template>
