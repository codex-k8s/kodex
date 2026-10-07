import { randomUUID } from "node:crypto";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { isAbsolute, relative, resolve } from "node:path";
import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import type { FileSystemServeOptions, Plugin } from "vite";
import { defineConfig } from "vitest/config";
import { sessionProbeRequested } from "./src/shared/api/session-probe";

const developmentPublicHost = process.env.KODEX_DEV_PUBLIC_HOST;
const developmentApiTarget = process.env.KODEX_DEV_API_TARGET;
const controlCenterRoot = fileURLToPath(new URL(".", import.meta.url));
const remoteDevelopmentEnabled = Boolean(
  developmentPublicHost && developmentApiTarget,
);

export const controlCenterReloadPollIntervalMs = 1_000;
export const controlCenterReloadSettleMs = 1_500;
const controlCenterRestartTimeoutMs = 30_000;
const controlCenterCodegenTimeoutMs = 120_000;
const controlCenterReloadClientPath = "/__kodex_dev_reload.js";
const controlCenterEntryClientPath = "/__kodex_dev_entry.js";
const controlCenterRevisionPath = "/__kodex_dev_revision";
const controlCenterMainEntryPattern =
  /<script\s+type=["']module["']\s+src=["']\/src\/main\.ts(?:\?[^"']*)?["']><\/script>\s*/u;
const viteHMRClientScriptPattern =
  /<script\s+type=["']module["']\s+src=["'][^"']*\/@vite\/client["']><\/script>\s*/u;
const viteClientModulePattern =
  /[/\\]vite[/\\]dist[/\\]client[/\\]client\.mjs(?:\?.*)?$/u;
const viteHMRConnectPattern =
  /\ntransport\.connect\(createHMRHandler\(handleMessage\)\);\n/u;

export function withoutViteHMRClient(html: string): string {
  return html.replace(viteHMRClientScriptPattern, "");
}

export function withoutViteHMRConnection(source: string): string {
  const result = source.replace(viteHMRConnectPattern, "\n");
  if (result === source)
    throw new Error("Vite client HMR bootstrap is missing");
  return result;
}

export function remoteReloadRuntimeFile(file: string, root: string): boolean {
  const name = relative(root, file).replaceAll("\\", "/");
  if (!name || name.startsWith("../") || isAbsolute(name)) return false;
  if (
    /(?:^|\/)(?:node_modules|\.git|\.kodex-codegen|coverage|test-results|playwright-report|__tests__|__mocks__)(?:\/|$)/u.test(
      name,
    )
  )
    return false;
  if (name.startsWith("public/")) return true;
  if (/\.(?:test|spec)\.[cm]?[jt]sx?$/u.test(name) || name.endsWith(".d.ts"))
    return false;
  return (
    name.startsWith("src/") ||
    name.startsWith("public/") ||
    name === "index.html" ||
    /^(?:package(?:-lock)?\.json|tsconfig(?:\.[\w-]+)?\.json|vite\.config\.[cm]?[jt]s)$/u.test(
      name,
    )
  );
}

function frontendCodegenState(root: string): "READY" | "RUNNING" | "FAILED" {
  const directory = resolve(root, ".kodex-codegen");
  try {
    const kinds = readdirSync(directory);
    let running = false;
    for (const kind of kinds) {
      if (!["openapi", "asyncapi", "integration-schema"].includes(kind))
        return "FAILED";
      try {
        const status = JSON.parse(
          readFileSync(resolve(directory, kind, "status.json"), "utf8"),
        ) as { state?: unknown; startedAt?: unknown };
        if (
          status.state !== "RUNNING" ||
          typeof status.startedAt !== "number" ||
          !Number.isSafeInteger(status.startedAt) ||
          Date.now() - status.startedAt > controlCenterCodegenTimeoutMs ||
          status.startedAt > Date.now()
        )
          return "FAILED";
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== "ENOENT") return "FAILED";
        // Короткое окно создания/удаления marker не объявляет SDK готовым.
        if (
          Date.now() - statSync(resolve(directory, kind)).mtimeMs >
          controlCenterCodegenTimeoutMs
        )
          return "FAILED";
      }
      running = true;
    }
    return running ? "RUNNING" : "READY";
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === "ENOENT"
      ? "READY"
      : "FAILED";
  }
}

export function controlCenterRemoteReloadPlugin(): Plugin {
  const serverRevision = randomUUID();
  let revision = 0;
  let pending = false;
  let changedAt = 0;
  let codegenFailed = false;
  return {
    name: "kodex:remote-live-reload",
    apply: "serve",
    enforce: "post",
    configureServer(server) {
      let closed = false;
      let restartPending = false;
      let restartFailed = false;
      let restartTimer: ReturnType<typeof setTimeout> | undefined;
      let restartTimeout: ReturnType<typeof setTimeout> | undefined;
      let restartAttempt = 0;
      const scheduleConfigRestart = (): void => {
        restartPending = true;
        restartFailed = false;
        restartAttempt += 1;
        const attempt = restartAttempt;
        if (restartTimer !== undefined) clearTimeout(restartTimer);
        if (restartTimeout !== undefined) clearTimeout(restartTimeout);
        restartTimer = setTimeout(() => {
          restartTimer = undefined;
          if (closed || attempt !== restartAttempt) return;
          const previousConfig = server.config;
          let settled = false;
          const fail = (): void => {
            if (closed || attempt !== restartAttempt || settled) return;
            settled = true;
            if (restartTimeout !== undefined) clearTimeout(restartTimeout);
            restartTimeout = undefined;
            restartPending = false;
            restartFailed = true;
            server.config.logger.error(
              "Frontend development server restart failed",
            );
          };
          restartTimeout = setTimeout(fail, controlCenterRestartTimeoutMs);
          // Vite при hmr:false пропускает restart конфигурации. Его restart()
          // также может поглотить ошибку загрузки: проверяем замену config.
          void Promise.resolve()
            .then(() => {
              if (closed || attempt !== restartAttempt) return;
              return server.restart();
            })
            .then(() => {
              if (closed || attempt !== restartAttempt || settled) return;
              if (server.config === previousConfig) {
                fail();
                return;
              }
              settled = true;
              if (restartTimeout !== undefined) clearTimeout(restartTimeout);
              restartTimeout = undefined;
              restartPending = false;
            }, fail);
        }, controlCenterReloadSettleMs);
      };
      const advanceRevision = (file: string): void => {
        if (closed) return;
        const configDependency =
          file === server.config.configFile ||
          server.config.configFileDependencies.includes(file);
        if (
          !configDependency &&
          !remoteReloadRuntimeFile(file, server.config.root)
        )
          return;
        // Встроенный HMR client выключен: сначала сбрасываем серверный кэш
        // трансформации, затем разрешаем браузеру увидеть новую ревизию.
        // Иначе reload может повторно получить старый generated SDK.
        for (const environment of Object.values(server.environments))
          environment.moduleGraph.onFileChange(file);
        pending = true;
        changedAt = Date.now();
        if (configDependency && server.config.server.hmr === false)
          scheduleConfigRestart();
      };
      server.watcher.on("add", advanceRevision);
      server.watcher.on("change", advanceRevision);
      server.watcher.on("unlink", advanceRevision);
      server.middlewares.use((request, response, next) => {
        const pathname = new URL(request.url ?? "/", "http://kodex.invalid")
          .pathname;
        let body: string | undefined;
        let contentType: string | undefined;
        if (pathname === controlCenterRevisionPath) {
          if (restartPending || restartFailed) {
            response.statusCode = restartFailed ? 503 : 204;
            response.setHeader("Cache-Control", "no-store");
            response.end();
            return;
          }
          const generation = frontendCodegenState(server.config.root);
          if (generation !== "READY") {
            pending = true;
            changedAt = Date.now();
            if (generation === "FAILED" && !codegenFailed)
              server.config.logger.error(
                "Frontend code generation failed or expired; rerun the failed generator before reloading",
              );
            codegenFailed = generation === "FAILED";
            response.statusCode = codegenFailed ? 503 : 204;
            response.setHeader("Cache-Control", "no-store");
            response.end();
            return;
          }
          codegenFailed = false;
          if (pending && Date.now() - changedAt < controlCenterReloadSettleMs) {
            response.statusCode = 204;
            response.setHeader("Cache-Control", "no-store");
            response.end();
            return;
          }
          if (pending) {
            revision += 1;
            pending = false;
          }
          body = `${serverRevision}:${String(revision)}`;
          contentType = "text/plain; charset=utf-8";
        } else if (pathname === controlCenterReloadClientPath) {
          body = remoteReloadClientSource();
          contentType = "application/javascript; charset=utf-8";
        } else if (pathname === controlCenterEntryClientPath) {
          body = remoteEntryClientSource();
          contentType = "application/javascript; charset=utf-8";
        }
        if (body === undefined || contentType === undefined) {
          next();
          return;
        }
        response.statusCode = 200;
        response.setHeader("Cache-Control", "no-store");
        response.setHeader("Content-Type", contentType);
        response.setHeader("Content-Length", Buffer.byteLength(body));
        response.end(body);
      });
      server.httpServer?.once("close", () => {
        closed = true;
        restartAttempt += 1;
        if (restartTimer !== undefined) clearTimeout(restartTimer);
        if (restartTimeout !== undefined) clearTimeout(restartTimeout);
        restartTimer = undefined;
        restartTimeout = undefined;
        server.watcher.off("add", advanceRevision);
        server.watcher.off("change", advanceRevision);
        server.watcher.off("unlink", advanceRevision);
      });
    },
    transformIndexHtml: {
      order: "post",
      handler(html) {
        const hasMainEntry = controlCenterMainEntryPattern.test(html);
        return {
          html: withoutViteHMRClient(html).replace(
            controlCenterMainEntryPattern,
            "",
          ),
          tags: [
            {
              tag: "script",
              attrs: { src: controlCenterReloadClientPath, type: "module" },
              injectTo: "body",
            },
            ...(hasMainEntry
              ? [
                  {
                    tag: "script",
                    attrs: {
                      src: controlCenterEntryClientPath,
                      type: "module",
                    },
                    injectTo: "body" as const,
                  },
                ]
              : []),
          ],
        };
      },
    },
    transform(source, id) {
      if (!viteClientModulePattern.test(id)) return;
      return { code: withoutViteHMRConnection(source), map: null };
    },
  };
}

export const controlCenterReloadTimeoutMs = 3_000;

export function remoteEntryClientSource(): string {
  return `
(async () => {
  try {
    await import("/src/main.ts");
  } catch {
    // Этот loader не зависит от SDK: static import может завершиться до main.
    console.error("Control Center module loading failed");
    document.documentElement.dataset.kodexBootstrap = "failed";
    const root = document.getElementById("app");
    if (!root) return;
    const english = document.documentElement.lang.startsWith("en");
    const style = document.createElement("link");
    style.rel = "stylesheet";
    style.href = "/src/app/styles/base.css?direct";
    document.head.append(style);
    const container = document.createElement("section");
    container.className = "auth-gate";
    const card = document.createElement("div");
    card.className = "auth-card";
    card.setAttribute("role", "alert");
    const title = document.createElement("h1");
    title.textContent = english ? "Could not load the interface" : "Не удалось загрузить интерфейс";
    const retry = document.createElement("button");
    retry.type = "button";
    retry.className = "button button--primary";
    retry.textContent = english ? "Retry" : "Повторить";
    retry.addEventListener("click", () => {
      retry.disabled = true;
      window.location.reload();
    });
    card.append(title, retry);
    container.append(card);
    root.replaceChildren(container);
  }
})();
`;
}

export function remoteReloadClientSource(): string {
  return `
(() => {
  const revisionEndpoint = ${JSON.stringify(controlCenterRevisionPath)};
  const pollIntervalMs = ${String(controlCenterReloadPollIntervalMs)};
  const timeoutMs = ${String(controlCenterReloadTimeoutMs)};
  const key = Symbol.for("kodex.dev.reload");
  window[key]?.dispose();
  let observedRevision;
  let initialGenerationPending = false;
  let timer;
  let active;
  let paused = false;
  let disposed = false;
  let reloading = false;

  function clearTimer() {
    if (timer !== undefined) window.clearTimeout(timer);
    timer = undefined;
  }
  function pause() {
    paused = true;
    clearTimer();
    active?.controller.abort();
  }
  function dispose() {
    disposed = true;
    pause();
    window.removeEventListener("pagehide", pause);
    window.removeEventListener("beforeunload", pause);
    window.removeEventListener("pointerdown", resumeAfterInteraction);
    window.removeEventListener("keydown", resumeAfterInteraction);
    window.removeEventListener("pageshow", resume);
  }
  function schedule() {
    if (disposed || paused || reloading || active || timer !== undefined) return;
    timer = window.setTimeout(() => { timer = undefined; pollRevision(); }, pollIntervalMs);
  }
  function resume() {
    if (!paused || disposed || reloading) return;
    paused = false;
    schedule();
  }
  function resumeAfterInteraction(event) {
    // Отменённый beforeunload не создаёт pageshow. Только настоящее новое
    // действие пользователя возобновляет poll сохранившегося документа.
    if (event.isTrusted) resume();
  }
  function pollRevision() {
    if (disposed || paused || reloading || active) return;
    const controller = new AbortController();
    const attempt = { controller, timeout: undefined };
    active = attempt;
    attempt.timeout = window.setTimeout(() => controller.abort(), timeoutMs);
    const current = () => active === attempt && !disposed && !paused && !reloading && !controller.signal.aborted;
    const finish = () => {
      window.clearTimeout(attempt.timeout);
      if (active === attempt) active = undefined;
      schedule();
    };
    // Обработчики обеих ветвей устанавливаются сразу; завершение отменённого
    // документа не запускает новый poll и не принимает поздний revision.
    Promise.resolve().then(() => {
      if (!current()) return;
      return fetch(revisionEndpoint, { cache: "no-store", credentials: "same-origin", redirect: "manual", signal: controller.signal })
        .then(response => {
          if (current() && (response.type === "opaqueredirect" || response.status === 401 || response.status === 403)) {
            // Ответ уже получен: отмена завершившегося fetch создаёт лишний
            // requestfailed в Chromium. Закрываем только следующий poll.
            paused = true;
            clearTimer();
            window.dispatchEvent(new Event(${JSON.stringify(sessionProbeRequested)}));
            return;
          }
          if (!current() || !response.ok) return;
          const address = new URL(response.url);
          if (address.origin !== window.location.origin || address.pathname !== revisionEndpoint) return;
          if (response.status === 204) {
            // Первый документ мог загрузиться среди частично записанного SDK.
            // READY требует одного нового document, а не принятия его baseline.
            if (observedRevision === undefined) initialGenerationPending = true;
            return;
          }
          return response.text().then(revision => {
            if (!current() || !/^[a-f0-9-]{36}:[0-9]{1,12}$/.test(revision)) return;
            if (initialGenerationPending || (observedRevision !== undefined && revision !== observedRevision)) {
              reloading = true;
              clearTimer();
              window.location.reload();
            } else observedRevision = revision;
          });
        });
    }).then(finish, finish);
  }
  window[key] = { dispose };
  // pagehide приходит после загрузки нового документа: beforeunload закрывает
  // окно, в котором WebKit уже запрещает fetch старого документа.
  window.addEventListener("beforeunload", pause);
  window.addEventListener("pointerdown", resumeAfterInteraction);
  window.addEventListener("keydown", resumeAfterInteraction);
  window.addEventListener("pagehide", pause);
  window.addEventListener("pageshow", resume);
  pollRevision();
})();
`;
}

export const controlCenterFileSystemBoundary = {
  strict: true,
  allow: [controlCenterRoot],
  deny: [
    ".env",
    ".env.*",
    ".kodex-env",
    ".kodex-env.*",
    ".kodex-remote-env",
    ".kodex-remote-env.*",
    ".npmrc",
    ".yarnrc",
    ".yarnrc.*",
    ".netrc",
    "*.{key,crt,cer,der,pem,p12,pfx,p7b,p7c,pk8,pkcs8,jks,keystore}",
    "*.{token,secret,credentials,private}",
    "**/{id_rsa,id_dsa,id_ecdsa,id_ed25519}",
    "**/{credentials,secrets,token,private}",
    "**/{auth,credentials,secrets,token,private}.{json,yaml,yml,toml,txt}",
    "**/.aws/credentials",
    "**/.config/gh/hosts.yml",
    "**/.config/gcloud/application_default_credentials.json",
    "**/.docker/config.json",
    "**/.kube/config",
    "**/.git/**",
    "**/.kodex-codegen/**",
  ],
} satisfies FileSystemServeOptions;

export default defineConfig({
  cacheDir: process.env.KODEX_DEV_CACHE_DIR ?? "node_modules/.vite",
  plugins: [
    vue(),
    ...(remoteDevelopmentEnabled ? [controlCenterRemoteReloadPlugin()] : []),
  ],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    target: "es2022",
    sourcemap: true,
  },
  server: {
    fs: controlCenterFileSystemBoundary,
    ...(developmentPublicHost && developmentApiTarget
      ? {
          allowedHosts: [developmentPublicHost],
          hmr: false,
          watch: {
            ignored: [
              "**/.auth/**",
              "**/e2e/**",
              "**/test-results/**",
              "**/playwright-report/**",
              "**/.kodex-codegen/**",
            ],
            interval: 500,
            usePolling: true,
          },
          proxy: {
            "/api": {
              changeOrigin: false,
              secure: false,
              target: developmentApiTarget,
              ws: true,
            },
          },
        }
      : {}),
  },
  test: {
    clearMocks: true,
    restoreMocks: true,
    environment: "node",
    include: ["src/**/*.test.ts", "e2e/**/*.test.ts"],
  },
});
