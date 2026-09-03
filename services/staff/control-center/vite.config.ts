import { randomUUID } from "node:crypto";
import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue";
import type { FileSystemServeOptions, Plugin } from "vite";
import { defineConfig } from "vitest/config";

const developmentPublicHost = process.env.KODEX_DEV_PUBLIC_HOST;
const developmentApiTarget = process.env.KODEX_DEV_API_TARGET;
const controlCenterRoot = fileURLToPath(new URL(".", import.meta.url));
const remoteDevelopmentEnabled = Boolean(
  developmentPublicHost && developmentApiTarget,
);

export const controlCenterReloadPollIntervalMs = 1_000;
export const controlCenterRemoteHMRPort = 24_678;
export const controlCenterRemoteHMRClientPort = 9;
const controlCenterReloadClientPath = "/__kodex_dev_reload.js";
const controlCenterRevisionPath = "/__kodex_dev_revision";
const viteHMRClientScriptPattern =
  /<script\s+type=["']module["']\s+src=["'][^"']*\/@vite\/client["']><\/script>\s*/u;

export function withoutViteHMRClient(html: string): string {
  return html.replace(viteHMRClientScriptPattern, "");
}

function controlCenterRemoteReloadPlugin(): Plugin {
  const serverRevision = randomUUID();
  let revision = 0;
  return {
    name: "kodex:remote-live-reload",
    apply: "serve",
    configureServer(server) {
      const advanceRevision = (): void => {
        revision += 1;
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
          body = `${serverRevision}:${String(revision)}`;
          contentType = "text/plain; charset=utf-8";
        } else if (pathname === controlCenterReloadClientPath) {
          body = remoteReloadClientSource();
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
        server.watcher.off("add", advanceRevision);
        server.watcher.off("change", advanceRevision);
        server.watcher.off("unlink", advanceRevision);
      });
    },
    transformIndexHtml: {
      order: "post",
      handler(html) {
        return {
          html: withoutViteHMRClient(html),
          tags: [
            {
              tag: "script",
              attrs: { src: controlCenterReloadClientPath, type: "module" },
              injectTo: "body",
            },
          ],
        };
      },
    },
  };
}

function remoteReloadClientSource(): string {
  return `
const revisionEndpoint = ${JSON.stringify(controlCenterRevisionPath)};
const pollIntervalMs = ${String(controlCenterReloadPollIntervalMs)};
let observedRevision;

async function pollRevision() {
  try {
    const response = await fetch(revisionEndpoint, {
      cache: "no-store",
      credentials: "same-origin",
    });
    const responseLocation = new URL(response.url);
    if (
      response.ok &&
      responseLocation.origin === window.location.origin &&
      responseLocation.pathname === revisionEndpoint
    ) {
      const revision = await response.text();
      if (observedRevision !== undefined && revision !== observedRevision) {
        window.location.reload();
        return;
      }
      observedRevision = revision;
    }
  } catch {
    // Следующий bounded poll восстановит live reload после краткого outage.
  }
  window.setTimeout(pollRevision, pollIntervalMs);
}

void pollRevision();
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
  ],
} satisfies FileSystemServeOptions;

export default defineConfig({
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
          // Vite client нужен CSS-модулям, но его HMR transport не должен
          // занимать публичный WebSocket proxy; reload выполняет polling выше.
          hmr: {
            clientPort: controlCenterRemoteHMRClientPort,
            host: "127.0.0.1",
            overlay: false,
            port: controlCenterRemoteHMRPort,
            protocol: "ws",
          },
          watch: {
            ignored: [
              "**/.auth/**",
              "**/e2e/**",
              "**/test-results/**",
              "**/playwright-report/**",
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
