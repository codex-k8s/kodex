import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./AppShell.vue", import.meta.url), "utf8");

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

  it("держит project switcher в sidebar и освобождает header для поиска", () => {
    expect(source).not.toContain("topbar-project-switcher");
    expect(source).toContain(
      '<div class="project-switcher project-switcher--sidebar">',
    );
    expect(source).toContain('class="global-search-wrap"');
  });
});
