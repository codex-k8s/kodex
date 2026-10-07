import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { transpile } from "typescript";
import { computed, effectScope, nextTick, reactive, ref, watch } from "vue";
import { describe, expect, it, vi } from "vitest";

const source = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);
const prefillSource = source.slice(
  source.indexOf("const contextIdentity = computed"),
  source.indexOf("function hydrateFromRealtimeSnapshot"),
);
const draftWatcher =
  source.match(
    /watch\(\s*currentDraftKey,[\s\S]*?\{ flush: "sync" \},\s*\);/,
  )?.[0] ?? "";

class OpenEvent {
  constructor(public detail: unknown) {}
}

function workspace(assistantScope: "SYSTEM" | "PROJECT" = "SYSTEM") {
  const scope = effectScope();
  const store = reactive({
    assistantScope,
    activeAssistantRef: "assistant_one",
    selectedRef: "conversation_old" as string | undefined,
    loading: false,
  });
  const props = reactive({ context: "screen_one", projectRef: "project_one" });
  const platform = reactive({
    assistant: {} as object | undefined,
    assistantRealtimeScopeKey: "project_one",
  });
  const message = ref("");
  const messageDrafts = new Map<string, string>();
  const open = ref(true);
  const focus = vi.fn();
  const requestConfirmation = vi.fn(() => Promise.resolve(true));
  const bindings = scope.run(
    () =>
      runInNewContext(
        transpile(
          `${prefillSource}\n${draftWatcher}\n({handleOpenAssistant, prefillGeneration})`,
        ),
        {
          computed,
          ref,
          watch,
          nextTick,
          props,
          store,
          platform,
          message,
          messageDrafts,
          open,
          composer: ref({ focus }),
          requestConfirmation,
          assistantContextIdentity: (context: string) => context,
          CustomEvent: OpenEvent,
          show: async () => nextTick(),
          t: (key: string, values?: Record<string, unknown>) =>
            `${key}${values ? JSON.stringify(values) : ""}`,
          isAssistantSettingsRequest: () => false,
          isAssistantSetupRequest: (value: { kind?: string } | undefined) =>
            value?.kind === "SETUP",
          isAssistantRoleImageBuildDebugRequest: (
            value: { kind?: string } | undefined,
          ) => value?.kind === "ROLE_IMAGE_BUILD_DEBUG",
          isAssistantRunDebugRequest: (value: { kind?: string } | undefined) =>
            value?.kind === "RUN_DEBUG",
        },
      ) as {
        handleOpenAssistant: (event: OpenEvent) => void;
        prefillGeneration: { value: number };
      },
  );
  if (!bindings || !draftWatcher)
    throw new Error("Workspace prefill setup is missing");
  return {
    store,
    props,
    platform,
    message,
    open,
    focus,
    requestConfirmation,
    publish: (revisionRef = "mrev_one") =>
      bindings.handleOpenAssistant(
        new OpenEvent({ configurationRef: "mcfg_one", revisionRef }),
      ),
    dispose: () => {
      ++bindings.prefillGeneration.value;
      scope.stop();
    },
  };
}

async function settle() {
  for (let index = 0; index < 8; index += 1) await nextTick();
}

