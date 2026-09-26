import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./AppShell.vue", import.meta.url), "utf8");
const bootstrapSource = readFileSync(
  new URL("../main.ts", import.meta.url),
  "utf8",
);

describe("AppShell navigation", () => {
  it("даёт глобальному поиску стабильное имя поля", () => {
    expect(source).toContain('name="global-search"');
    expect(source).toContain("useAdaptiveCursorPageSize");
    expect(source).toContain("useCursorInfiniteScroll");
    expect(source).toContain("platform.searchNextPageToken");
    expect(source).toContain("platform.loadMoreSearch(searchPageSize.value)");
  });

  it("оставляет Kodex только глобальным FAB и drawer", () => {
    expect(source).not.toContain("assistant-entry");
    expect(source).toContain("<AssistantWorkspace");
    expect(source).not.toContain("openAssistantWorkspace");
  });

  it("запускает realtime до readback и не загружает полный каталог Проектов в оболочке", () => {
    expect(source).toContain("<RealtimeStatus");
    expect(source).toContain("router.isReady().then");
    expect(source).toContain("selectProjectRef(projectRef.value)");
    expect(source).toContain("realtime.openPlatform()");
    expect(source.indexOf("realtime.openPlatform()")).toBeLessThan(
      source.indexOf("platform.loadPendingGateCount()"),
    );
    expect(source).not.toContain("platform.loadGates()");
    expect(source).not.toContain("platform.loadProjects()");
    expect(source).not.toContain("]).finally(() => {");
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
