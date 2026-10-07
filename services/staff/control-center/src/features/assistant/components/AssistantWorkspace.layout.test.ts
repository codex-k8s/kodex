import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { compileStyle } from "@vue/compiler-sfc";
import { createSSRApp, effectScope, ref, watch } from "vue";
import { renderToString } from "vue/server-renderer";

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
const configurationPage = readFileSync(
  new URL("../../../pages/ConfigurationPage.vue", import.meta.url),
  "utf8",
);
const integrationsPage = readFileSync(
  new URL("../../../pages/IntegrationsPage.vue", import.meta.url),
  "utf8",
);
const historyFilter = readFileSync(
  new URL("./AssistantHistoryFilter.vue", import.meta.url),
  "utf8",
);
const attachmentComposer = readFileSync(
  new URL("../../../shared/ui/AttachmentComposer.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);
const styles = source.slice(source.indexOf("<style scoped>"));

describe("AssistantWorkspace layout", () => {
  it("использует один exact lifecycle turn для индикатора, Stop и очереди", () => {
    const lifecycle = source.slice(
      source.indexOf("const activeUserTurn = computed"),
      source.indexOf("const providerAccountRequired"),
    );
    expect(lifecycle).toContain("store.selectedConversation,");
    expect(lifecycle).toContain("platform.runs,");
    expect(lifecycle).toContain("platform.bootstrap?.organizationRef,");
    expect(lifecycle).toContain(
      "const awaitingReply = computed(() => Boolean(activeUserTurn.value))",
    );
    expect(lifecycle).toContain("const runRef = activeUserTurn.value?.runRef");
    expect(template).toContain('v-if="awaitingReply"');
    expect(template).toContain(
      'v-if="showWorkingFallback && !store.loading && !store.problem"',
    );
  });

  it.each(["plan", "form", "move"] as const)(
    "закрывает вложенный контекст до inert-перехода drawer: %s",
    (transition) => {
      const contextWatcher = source.match(
        /watch\(\s*\(\) =>\s*Boolean\(currentPlan\.value\) \|\|\s*assistantFormActive\.value \|\|\s*Boolean\(pendingProjectMove\.value\),[\s\S]*?\{ flush: "sync" \},\s*\);/,
      )?.[0];
      expect(contextWatcher).toBeDefined();
      if (!contextWatcher) throw new Error("Context inert watcher is missing");
      const currentPlan = ref<object>();
      const assistantFormActive = ref(false);
      const pendingProjectMove = ref<object>();
      const contextOpen = ref(true);
      const scope = effectScope();
      try {
        scope.run(() => {
          runInNewContext(contextWatcher, {
            watch,
            currentPlan,
            assistantFormActive,
            pendingProjectMove,
            contextOpen,
          });
        });
        expect(contextOpen.value).toBe(true);
        if (transition === "plan") currentPlan.value = {};
        if (transition === "form") assistantFormActive.value = true;
        if (transition === "move") pendingProjectMove.value = {};
        expect(contextOpen.value).toBe(false);
        currentPlan.value = undefined;
        assistantFormActive.value = false;
        pendingProjectMove.value = undefined;
        expect(contextOpen.value).toBe(false);
        contextOpen.value = true;
        expect(contextOpen.value).toBe(true);
      } finally {
        scope.stop();
      }
    },
  );

  it("сохраняет принятую переписку при обновлении, но не подменяет начальную загрузку и ошибку", async () => {
    const loadingBranch = template.match(
      /<div\s+v-else-if="store\.loading[^"]*"[\s\S]*?<\/div>/,
    )?.[0];
    expect(loadingBranch).toBeDefined();
    if (!loadingBranch) throw new Error("Loading branch is missing");
    expect(template).toContain('v-if="store.problem"');

    const cases = [
      {
        loading: true,
        selectedConversation: { turns: ["USER"] },
        expected: "transcript",
      },
      {
        loading: false,
        selectedConversation: { turns: ["USER"] },
        expected: "transcript",
      },
      { loading: true, selectedConversation: undefined, expected: "loading" },
      {
        loading: true,
        selectedConversation: undefined,
        problem: true,
        expected: "problem",
      },
      {
        loading: true,
        selectedConversation: { turns: ["USER"] },
        problem: true,
        expected: "problem",
      },
    ];
    for (const state of cases) {
      const app = createSSRApp({
        data: () => ({ store: state }),
        template: `<p v-if="store.problem" data-problem>problem</p>${loadingBranch}<p v-else data-transcript>transcript</p>`,
      });
      app.config.globalProperties.$t = () => "loading";
      const html = await renderToString(app);
      expect(html).toContain(state.expected);
      if (state.expected !== "loading") expect(html).not.toContain("spinner");
      if (state.expected !== "transcript")
        expect(html).not.toContain("data-transcript");
    }
  });

  it("не перекрывает отправку в панели запуска и действия в модалках", () => {
    const rule = styles
      .split(
        ':global(body:has([aria-modal="true"], .run-activity-overlay) .assistant-fab) {',
      )[1]
      ?.split("}")[0];
    expect(rule).toContain("visibility: hidden");
    expect(rule).toContain("pointer-events: none");
    const compiled = compileStyle({
      source: styles.replace("<style scoped>", "").replace("</style>", ""),
      filename: "AssistantWorkspace.vue",
      id: "data-v-test",
      scoped: true,
    });
    expect(compiled.errors).toEqual([]);
    expect(compiled.code).toContain(
      'body:has([aria-modal="true"], .run-activity-overlay) .assistant-fab {',
    );
    expect(compiled.code).not.toContain(
      'body:has([aria-modal="true"], .run-activity-overlay) {',
    );
    const formSlot = template
      .split('class="assistant-form-slot"')[1]
      ?.split(">\n")[0];
    expect(formSlot).toContain(
      ':aria-modal="open && assistantFormActive ? true : undefined"',
    );
  });
  it("называет главную страницу понятно и не дублирует маршрут в компактном контексте", () => {
    expect(source).toContain('route.name === "home"');
    expect(source).toContain('return t("nav.home")');
    const contextStrip = template
      .split('class="assistant-context-strip"')[1]
      ?.split("</button>")[0];
    expect(contextStrip).toContain("{{ contextTitle }}");
    expect(contextStrip).not.toContain("{{ context.route }}");
  });
  it("не создаёт пустой successful fallback bubble только при авторитетном terminal binding", () => {
    expect(source).toContain("assistantTurnIsEmptyTerminalReceipt(");
    expect(source).toContain("!turnIsEmptyTerminalReceipt(turn)");
    const rule = source.slice(
      source.indexOf("function turnIsEmptyTerminalReceipt"),
      source.indexOf("const transcriptTurns"),
    );
    expect(rule).toContain("store.selectedConversation");
    expect(rule).toContain("platform.bootstrap?.organizationRef");
    expect(rule).toContain("graph?.nodes ?? []");
    expect(rule).toContain("conversationRunEvents.value");
  });
  it("помещает применённый план в одну компактную карточку без внешнего повторного статуса", () => {
    expect(template).toContain("'assistant-message--applied-plan'");
    expect(template).toContain("turn.plan?.state !== 'APPLIED'");
    const applied = styles
      .slice(styles.indexOf(".assistant-message--applied-plan {"))
      .split("}")[0];
    expect(applied).toContain("padding: 0");
    expect(applied).toContain("border: 0");
    expect(applied).toContain("background: transparent");
    const record = template.slice(
      template.indexOf("<AssistantPlanRecord"),
      template.indexOf("</AssistantPlanRecord>"),
    );
    expect(record).toContain("turn.plan.state === 'APPLIED'");
    expect(record).toContain(':content="transcriptTurnContent(turn)"');
  });
  it("не дублирует exact активный transcript нижним working fallback", () => {
    expect(template).toContain(
      'v-if="showWorkingFallback && !store.loading && !store.problem"',
    );
    expect(source).toContain("assistantTranscriptReplacesWorkingFallback(");
    const typingStyle = styles
      .slice(styles.indexOf(".assistant-message--typing {"))
      .split("}")[0];
    expect(typingStyle).toContain("padding: 6px 10px");
    expect(typingStyle).toContain("border: 0");
    expect(source).toContain("activeUserTurn.value?.runRef");
    expect(template).toContain(
      "'assistant-composer__field--active': awaitingReply",
    );
  });
  it("объединяет owner-checked историю run в чат без отдельного cache и отпускает scoped subscriptions", () => {
    expect(source).toContain("Object.values(platform.events[runRef] ?? {})");
    expect(source).toContain("await platform.loadRun(runRef)");
    expect(source).toContain("realtime.acquireRun(runRef)");
    expect(source).toContain(
      "for (const release of transcriptLeases.values()) release()",
    );
    expect(template).toContain('v-for="entry in chatTimeline"');
    expect(template).toContain(':events="entry.events"');
    expect(template).toContain(
      ':active-item-id="entry.isolated ? null : chatActiveItemId"',
    );
    expect(source).toContain("buildAssistantChatTimeline(");
    expect(template).toContain(
      ':closed-execution-keys="closedTranscriptExecutionKeys"',
    );
    expect(source).toContain("assistantTerminalTranscriptScopes(");
    expect(source).toContain("platform.bootstrap?.organizationRef");
    expect(template).not.toContain("runs.unscopedHistory");
    expect(source).not.toContain("hasHistoricalTurns");
    expect(source).toContain("assistantTurnHasAuthoritativeActivity(");
    expect(source).toContain('turn.role === "ASSISTANT"');
    expect(source).toContain(
      "assistantFailureMessageKey(turn.content, turn.state)",
    );
    expect(template).toContain("turn.plan?.state !== 'APPLIED'");
  });

  it("сохраняет позицию чтения истории и предлагает кнопку новых сообщений", () => {
    expect(source).toContain("if (!chatFollowing.value)");
    expect(source).toContain("chatUnread.value = true");
    expect(template).toContain('@scroll.passive="onChatScroll"');
    expect(template).toContain('v-if="chatUnread"');
    expect(template).toContain('$t("runs.newMessages")');
  });
  it("подготовка настроек заполняет только черновик сообщения и не отправляет его", () => {
    const helper = source.slice(
      source.indexOf("function prepareAssistantSettings("),
      source.indexOf("function handleAssistantLink("),
    );
    expect(helper).toContain("message.value.trim()");
    expect(helper).toContain("suggestSetup(prompt)");
    expect(helper).not.toContain("store.send(");
    expect(helper).not.toContain("store.apply(");
    expect(template).toContain("prepareAssistantSettings('IMAGE')");
  });
  it("открывает точный run сообщения после подтверждённого закрытия чата", () => {
    expect(template).toContain('v-if="turn.runRef"');
    expect(template).toContain("openAssistantTurnRun(turn.runRef)");
    expect(source).toContain(
      "const target = runPath(runRef, conversation.projectRef)",
    );
    expect(source).toContain("if (open.value) return;");
  });
  it("явно разделяет system/project настройки, черновики и отправку", () => {
    expect(template).toContain('value="SYSTEM"');
    expect(template).toContain('value="PROJECT"');
    expect(source).toContain("store.assistantScope");
    expect(source).toContain("projectAssistantCanRun");
    expect(template).toContain("<AssistantProjectProfileSetup");
    expect(source).toContain('if (store.assistantScope !== "SYSTEM") return;');
    expect(source).toContain(
      "params: { projectRef: profile.projectRef, agentRef: profile.agentRef }",
    );
  });
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

  it("именует поля диалога для браузерной диагностики", () => {
    expect(historyFilter).toContain('name="assistant-history-search"');
    expect(historyFilter).toContain('name="assistant-history-state"');
    expect(template).toContain('name="assistant-message"');
    expect(attachmentComposer).toContain('name="attachments"');
  });

  it("даёт каждому диалогу меню действий и показывает пустую корзину", () => {
    expect(template).toContain("assistant-conversation-actions__toggle");
    expect(template).toContain('$t("assistant.deleteConversation")');
    expect(template).toContain('$t("assistant.restoreConversation")');
    expect(template).toContain('$t("assistant.purgeConversation")');
    expect(template).toContain('$t("assistant.emptyTrash")');
    expect(template).toContain('$t("assistant.trashEmpty")');
  });

  it("открывает большую desktop модалку с отдельной колонкой истории", () => {
    expect(styles).toMatch(
      /\.assistant-drawer\s*\{[\s\S]*?inset:\s*4dvh 4vw[\s\S]*?width:\s*92vw[\s\S]*?height:\s*92dvh/,
    );
    expect(template).not.toContain('v-if="!currentPlan"');
    expect(template).toContain('class="assistant-conversation-sidebar"');
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

  it("сжимает название на узком mobile, сохраняя кнопки header в одном ряду", () => {
    const mobile = styles.slice(
      styles.indexOf(
        "@media (max-width: 720px) {",
        styles.indexOf(".assistant-composer__protected-link:focus-visible"),
      ),
    );
    const identity = mobile
      .split(".assistant-drawer__identity {")[1]
      ?.split("}")[0];
    expect(identity).toContain("flex: 1 1 0");
    expect(identity).not.toContain("80px");
  });

  it("оставляет scroll только логу и закрепляет composer", () => {
    expect(styles).toMatch(
      /\.assistant-chat-log\s*\{[\s\S]*?flex:\s*1 1 auto[\s\S]*?overflow:\s*auto/,
    );
    expect(styles).toMatch(
      /\.assistant-composer\s*\{[\s\S]*?position:\s*sticky[\s\S]*?bottom:\s*0/,
    );
  });

  it("расширяет карточку плана, не растягивая обычные реплики", () => {
    expect(template).toContain(
      "{ 'assistant-message--with-plan': Boolean(turn.plan) }",
    );
    expect(styles).toMatch(
      /\.assistant-message--with-plan\s*\{[\s\S]*?width:\s*min\(96%, 1180px\)/,
    );
    expect(styles).toMatch(
      /\.assistant-message\s*\{[\s\S]*?width:\s*min\(86%, 760px\)/,
    );
    expect(template).not.toContain("assistant-plan-card__parameters");
  });

  it("fallback реплики владельца справа, агента и квитанции слева с переносом длинного текста", () => {
    const rule = (selector: string) =>
      styles.slice(styles.indexOf(`${selector} {`)).split("}")[0];
    expect(template).toContain(
      "`assistant-message--${turn.role.toLowerCase()}`",
    );
    expect(rule(".assistant-message")).toContain("margin-left: 0");
    expect(rule(".assistant-message")).toContain("margin-right: auto");
    expect(rule(".assistant-message")).toContain("max-width: 100%");
    expect(rule(".assistant-message")).toContain("overflow-wrap: anywhere");
    expect(rule(".assistant-message--user")).toContain("margin-left: auto");
    expect(rule(".assistant-message--user")).toContain("margin-right: 0");
    expect(rule(".assistant-message--system_receipt")).not.toContain(
      "width: 100%",
    );
    expect(styles.slice(styles.indexOf("@media (max-width: 720px)"))).toMatch(
      /\.assistant-message\s*\{\s*width: 94%/,
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

  it("оставляет подготовку запуска диагностики в текущем чате", () => {
    expect(template).toContain('@debug="suggestSetup"');
    expect(source).toContain("function suggestSetup(prompt: string)");
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
    expect(template).toContain(':assistant-return-path="route.fullPath"');
    expect(source).toContain("consumeRuntimeSecretReauthSuggestion");
    expect(source).toContain("workspaceMounted");
    expect(source).toContain("secretResumePending");
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

  it("показывает штатные редакторы поверх полного чата", () => {
    expect(template).toContain('id="assistant-form-slot"');
    expect(template).toContain('v-show="open && assistantFormActive"');
    expect(template).toContain('class="assistant-detail-backdrop"');
    expect(template).toContain('class="assistant-plan-dialog"');
    expect(template).toMatch(
      /:inert="\s*Boolean\(currentPlan\) \|\|\s*assistantFormActive \|\|\s*Boolean\(pendingProjectMove\) \|\|\s*undefined\s*"/,
    );
    expect(template).not.toContain(
      ':aria-hidden="Boolean(currentPlan) || assistantFormActive || undefined"',
    );
    expect(source).toMatch(
      /\.assistant-detail-backdrop\s*{[^}]*position: fixed;[^}]*inset: 0;[^}]*background:/s,
    );
    expect(source).toMatch(
      /\.assistant-plan-dialog,\s*\.assistant-form-slot\s*{[^}]*position: fixed;[^}]*inset: 6dvh 6vw;[^}]*border: 1px solid var\(--border\);[^}]*box-shadow:/s,
    );
    expect(source).toMatch(/\.assistant-form-slot\s*{[^}]*z-index: 72;/s);
    expect(source).toContain("max-height: 88dvh;");
    expect(source).toContain("transform: translateY(-50%);");
    expect(source).toContain("max-height: calc(88dvh - 124px);");
    expect(source).toContain('route.query.assistantForm === "1"');
    expect(source).toContain("watch(assistantFormActive, (active) => {");
    expect(source).toContain("if (active && !open.value) void show();");
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

  it("переключает открытый диалог только на realtime-снимок нового scope", () => {
    expect(source).toMatch(
      /watch\(contextIdentity,[\s\S]*if \(open\.value\) loadWorkspace\(\);\s*else store\.setContext\(props\.context, props\.projectRef\);/,
    );
    expect(source).toContain("function hydrateFromRealtimeSnapshot(): boolean");
    expect(source).toContain(
      "store.setContext(props.context, props.projectRef)",
    );
    expect(source).not.toContain(
      "await store.load(props.context, props.projectRef)",
    );
    expect(source).not.toContain("platform.reloadPlatformKind(kind)");
  });

  it("возвращает фокус к карточке варианта после закрытия редактора", () => {
    expect(template).toContain('@click="openPlan(turn.plan, $event)"');
    expect(source).toContain(
      "planTrigger.value = event.currentTarget as HTMLButtonElement",
    );
    expect(template).toContain('ref="planDialog"');
    expect(source).toContain("focusableElements(planDialog.value)[0]");
    expect(source).toContain("if (trigger?.isConnected) trigger.focus()");
    expect(template).toContain('["APPLIED", "REJECTED"].includes');
    expect(template).toContain("assistant.viewPlan");
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
    expect(source).toContain('query: { assistantForm: "1" }');
    expect(source).not.toContain("if (!open.value)");
    expect(configurationPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
    expect(configurationPage).toContain(
      '...(assistantForm.value ? { assistantForm: "1" } : {})',
    );
    expect(appShell).toContain(
      'route.params.kind === "INTEGRATION_DEFINITION"',
    );
  });

  it("сохраняет помощника при просмотре образа и интеграционного подключения", () => {
    expect(template).toContain("<AssistantRoleImageBuildCard");
    expect(integrationsPage).toContain(
      '<Teleport to="#assistant-form-slot" :disabled="!assistantForm" defer>',
    );
    expect(source).toContain("<AssistantIntegrationConnectionCard");
    expect(source).not.toContain(
      '<AssistantIntegrationConnectionCard @navigate="close"',
    );
    expect(template).toContain("item.type === 'UPDATE_INTEGRATION_CONNECTION'");
    expect(template).toContain("item.type === 'TEST_INTEGRATION_CONNECTION'");
    expect(template).toContain("item.type === 'UPDATE_PROJECT'");
    expect(template).toContain("item.type === 'UPDATE_AGENT'");
  });

  it("после запроса доработки возвращает в диалог без отправки за пользователя", () => {
    expect(source).toContain("async function requestPlanChanges()");
    expect(source).toContain("await closePlan()");
    expect(source).toContain("assistant.planEditor.revisionRequest");
    expect(source).toContain("planVariantNumber(plan.ref)");
    expect(template).toContain("assistant.planVariant");
    expect(source).toContain("composer.value?.focus()");
    expect(template).toContain('@request-changes="requestPlanChanges"');
  });

  it("сохраняет DOM чата под формой плана и не закрывает занятое применение", () => {
    expect(template).toContain('<div class="assistant-workspace-content">');
    expect(template).not.toContain(
      '<template>\n        <nav v-if="isRunContext"',
    );
    expect(template).toContain('key="CHAT"');
    expect(template).toContain('v-if="currentPlan && !assistantFormActive"');
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

  it("изолирует черновики по диалогам и временно именует новый диалог", () => {
    expect(source).toContain("const messageDrafts = new Map<string, string>()");
    expect(source).toContain("const currentDraftKey = computed(");
    expect(source).toContain("messageDrafts.set(previous, message.value)");
    expect(source).toContain('message.value = messageDrafts.get(next) ?? ""');
    expect(source).toContain("temporaryConversationTitle(conversationRef)");
    expect(source).toContain("normalized.length < 12");
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
    expect(sendAccess).toContain("!store.loading");
    expect(sendAccess).toContain(
      "store.selectedConversation || canCreateConversation.value",
    );
    expect(sendAccess).not.toContain("store.problem");
    expect(startAccess).toContain("store.conversationCreationReady");
    expect(startAccess).toContain("props.live");
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

  it("показывает в краткой карточке плана действие и цель без технических параметров", () => {
    expect(template).toContain('class="assistant-plan-card__action"');
    expect(template).toContain("operationActionLabel(operation.action)");
    expect(template).toContain('class="assistant-plan-card__target"');
    expect(template).toContain("operationTargetLabel(operation.target)");
    expect(template).toContain(
      "operationTargetKindLabel(operation.target.kind)",
    );
    expect(template).toContain("operationSupportingTitle(operation)");
    expect(source).toContain(
      'PROJECT: "assistant.planEditor.targetKinds.PROJECT"',
    );
    expect(template).not.toContain("operation.parameters");
    expect(template).toMatch(
      /turn\.plan\.auditSummary\.trim\(\) !==\s*turn\.content\.trim\(\)/,
    );
  });

  it("экспонирует стабильную последовательность turn для realtime и E2E", () => {
    expect(template).toContain(':data-turn-ref="turn.ref"');
    expect(template).toContain(':data-turn-sequence="turn.sequence"');
  });
});