describe("AssistantWorkspace заполнение черновика", () => {
  it.each(["SYSTEM", "PROJECT"] as const)(
    "ждёт позднего выбора диалога %s и сохраняет изоляцию черновиков",
    async (assistantScope) => {
      const view = workspace(assistantScope);
      try {
        view.message.value = "Черновик прежнего диалога";
        view.store.loading = true;
        view.publish();
        await settle();
        expect(view.message.value).toBe("Черновик прежнего диалога");
        expect(view.requestConfirmation).not.toHaveBeenCalled();

        view.store.selectedRef = "conversation_restored";
        view.store.loading = false;
        await settle();
        const publication = view.message.value;
        expect(publication).toContain("assistant.publishIntegrationRequest");
        expect(publication).toContain('"configurationRef":"mcfg_one"');
        expect(view.requestConfirmation).not.toHaveBeenCalled();
        expect(view.focus).toHaveBeenCalledOnce();

        view.store.selectedRef = "conversation_old";
        expect(view.message.value).toBe("Черновик прежнего диалога");
        view.store.selectedRef = "conversation_restored";
        expect(view.message.value).toBe(publication);
      } finally {
        view.dispose();
      }
    },
  );

  it("ждёт первого SYSTEM снимка перед восстановлением выбранного диалога", async () => {
    const view = workspace();
    try {
      view.platform.assistant = undefined;
      view.store.selectedRef = undefined;
      view.publish();
      await settle();
      expect(view.message.value).toBe("");
      view.platform.assistant = {};
      view.store.selectedRef = "conversation_restored";
      await settle();
      expect(view.message.value).toContain(
        "assistant.publishIntegrationRequest",
      );
    } finally {
      view.dispose();
    }
  });

  it.each([true, false])(
    "подтверждает замену непустого черновика восстановленного диалога: %s",
    async (confirmed) => {
      const view = workspace();
      try {
        view.store.selectedRef = "conversation_restored";
        view.message.value = "Сохранённый черновик";
        view.store.selectedRef = "conversation_old";
        view.store.loading = true;
        view.requestConfirmation.mockResolvedValue(confirmed);
        view.publish();
        await settle();
        view.store.selectedRef = "conversation_restored";
        view.store.loading = false;
        await settle();
        expect(view.requestConfirmation).toHaveBeenCalledOnce();
        expect(view.message.value).toContain(
          confirmed
            ? "assistant.publishIntegrationRequest"
            : "Сохранённый черновик",
        );
      } finally {
        view.dispose();
      }
    },
  );

  it.each(["dialog", "text", "scope", "context", "close"])(
    "не применяет подтверждение после изменения %s",
    async (change) => {
      const view = workspace();
      let confirm!: (value: boolean) => void;
      try {
        view.message.value = "Исходный черновик";
        view.requestConfirmation.mockImplementation(
          () => new Promise<boolean>((resolve) => (confirm = resolve)),
        );
        view.publish();
        await settle();
        expect(view.requestConfirmation).toHaveBeenCalledOnce();
        if (change === "dialog") view.store.selectedRef = "conversation_other";
        if (change === "text") view.message.value = "Новый ручной текст";
        if (change === "scope") view.store.assistantScope = "PROJECT";
        if (change === "context") view.props.context = "screen_other";
        if (change === "close") view.open.value = false;
        const expectedMessage = view.message.value;
        confirm(true);
        await settle();
        expect(view.message.value).toBe(expectedMessage);
        expect(view.focus).not.toHaveBeenCalled();
      } finally {
        view.dispose();
      }
    },
  );

  it("при двух запросах во время загрузки применяет только последний", async () => {
    const view = workspace();
    try {
      view.store.loading = true;
      view.publish("mrev_first");
      await settle();
      view.publish("mrev_second");
      await settle();
      view.store.selectedRef = "conversation_restored";
      view.store.loading = false;
      await settle();
      expect(view.message.value).toContain("mrev_second");
      expect(view.message.value).not.toContain("mrev_first");
      expect(view.focus).toHaveBeenCalledOnce();
    } finally {
      view.dispose();
    }
  });

  it.each(["scope", "context", "close"])(
    "отменяет ожидающее заполнение после выхода и возврата: %s",
    async (change) => {
      const view = workspace();
      try {
        view.store.loading = true;
        view.publish();
        await settle();
        if (change === "scope") {
          view.store.assistantScope = "PROJECT";
          view.store.assistantScope = "SYSTEM";
        }
        if (change === "context") {
          view.props.context = "screen_other";
          view.props.context = "screen_one";
        }
        if (change === "close") {
          view.open.value = false;
          view.open.value = true;
        }
        view.store.loading = false;
        await settle();
        expect(view.message.value).toBe("");
        expect(view.requestConfirmation).not.toHaveBeenCalled();
        expect(view.focus).not.toHaveBeenCalled();
      } finally {
        view.dispose();
      }
    },
  );
});
