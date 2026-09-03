import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const mainSource = readFileSync(new URL("../main.ts", import.meta.url), "utf8");
const appSource = readFileSync(new URL("../App.vue", import.meta.url), "utf8");
const authGateSource = readFileSync(
  new URL("./AuthGate.vue", import.meta.url),
  "utf8",
);
const viteConfigSource = readFileSync(
  new URL("../../vite.config.ts", import.meta.url),
  "utf8",
);

describe("bootstrap Control Center", () => {
  it("монтирует shell без ожидания initial navigation и session probe", () => {
    expect(mainSource).toContain('app.mount("#app")');
    expect(mainSource).not.toContain("await router.isReady()");
    expect(mainSource.indexOf("app.use(router)")).toBeLessThan(
      mainSource.indexOf('app.mount("#app")'),
    );
  });

  it("сохраняет публичный OIDC callback и явные состояния session gate", () => {
    expect(appSource).toContain('<RouterView v-if="route.meta.public" />');
    expect(appSource).toContain("<AuthGate v-else />");
    expect(authGateSource).toContain("session.phase === 'checking'");
    expect(authGateSource).toContain('role="status"');
    expect(authGateSource).toContain("<ProblemNotice");
    expect(authGateSource).toContain('@click="startLogin"');
    expect(authGateSource).toContain("session.beginLogin().catch");
    expect(authGateSource).toContain('@retry="retryAuthentication"');
  });

  it("не наблюдает каталоги с результатами browser tests", () => {
    expect(viteConfigSource).toContain('"**/test-results/**"');
    expect(viteConfigSource).toContain('"**/playwright-report/**"');
  });

  it("не подавляет Vite preload error до bounded router recovery", () => {
    expect(mainSource).toContain('"vite:preloadError"');
    expect(mainSource).not.toContain("event.preventDefault()");
    expect(mainSource).not.toContain("window.location.reload()");
  });
});
