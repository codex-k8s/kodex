import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);
const appShell = readFileSync(
  new URL("../../../app/AppShell.vue", import.meta.url),
  "utf8",
);
const environmentPage = readFileSync(
  new URL("../../../pages/RuntimeEnvironmentEditorPage.vue", import.meta.url),
  "utf8",
);
const roleImagePage = readFileSync(
  new URL("../../../pages/RoleImageEditorPage.vue", import.meta.url),
  "utf8",
);
const workflowPage = readFileSync(
  new URL("../../../pages/WorkflowDetailPage.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);
const styles = source.slice(source.indexOf("<style scoped>"));

describe("AssistantWorkspace layout", () => {
  it("сохраняет controls диалога в постоянном header", () => {
    const header = template.indexOf(
      '<header class="assistant-drawer__header">',
    );
    const planEditor = template.indexOf("<AssistantPlanEditor");
    const headerMarkup = template.slice(header, planEditor);

    expect(header).toBeGreaterThan(-1);
    expect(header).toBeLessThan(planEditor);
    expect(headerMarkup).toContain(
      ":aria-label=\"$t('assistant.newConversation')\"",
    );
    expect(headerMarkup).toContain(":aria-label=\"$t('assistant.history')\"");
    expect(headerMarkup).toContain(":aria-label=\"$t('common.close')\"");
  });

  it("открывает большую desktop модалку с отдельной колонкой истории", () => {
    expect(styles).toMatch(
      /\.assistant-drawer\s*\{[\s\S]*?inset:\s*4dvh 4vw[\s\S]*?width:\s*92vw[\s\S]*?height:\s*92dvh/,
    );
    expect(template).toContain("'assistant-drawer--plan': currentPlan");
    expect(styles).toMatch(
      /\.assistant-drawer--plan\s*\{[\s\S]*?inset:\s*4dvh 4vw[\s\S]*?width:\s*92vw[\s\S]*?height:\s*92dvh/,
    );
  });

  it("переключает modal в полноэкранный mobile", () => {
    const mobile = styles.slice(styles.indexOf("@media (max-width: 720px)"));

    expect(mobile).toMatch(/\.assistant-drawer\s*\{[\s\S]*?left:\s*0/);
    expect(mobile).toMatch(/top:\s*auto/);
    expect(mobile).toMatch(/bottom:\s*0/);
    expect(mobile).toMatch(/width:\s*100%/);
    expect(mobile).toMatch(/height:\s*100dvh/);
    expect(mobile).toMatch(/border-radius:\s*0/);
  });

  it("оставляет scroll только логу и закрепляет composer", () => {
    expect(styles).toMatch(
      /\.assistant-chat-log\s*\{[\s\S]*?flex:\s*1 1 auto[\s\S]*?overflow:\s*auto/,
    );
    expect(styles).toMatch(
      /\.assistant-composer\s*\{[\s\S]*?position:\s*sticky[\s\S]*?bottom:\s*0/,
    );
  });

  it("передаёт точный Project context в файловый composer", () => {
    const attachmentComposer = template.slice(
      template.indexOf("<AttachmentComposer"),
      template.indexOf("</footer>"),
    );

    expect(attachmentComposer).toContain(':project-ref="projectRef"');
    expect(attachmentComposer).toContain('purpose="ASSISTANT_MESSAGE"');
  });

  it("открывает защищённую форму секрета только в текущем проекте", () => {
    const composer = template.slice(
      template.indexOf('<footer class="assistant-composer">'),
      template.indexOf("</footer>"),
    );
    expect(composer).toContain('v-if="projectRef"');
    expect(composer).toContain('@click="openPlainSecretForm"');
    expect(source).toMatch(
      /function openPlainSecretForm\(\): void \{[\s\S]*?secretDialogOpen\.value = true/,
    );
    expect(template).toContain('<Teleport to="body">');
    expect(template).toContain("<RuntimeSecretDraftDialog");
    expect(template).toContain(':project-ref="projectRef"');
    expect(template).toContain(':initial-draft-ref="secretInitialDraftRef"');
    expect(template).toContain("assistant\n");
    expect(template).toContain('v-if="open && secretDialogOpen && projectRef"');
    expect(template).toMatch(
      /:inert="\s*integrationImportOpen\s*\|\|\s*secretDialogOpen/,
    );
    expect(composer).not.toContain("name: 'runtime-secrets'");
    expect(composer).not.toContain("credentialValue");
    expect(composer).not.toContain("secretValue");
  });

  it("не закрывает чат при защищённом вводе credential подключения", () => {
    const credentialDialog = readFileSync(
      new URL("./AssistantIntegrationCredentialDialog.vue", import.meta.url),
      "utf8",
    );
    const manual = readFileSync(
      new URL("../../../pages/IntegrationsPage.vue", import.meta.url),
      "utf8",
    );
    expect(template).toContain(
      '@prepare-credential="credentialConnectionRef = $event"',
    );
    expect(template).toContain("<AssistantIntegrationCredentialDialog");
    expect(template).toMatch(
      /:inert="\s*integrationImportOpen\s*\|\|\s*secretDialogOpen\s*\|\|\s*Boolean\(credentialConnectionRef\)/,
    );
    expect(credentialDialog).toContain("getIntegrationConnection");
    expect(credentialDialog).toContain("canConfigureCredential");
    expect(credentialDialog).toContain(
      "fresh.version !== connection.value.version",
    );
    expect(credentialDialog).toContain('credentialValue.value = ""');
    expect(credentialDialog).toContain("<IntegrationCredentialField");
    expect(manual).toContain("<IntegrationCredentialField");
    expect(credentialDialog).not.toContain("assistantCredentialRef");
  });

  it("показывает ручные редакторы окружения, образа и процесса рядом с чатом", () => {
    expect(template).toContain('id="assistant-form-slot"');
    expect(template).toContain(
      "'assistant-drawer--with-form': assistantFormActive",
    );
    expect(source).toContain('route.query.assistantForm === "1"');
    expect(template).toContain('@click="closeAssistantForm"');
    expect(appShell).toContain("assistantStore.context");
    expect(environmentPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
    expect(roleImagePage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
    expect(workflowPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
  });

  it("открывает защищённый импорт OpenAPI поверх диалога без передачи документа модели", () => {
    expect(template).toContain('@click.capture="handleAssistantLink"');
    expect(source).toContain('"/configurations/INTEGRATION_DEFINITION"');
    expect(source).toContain("integrationImportOpen.value = true");
    expect(template).toContain("<OpenAPIImportDialog");
    expect(template).toContain('@created="integrationDraftCreated"');
    expect(source).not.toContain("message.value = source");
    expect(source).not.toContain("store.send(source");
  });

  it("после импорта предлагает проверить и опубликовать созданную ревизию", () => {
    expect(template).toContain('v-if="createdDefinitionRef"');
    expect(template).toContain("assistant.integrationDraftCreated");
    expect(template).toContain("assistant.openIntegrationDraft");
    expect(source).toContain('name: "configuration"');
    expect(source).toContain('kind: "INTEGRATION_DEFINITION"');
  });

  it("закрывает помощника при переходе к созданному образу", () => {
    expect(template).toContain("<AssistantRoleImageBuildCard");
    expect(template).toContain('@navigate="close"');
  });

  it("после запроса доработки возвращает в диалог без отправки за пользователя", () => {
    expect(source).toContain("async function requestPlanChanges()");
    expect(source).toContain("await closePlan()");
    expect(source).toContain("assistant.planEditor.revisionRequest");
    expect(source).toContain("composer.value?.focus()");
    expect(template).toContain('@request-changes="requestPlanChanges"');
  });

  it("разделяет DOM экрана плана и чата и не закрывает занятое применение", () => {
    expect(template).toContain(":key=\"currentPlan ? 'PLAN' : 'CHAT'\"");
    expect(source).toContain("if (store.busy) return;");
  });

  it("показывает этапы настройки и только подставляет запрос в composer", () => {
    expect(source).toContain(
      '["agent", "environment", "integration", "launch"]',
    );
    expect(source).toContain('["project"]');
    expect(source).toContain('class="assistant-setup-guide"');
    expect(source).toContain("message.value = prompt");
    expect(source).not.toContain("store.send(prompt");
  });

  it("держит новый диалог видимым действием, а не пунктом history menu", () => {
    const header = template.slice(
      template.indexOf('<header class="assistant-drawer__header">'),
      template.indexOf("<AssistantPlanEditor"),
    );

    expect(header).toContain('class="assistant-new-conversation"');
    expect(header).toContain('{{ $t("assistant.newConversation") }}');
    expect(header).toContain('class="icon-button assistant-history__toggle"');
  });

  it("разрешает новый диалог после ошибки истории только готовому assistant", () => {
    const createAccess = source.slice(
      source.indexOf("const canCreateConversation"),
      source.indexOf("const canSend"),
    );
    const sendAccess = source.slice(
      source.indexOf("const canSend"),
      source.indexOf("const canStartConversation"),
    );
    const startAccess = source.slice(
      source.indexOf("const canStartConversation"),
      source.indexOf("const isRunContext"),
    );

    expect(createAccess).toContain('assistantRuntimeState.value === "READY"');
    expect(createAccess).toContain(
      'nextActions.includes("CREATE_CONVERSATION")',
    );
    expect(sendAccess).toContain('nextActions.includes("ADD_TURN")');
    expect(sendAccess).toContain(
      "store.selectedConversation || canCreateConversation.value",
    );
    expect(sendAccess).not.toContain("store.problem");
    expect(startAccess).toContain("!store.loading");
    expect(startAccess).toContain("!store.busy");
    expect(startAccess).toContain("canCreateConversation.value");
    expect(startAccess).not.toContain("store.problem");
    expect(template).toContain('v-if="store.problem"');
    expect(template).toContain(':aria-busy="store.busy || store.loading"');
    expect(source).toContain(
      "await handleStoreMutation(() => store.startConversation())",
    );
    expect(source).toContain("if (!(error instanceof AppProblem)) throw error");
  });

  it("блокирует готовность composer до завершения server-side scan", () => {
    expect(source).toContain("attachmentComposer.value?.finalize()");
    expect(source).toContain("attachmentState.value.ready");
  });

  it("ведёт provider-free first-run к авторизации без включения диалога", () => {
    expect(source).toContain("assistantRequiresProviderAccount");
    expect(template).toContain('v-if="providerAccountRequired"');
    expect(template).toContain("assistant.providerAccountRequiredHelp");
    expect(template).toContain(":to=\"{ name: 'provider-accounts' }\"");
    expect(template).toContain("assistant.openProviderAccounts");
  });

  it("показывает в карточке плана действие, target и все явные параметры", () => {
    expect(template).toContain('class="assistant-plan-card__action"');
    expect(template).toContain("operationActionLabel(operation.action)");
    expect(template).toContain('class="assistant-plan-card__target"');
    expect(template).toContain("operationTargetLabel(operation.target)");
    expect(template).toContain(
      '<SafeStructuredData :value="operation.parameters" />',
    );
  });

  it("экспонирует стабильную последовательность turn для realtime и E2E", () => {
    expect(template).toContain(':data-turn-ref="turn.ref"');
    expect(template).toContain(':data-turn-sequence="turn.sequence"');
  });
});
