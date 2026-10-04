import { EventEmitter } from "node:events";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { ViteDevServer } from "vite";
import { afterEach, expect, test, vi } from "vitest";
import { controlCenterRemoteReloadPlugin } from "../../vite.config";

const roots: string[] = [];
function fixture() {
  const root = mkdtempSync(join(tmpdir(), "kodex-reload-test-"));
  roots.push(root);
  const watcher = new EventEmitter();
  const httpServer = new EventEmitter();
  const invalidation = vi.fn();
  const diagnostic = vi.fn();
  let middleware: (
    request: { url: string },
    response: object,
    next: () => void,
  ) => void;
  const server = {
    config: {
      root,
      configFile: join(root, "vite.config.ts"),
      configFileDependencies: [],
      logger: { error: diagnostic },
    },
    watcher,
    httpServer,
    environments: { client: { moduleGraph: { onFileChange: invalidation } } },
    middlewares: {
      use(handler: typeof middleware) {
        middleware = handler;
      },
    },
  };
  const plugin = controlCenterRemoteReloadPlugin();
  const configure = plugin.configureServer;
  if (typeof configure !== "function")
    throw new Error("Fixture requires configureServer");
  void configure.call({} as never, server as unknown as ViteDevServer);
  function read() {
    const response = { statusCode: 0, setHeader: vi.fn(), end: vi.fn() };
    middleware({ url: "/__kodex_dev_revision" }, response, vi.fn());
    return {
      status: response.statusCode,
      body: response.end.mock.calls[0]?.[0] as string | undefined,
    };
  }
  const change = (file: string, event = "change") =>
    watcher.emit(event, join(root, file));
  return { root, watcher, httpServer, diagnostic, invalidation, read, change };
}
afterEach(() => {
  vi.useRealTimers();
  for (const root of roots.splice(0)) rmSync(root, { recursive: true });
});

test("tests, tools, git и build output не объявляют новую runtime revision", () => {
  vi.useFakeTimers();
  const f = fixture();
  const baseline = f.read();
  for (const path of [
    "src/app/example.test.ts",
    "src/app/example.spec.ts",
    "tools/generate.mjs",
    ".git/index",
    "dist/app.js",
    "node_modules/.cache/project.tsbuildinfo",
    "coverage/result.json",
  ])
    f.change(path);
  vi.advanceTimersByTime(2_000);
  expect(f.read()).toEqual(baseline);
  expect(f.invalidation).not.toHaveBeenCalled();
  f.httpServer.emit("close");
});

test("runtime batch invalidates сразу, но revision доступна только после 1500ms тишины", () => {
  vi.useFakeTimers();
  const f = fixture();
  const baseline = f.read();
  f.change("src/App.vue");
  expect(f.invalidation).toHaveBeenCalledTimes(1);
  expect(f.read().status).toBe(204);
  vi.advanceTimersByTime(1_000);
  f.change("src/shared/api/generated/asyncapi/Run.ts", "unlink");
  vi.advanceTimersByTime(1_499);
  expect(f.read().status).toBe(204);
  vi.advanceTimersByTime(1);
  const stable = f.read();
  expect(stable.status).toBe(200);
  expect(stable.body).not.toBe(baseline.body);
  expect(f.read()).toEqual(stable);
  f.httpServer.emit("close");
  f.change("src/App.vue");
  expect(f.watcher.listenerCount("change")).toBe(0);
  expect(vi.getTimerCount()).toBe(0);
});

test("generation barrier сохраняется дольше quiet batch; FAILED наблюдаем и recovery требует готового SDK", () => {
  vi.useFakeTimers();
  const f = fixture();
  const barrier = join(f.root, ".kodex-codegen", "asyncapi");
  mkdirSync(barrier, { recursive: true });
  writeFileSync(
    join(barrier, "status.json"),
    JSON.stringify({ state: "RUNNING", startedAt: Date.now() }),
  );
  f.change("src/shared/api/generated/asyncapi/Run.ts", "unlink");
  vi.advanceTimersByTime(10_000);
  expect(f.read().status).toBe(204);
  writeFileSync(
    join(barrier, "status.json"),
    JSON.stringify({ state: "FAILED", startedAt: Date.now() }),
  );
  expect(f.read().status).toBe(503);
  expect(f.read().status).toBe(503);
  expect(f.diagnostic).toHaveBeenCalledTimes(1);
  rmSync(barrier, { recursive: true });
  f.change("src/shared/api/generated/asyncapi/Run.ts", "add");
  expect(f.read().status).toBe(204);
  vi.advanceTimersByTime(1_500);
  expect(f.read().status).toBe(200);
  f.httpServer.emit("close");
});

test("прерванная generation не зависает молча: expired RUNNING закрыто даёт 503", () => {
  vi.useFakeTimers();
  const f = fixture();
  const barrier = join(f.root, ".kodex-codegen", "openapi");
  mkdirSync(barrier, { recursive: true });
  writeFileSync(
    join(barrier, "status.json"),
    JSON.stringify({ state: "RUNNING", startedAt: Date.now() }),
  );
  expect(f.read().status).toBe(204);
  vi.advanceTimersByTime(120_001);
  expect(f.read().status).toBe(503);
  expect(f.diagnostic).toHaveBeenCalledTimes(1);
  f.httpServer.emit("close");
});
