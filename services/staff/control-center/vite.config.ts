import { fileURLToPath, URL } from "node:url";

import vue from "@vitejs/plugin-vue";
import type { FileSystemServeOptions } from "vite";
import { defineConfig } from "vitest/config";

const developmentPublicHost = process.env.KODEX_DEV_PUBLIC_HOST;
const developmentApiTarget = process.env.KODEX_DEV_API_TARGET;
const controlCenterRoot = fileURLToPath(new URL(".", import.meta.url));

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
  plugins: [vue()],
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
          hmr: {
            clientPort: 443,
            host: developmentPublicHost,
            protocol: "wss",
          },
          watch: {
            ignored: [
              "**/.auth/**",
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
