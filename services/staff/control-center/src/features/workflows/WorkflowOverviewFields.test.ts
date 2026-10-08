import { renderToString } from "@vue/server-renderer";
import {
  createRenderer,
  createSSRApp,
  defineComponent,
  h,
  nextTick,
  reactive,
  ssrContextKey,
  type Ref,
  type SetupContext,
} from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
vi.mock("@/shared/ui/AsyncEntityPicker.vue", () => ({
  default: defineComponent({ render: () => h("span") }),
}));
vi.mock("@/shared/ui/VoiceInputButton.vue", () => ({
  default: defineComponent({ render: () => null }),
}));
import Component from "./WorkflowOverviewFields.vue";

function fields(completionCriteria: string) {
  return {
    name: "Процесс",
    purpose: "Проверка",
    coordinatorAgentRef: "agt_fixture",
    projectRef: "prj_fixture",
    timeoutSeconds: 1800,
    maxConcurrency: 1,
    completionCriteria,
    loadAgents: () => Promise.resolve({ items: [], hasMore: false }),
  };
}

const disposers: Array<() => void> = [];
afterEach(() => disposers.splice(0).forEach((dispose) => dispose()));
function mount(completionCriteria: string) {
  const props = reactive(fields(completionCriteria));
  const events: string[] = [];
  let state!: {
    completionCriteriaLength: Ref<number>;
    completionCriteriaTooLong: Ref<boolean>;
    completionCriteriaDescription: Ref<string>;
  };
  const renderer = createRenderer<object, object>({
    insert() {},
    remove() {},
    patchProp() {},
    createElement: () => ({}),
    createText: () => ({}),
    createComment: () => ({}),
    setText() {},
    setElementText() {},
    parentNode: () => null,
    nextSibling: () => null,
  });
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          state = (
            Component as unknown as {
              setup: (props: object, context: SetupContext) => typeof state;
            }
          ).setup(props, {
            ...context,
            emit: (_name: string, value: string) => events.push(value),
          });
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  return { props, events, state };
}

describe("лимит критерия завершения процесса", () => {
  it.each(["ru", "en"] as const)(
    "показывает счётчик и понятную ошибку рядом с полем (%s)",
    async (locale) => {
      const previous = i18n.global.locale.value;
      i18n.global.locale.value = locale;
      try {
        const value = "я".repeat(2286);
        const html = await renderToString(
          createSSRApp(Component, fields(value)).use(i18n),
        );
        expect(html).toContain(value);
        expect(html).not.toContain('maxlength="2000"');
        expect(html).toContain('aria-invalid="true"');
        const label = html.match(/aria-labelledby="([^"]+)"/u)?.[1] ?? "";
        expect(label).toMatch(/-completion-label$/u);
        expect(html).toContain(`id="${label}"`);
        expect(html).toContain("2286 / 2000");
        expect(html).toContain('role="status"');
        expect(html).toContain(
          locale === "ru"
            ? "Сократите критерий завершения, чтобы сохранить изменения."
            : "Shorten the completion criteria to save your changes.",
        );
        const description = html.match(/aria-describedby="([^"]+)"/u)?.[1];
        expect(description?.split(" ")).toHaveLength(2);
        for (const id of description?.split(" ") ?? [])
          expect(html).toContain(`id="${id}"`);
      } finally {
        i18n.global.locale.value = previous;
      }
    },
  );

  it.each(["я", "🧭"])(
    "считает Unicode-символы на границе 2000/2001 (%s)",
    async (character) => {
      const value = character.repeat(2000);
      const { props, events, state } = mount(value);
      expect(props.completionCriteria).toBe(value);
      expect(state.completionCriteriaLength.value).toBe(2000);
      expect(state.completionCriteriaTooLong.value).toBe(false);
      props.completionCriteria = `${value}я`;
      await nextTick();
      expect(props.completionCriteria).toBe(`${value}я`);
      expect(state.completionCriteriaLength.value).toBe(2001);
      expect(state.completionCriteriaTooLong.value).toBe(true);
      expect(state.completionCriteriaDescription.value).toContain("-error");
      props.completionCriteria = value;
      await nextTick();
      expect(state.completionCriteriaTooLong.value).toBe(false);
      expect(state.completionCriteriaDescription.value).not.toContain("-error");
      expect(events).toEqual([]);
    },
  );

  it("не обрезает и не меняет загруженный текст, включая пробелы", async () => {
    const value = ` ${"я".repeat(2286)} `;
    const { props, events, state } = mount(value);
    expect(props.completionCriteria).toBe(value);
    expect(state.completionCriteriaLength.value).toBe(2288);
    expect(events).toEqual([]);
    props.completionCriteria = ` ${"я".repeat(2001)} `;
    await nextTick();
    expect(props.completionCriteria).toBe(` ${"я".repeat(2001)} `);
    expect(events).toEqual([]);
    expect(state.completionCriteriaTooLong.value).toBe(true);
  });

  it("пустой необязательный критерий допустим", async () => {
    const html = await renderToString(
      createSSRApp(Component, fields("")).use(i18n),
    );
    expect(html).toContain("0 / 2000");
    expect(html).toContain('aria-invalid="false"');
    expect(html).not.toContain('role="status"');
  });

  it("связывает подсказку с собственным полем каждого редактора", async () => {
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h("div", [h(Component, fields("")), h(Component, fields(""))]),
      }).use(i18n),
    );
    const descriptions = Array.from(
      html.matchAll(/aria-describedby="([^"]+)"/gu),
      (match) => match[1] ?? "",
    );
    expect(descriptions).toHaveLength(2);
    expect(new Set(descriptions).size).toBe(2);
    for (const id of descriptions) expect(html).toContain(`id="${id}"`);
  });
});
