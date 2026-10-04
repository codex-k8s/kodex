import { execFileSync } from "node:child_process";
import { runInNewContext } from "node:vm";
import { expect, test, vi } from "vitest";
import {
  controlCenterRemoteReloadPlugin,
  remoteEntryClientSource,
} from "../../vite.config";

test("dev HTML больше не исполняет main static graph без независимой ошибки загрузки", () => {
  const plugin = controlCenterRemoteReloadPlugin();
  const hook = plugin.transformIndexHtml;
  if (!hook || typeof hook === "function" || !("handler" in hook))
    throw new Error("Fixture requires HTML handler");
  const result = hook.handler.call(
    {} as never,
    '<div id="app"></div><script type="module" src="/src/main.ts"></script>',
    {} as never,
  ) as { html: string; tags: { attrs?: { src?: string } }[] };
  expect(result.html).not.toContain('src="/src/main.ts"');
  expect(
    result.tags.some((tag) => tag.attrs?.src === "/__kodex_dev_entry.js"),
  ).toBe(true);
});

test("missing static SDK import не достигает bootstrap catch; внешний loader нужен до main evaluation", () => {
  const result = execFileSync(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      `
    globalThis.bootstrapCatchInstalled = false;
    const source = 'import "file:///kodex-synthetic-missing-sdk.mjs"; globalThis.bootstrapCatchInstalled = true;';
    try { await import('data:text/javascript,' + encodeURIComponent(source)); }
    catch (error) { process.stdout.write(JSON.stringify({ code: error.code, installed: globalThis.bootstrapCatchInstalled })); }
  `,
    ],
    { encoding: "utf8" },
  );
  expect(JSON.parse(result)).toEqual({
    code: "ERR_MODULE_NOT_FOUND",
    installed: false,
  });
});

test("успешный initial module исполняется ровно один раз без error card или retry", () => {
  const source = remoteEntryClientSource().replace(
    '"/src/main.ts"',
    JSON.stringify(
      "data:text/javascript,globalThis.syntheticEntryCount=(globalThis.syntheticEntryCount??0)+1;",
    ),
  );
  const result = execFileSync(
    process.execPath,
    [
      "--input-type=module",
      "-e",
      `
    let fallback = 0;
    globalThis.document = { documentElement: { lang: 'ru', dataset: {} }, getElementById() { fallback++; return null; } };
    globalThis.console.error = () => { fallback++; };
    await eval(${JSON.stringify(source)});
    process.stdout.write(JSON.stringify({ count: globalThis.syntheticEntryCount, fallback }));
  `,
    ],
    { encoding: "utf8" },
  );
  expect(JSON.parse(result)).toEqual({ count: 1, fallback: 0 });
});

test("независимый dev loader показывает safe error card и retry сохраняет текущий URL", async () => {
  const plugin = controlCenterRemoteReloadPlugin();
  let middleware:
    | ((request: { url: string }, response: object, next: () => void) => void)
    | undefined;
  const configure = plugin.configureServer;
  if (typeof configure !== "function")
    throw new Error("Fixture requires configureServer");
  void configure.call(
    {} as never,
    {
      watcher: { on() {} },
      middlewares: {
        use(handler: typeof middleware) {
          middleware = handler;
        },
      },
    } as never,
  );
  const response = { setHeader: vi.fn(), end: vi.fn(), statusCode: 0 };
  if (!middleware) throw new Error("Fixture requires middleware");
  middleware({ url: "/__kodex_dev_entry.js" }, response, vi.fn());
  expect(response.statusCode).toBe(200);
  const reload = vi.fn();
  const diagnostic = vi.fn();
  const elements: {
    tag: string;
    textContent: string;
    className: string;
    href?: string;
    rel?: string;
    children: unknown[];
    attributes: Record<string, string>;
    click?: () => void;
  }[] = [];
  const createElement = (tag: string) => {
    const element = {
      tag,
      textContent: "",
      className: "",
      children: [] as unknown[],
      attributes: {} as Record<string, string>,
      append(...children: unknown[]) {
        this.children.push(...children);
      },
      setAttribute(key: string, value: string) {
        this.attributes[key] = value;
      },
      addEventListener(_name: string, handler: () => void) {
        this.click = handler;
      },
      click: undefined as (() => void) | undefined,
    };
    elements.push(element);
    return element;
  };
  const root = { replaceChildren: vi.fn() };
  const document = {
    createElement,
    documentElement: { lang: "ru", dataset: {} },
    getElementById: () => root,
    head: { append: vi.fn() },
  };
  // VM не разрешает dynamic import: это тот же rejected initial module path.
  await (runInNewContext(response.end.mock.calls[0]?.[0] as string, {
    document,
    window: { location: { reload } },
    console: { error: diagnostic },
  }) as Promise<void>);
  expect(root.replaceChildren).toHaveBeenCalledTimes(1);
  expect(elements.find((element) => element.tag === "h1")?.textContent).toBe(
    "Не удалось загрузить интерфейс",
  );
  const button = elements.find((element) => element.tag === "button");
  expect(button?.textContent).toBe("Повторить");
  expect(elements.find((element) => element.tag === "link")).toMatchObject({
    href: "/src/app/styles/base.css?direct",
    rel: "stylesheet",
  });
  expect(JSON.stringify(elements)).not.toMatch(
    /missing-sdk|ERR_|stack|file:\/\//u,
  );
  expect(diagnostic).toHaveBeenCalledExactlyOnceWith(
    "Control Center module loading failed",
  );
  expect(reload).not.toHaveBeenCalled();
  button?.click?.();
  expect(reload).toHaveBeenCalledTimes(1);
});
