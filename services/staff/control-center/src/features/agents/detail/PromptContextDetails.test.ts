import { renderToString } from "@vue/server-renderer";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { createI18n } from "vue-i18n";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PromptTemplatePreview } from "@/shared/api/generated/openapi/types.gen";
import type { AppProblem } from "@/shared/api/problem";
import PromptContextDetails from "./PromptContextDetails.vue";
import { promptContextMessages } from "./prompt-context-messages";

const longContent = `Начало ${"д".repeat(4096)} Конец`;
function preview(): PromptTemplatePreview {
  return {
    safePreview: "[protected input]",
    complete: true,
    diagnostics: [],
    templateRef: "preview_fixture",
    templateDigest: "a".repeat(64),
    materializationDigest: "b".repeat(64),
    effectiveCapabilities: [],
    serviceTemplateRevision: "prompt.v1",
    serviceTemplateDigest: "c".repeat(64),
    variableSnapshotDigest: "d".repeat(64),
    locale: "ru",
    slots: [{ source: "PLATFORM", slot: "WORKFLOW", position: 1 }],
    sections: [
      { source: "PLATFORM", slot: "WORKFLOW", content: "[WORKFLOW]" },
      { source: "USER_TEMPLATE", content: "**Безопасная цель**" },
      { source: "PLATFORM", slot: "INPUT", content: longContent },
    ],
    contextPin: { digest: "e".repeat(64) },
  };
}
function i18n() {
  return createI18n({
    legacy: false,
    locale: "ru",
    messages: {
      ru: {
        promptDetails: promptContextMessages.ru,
        promptContext: {
          copySection: "Скопировать содержимое блока {number}",
          sectionCopied: "Блок {number} скопирован",
        },
      },
    },
    missingWarn: false,
    fallbackWarn: false,
  });
}
async function render(value = preview()) {
  const app = createSSRApp(PromptContextDetails, { preview: value });
  app.use(i18n());
  return renderToString(app);
}
function visible(html: string): string {
  return html.replace(/<details\b[^>]*>[\s\S]*?<\/details>/gu, "");
}
type State = {
  copySection(index: number): Promise<void>;
  copiedSectionIndex: Ref<number | null>;
  copyingSectionIndex: Ref<number | null>;
  copyProblem: Ref<AppProblem | undefined>;
  isPlaceholder(content: string): boolean;
};
const writeText = vi.fn<(value: string) => Promise<void>>();
const disposers: (() => void)[] = [];
const completions: (() => void)[] = [];
function deferred() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  completions.push(resolve);
  return { promise, resolve, reject };
}
function mount() {
  const props = reactive({ preview: preview() });
  let state!: State;
  const source = PromptContextDetails as unknown as {
    setup(props: object, context: SetupContext): State;
  };
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
    patchProp() {},
  });
  const app = renderer.createApp(
    defineComponent({
      setup(_props, context) {
        state = source.setup(props, context);
        return () => null;
      },
    }),
  );
  app.use(i18n());
  app.provide(ssrContextKey, {});
  app.mount({});
  let mounted = true;
  const unmount = () => {
    if (!mounted) return;
    mounted = false;
    app.unmount();
  };
  disposers.push(unmount);
  return { props, state, unmount };
}
beforeEach(() => {
  writeText.mockReset().mockResolvedValue(undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText } });
});
afterEach(async () => {
  for (const dispose of disposers.splice(0)) dispose();
  for (const complete of completions.splice(0)) complete();
  await nextTick();
  vi.unstubAllGlobals();
});

