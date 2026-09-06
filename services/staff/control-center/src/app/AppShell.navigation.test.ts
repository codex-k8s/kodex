import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./AppShell.vue", import.meta.url), "utf8");
const bootstrapSource = readFileSync(
  new URL("../main.ts", import.meta.url),
  "utf8",
);

describe("AppShell navigation", () => {
  it("оставляет Kodex только глобальным FAB и drawer", () => {
    expect(source).not.toContain("assistant-entry");
    expect(source).toContain("<AssistantWorkspace");
    expect(source).not.toContain("openAssistantWorkspace");
  });

  it("использует одну realtime-индикацию без route reload", () => {
    expect(source).toContain("<RealtimeStatus");
    expect(source).toContain("realtime.openPlatform()");
    expect(source).not.toContain("offline-banner");
    expect(source).not.toContain("location.reload");
  });

  it("не перезагружает страницу автоматически при ошибке загрузки chunk", () => {
    expect(bootstrapSource).toContain('new Event("kodex:preload-error")');
    expect(bootstrapSource).not.toContain("window.location.reload");
    expect(bootstrapSource).not.toContain("preloadRecoveryKey");
    expect(source).toContain('v-if="preloadFailed"');
    expect(source).toContain("refreshAfterPreloadFailure");
  });

  it("держит async project picker между брендом и поиском", () => {
    expect(source).not.toContain('id="sidebar-project-switcher"');
    expect(source).toContain("<ProjectPicker");
    expect(source.indexOf('class="topbar-project-picker"')).toBeLessThan(
      source.indexOf('class="global-search-wrap"'),
    );
    expect(source).toContain('class="global-search-wrap"');
  });
});
