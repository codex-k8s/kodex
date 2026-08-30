import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./AppShell.vue", import.meta.url), "utf8");

describe("AppShell navigation", () => {
  it("оставляет Kodex только глобальным FAB и drawer", () => {
    expect(source).not.toContain("assistant-entry");
    expect(source).toContain("<AssistantWorkspace");
    expect(source).not.toContain("openAssistantWorkspace");
  });
});