describe("PromptContextDetails", () => {
  it("сразу показывает безопасные секции в авторитетном порядке", async () => {
    const html = visible(await render());
    expect(html).toContain("[WORKFLOW]");
    expect(html).toContain("Платформа · Процесс");
    expect(html).toContain("Пользовательский шаблон");
    expect(html).toContain("Безопасная цель");
    expect(html.indexOf("[WORKFLOW]")).toBeLessThan(
      html.indexOf("Безопасная цель"),
    );
    expect(html.indexOf("Безопасная цель")).toBeLessThan(
      html.indexOf("Начало"),
    );
  });

  it("делает placeholder доступной кнопкой копирования", async () => {
    const html = visible(await render());
    expect(html).toMatch(
      /<button\b[^>]*class="prompt-context-details__placeholder"[^>]*aria-label="[^"]+"[^>]*>[\s\S]*?\[WORKFLOW\][\s\S]*?<\/button>/u,
    );
  });

  it("сохраняет полный длинный текст в доступной области прокрутки", async () => {
    const html = visible(await render());
    expect(html).toContain(longContent);
    expect(html).toMatch(
      /class="prompt-context-details__content"[^>]*tabindex="0"/u,
    );
  });

  it("оставляет версии и дайджесты под катом без выдачи полного payload", async () => {
    const value = {
      ...preview(),
      fullMaterializedPrompt: "PRIVATE_FULL_PROMPT",
      privatePayload: "PRIVATE_RUNTIME_PAYLOAD",
    };
    const html = await render(value);
    expect(html).toContain(value.templateDigest);
    expect(html).toContain(value.contextPin?.digest);
    expect(visible(html)).not.toContain(value.templateDigest);
    expect(visible(html)).not.toContain(value.contextPin?.digest);
    expect(html).not.toContain("PRIVATE_FULL_PROMPT");
    expect(html).not.toContain("PRIVATE_RUNTIME_PAYLOAD");
  });

  it.each([0, 2])(
    "копирует exact safe content блока %s без усечения",
    async (index) => {
      const { props, state } = mount();
      await state.copySection(index);
      expect(writeText).toHaveBeenCalledExactlyOnceWith(
        props.preview.sections[index]?.content,
      );
      expect(state.copiedSectionIndex.value).toBe(index);
      expect(state.copyProblem.value).toBeUndefined();
      expect(state.copyingSectionIndex.value).toBeNull();
    },
  );

  it("показывает ошибку clipboard без ложного подтверждения копирования", async () => {
    writeText.mockRejectedValueOnce(new Error("Clipboard unavailable"));
    const { state } = mount();
    await state.copySection(0);
    expect(state.copiedSectionIndex.value).toBeNull();
    expect(state.copyProblem.value).toBeDefined();
    expect(state.copyingSectionIndex.value).toBeNull();
  });

  it("не запускает повторное копирование до завершения первого", async () => {
    const pending = deferred();
    writeText.mockReturnValueOnce(pending.promise);
    const { state } = mount();
    const copying = state.copySection(0);
    await state.copySection(1);
    expect(writeText).toHaveBeenCalledOnce();
    expect(state.copyingSectionIndex.value).toBe(0);
    pending.resolve();
    await copying;
  });

  it.each(["success", "error"] as const)(
    "замена preview не принимает поздний %s и не завершает новое копирование",
    async (outcome) => {
      const old = deferred();
      const fresh = deferred();
      writeText
        .mockReturnValueOnce(old.promise)
        .mockReturnValueOnce(fresh.promise);
      const { props, state } = mount();
      const oldCopy = state.copySection(0);
      props.preview = preview();
      const freshCopy = state.copySection(1);
      if (outcome === "success") old.resolve();
      else old.reject(new Error("Old clipboard failure"));
      await oldCopy;
      expect(state.copyingSectionIndex.value).toBe(1);
      expect(state.copiedSectionIndex.value).toBeNull();
      expect(state.copyProblem.value).toBeUndefined();
      fresh.resolve();
      await freshCopy;
      expect(state.copiedSectionIndex.value).toBe(1);
    },
  );

  it.each(["success", "error"] as const)(
    "unmount игнорирует поздний %s clipboard",
    async (outcome) => {
      const pending = deferred();
      writeText.mockReturnValueOnce(pending.promise);
      const { state, unmount } = mount();
      const copying = state.copySection(0);
      unmount();
      if (outcome === "success") pending.resolve();
      else pending.reject(new Error("Unmounted clipboard failure"));
      await copying;
      expect(state.copiedSectionIndex.value).toBeNull();
      expect(state.copyProblem.value).toBeUndefined();
      expect(state.copyingSectionIndex.value).toBeNull();
    },
  );

  it("копирует только существующую безопасную секцию, а не произвольный payload", async () => {
    const { state } = mount();
    await state.copySection(-1);
    await state.copySection(100);
    expect(writeText).not.toHaveBeenCalled();
  });

  it("различает placeholder и обычный Markdown без преобразования текста", () => {
    const { state } = mount();
    expect(state.isPlaceholder("[WORKFLOW]")).toBe(true);
    expect(state.isPlaceholder(" [AGENT.NAME] ")).toBe(true);
    expect(state.isPlaceholder("[ссылка](https://example.test)")).toBe(false);
    expect(state.isPlaceholder("Текст [WORKFLOW]")).toBe(false);
  });
});
