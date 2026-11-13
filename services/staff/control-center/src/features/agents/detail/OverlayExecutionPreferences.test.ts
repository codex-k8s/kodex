import {
  createSSRApp,
  defineComponent,
  reactive,
  ssrContextKey,
  type SetupContext,
} from "vue";
import { renderToString } from "@vue/server-renderer";
import { afterEach, expect, it, vi } from "vitest";
import { i18n } from "@/app/i18n";
import { createNoopRenderer } from "@/test-utils/create-noop-renderer";
import { overlaySchemaFixture as schema } from "@/test-utils/runtime-catalog-fixture";
import { parse } from "smol-toml";
import Component from "./OverlayExecutionPreferences.vue";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
const disposers: (() => void)[] = [];
afterEach(() => {
  for (const dispose of disposers.splice(0)) dispose();
});
function mount(content: string, disabled = false) {
  const props = reactive({ content, schema, disabled });
  const changes: string[] = [];
  let state!: { change(key: string, event: Event): void };
  const renderer = createNoopRenderer();
  const app = renderer
    .createApp(
      defineComponent({
        setup(_, context) {
          state = (
            Component as unknown as {
              setup(props: object, context: SetupContext): typeof state;
            }
          ).setup(props, {
            ...context,
            emit: (_name: string, value: string) => {
              changes.push(value);
              props.content = value;
            },
          });
          return () => null;
        },
      }),
    )
    .use(i18n);
  app.provide(ssrContextKey, {});
  app.mount({});
  disposers.push(() => app.unmount());
  function change(key: string, value: string, badInput = false) {
    const event = new Event("change");
    Object.defineProperty(event, "target", {
      value: { value, validity: { badInput } },
    });
    state.change(key, event);
  }
  return { props, changes, change };
}
it("публикует редактируемый TOML только через существующее событие черновика", () => {
  const editor = mount(
    'model_reasoning_effort = "high"\n[context]\nprune = false',
  );
  editor.change("cache.no_reuse", "true");
  expect(editor.changes).toHaveLength(1);
  expect(parse(editor.props.content)).toEqual({
    model_reasoning_effort: "high",
    context: { prune: false },
    cache: { no_reuse: true },
  });
  editor.change("cache.no_reuse", "");
  expect(parse(editor.props.content).cache).toEqual({});
});
it("не меняет readonly документ и не нормализует запрещённые поля", () => {
  const locked = mount("", true);
  locked.change("context.prune", "false");
  expect(locked.changes).toEqual([]);
  const bad = mount('cache.qualification = "SUPPORTED"');
  bad.change("cache.no_reuse", "true");
  expect(bad.changes).toEqual([]);
});
it("показывает семь настроек, наследование и предупреждение о гарантиях", async () => {
  const html = await renderToString(
    createSSRApp(Component, { content: "", schema, disabled: true }).use(i18n),
  );
  expect(html.match(/<select\b/g) ?? []).toHaveLength(7);
  expect(html).toContain("disabled");
  expect(html).toContain("Из настройки маршрута");
  expect(html).toContain("не доказывает отключение кеша");
  expect(html).not.toContain("web_search");
});

it("окно показывает requested/inherit и не публикует invalid DOM number как inherit", async () => {
  const editor = mount("");
  editor.change("context.window_tokens", "16384");
  expect(parse(editor.props.content)).toEqual({
    context: { window_tokens: 16384 },
  });
  for (const invalid of ["NaN", "Infinity", "1e4", "1.5", "9007199254740993"]) {
    editor.change("context.window_tokens", invalid);
    expect(editor.changes).toHaveLength(1);
  }
  editor.change("context.window_tokens", "", true);
  expect(editor.changes).toHaveLength(1);
  editor.change("context.window_tokens", "");
  expect(parse(editor.props.content)).toEqual({ context: {} });
  const html = await renderToString(
    createSSRApp(Component, { content: "", schema, disabled: false }).use(i18n),
  );
  expect(html).toContain('type="text"');
  expect(html).toContain('data-minimum="2049"');
  expect(html).toContain('data-maximum="32768"');
  expect(html).toContain("32768");
});
