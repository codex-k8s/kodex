import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);
const styles = source.slice(source.indexOf("<style scoped>"));

describe("AssistantWorkspace layout", () => {
  it("сохраняет controls диалога в постоянном header", () => {
    const header = template.indexOf(
      '<header class="assistant-drawer__header">',
    );
    const planEditor = template.indexOf("<AssistantPlanEditor");
    const headerMarkup = template.slice(header, planEditor);

    expect(header).toBeGreaterThan(-1);
    expect(header).toBeLessThan(planEditor);
    expect(headerMarkup).toContain(
      ":aria-label=\"$t('assistant.newConversation')\"",
    );
    expect(headerMarkup).toContain(":aria-label=\"$t('assistant.history')\"");
    expect(headerMarkup).toContain(":aria-label=\"$t('common.close')\"");
  });

  it("ограничивает desktop drawer диапазоном D12-A", () => {
    expect(styles).toMatch(/width:\s*clamp\(520px,\s*42vw,\s*640px\)/);
    expect(styles).toMatch(/max-width:\s*calc\(100vw\s*-\s*64px\)/);
  });

  it("переключает drawer в mobile bottom sheet", () => {
    const mobile = styles.slice(styles.indexOf("@media (max-width: 720px)"));

    expect(mobile).toMatch(/\.assistant-drawer\s*\{[\s\S]*?left:\s*0/);
    expect(mobile).toMatch(/top:\s*auto/);
    expect(mobile).toMatch(/bottom:\s*0/);
    expect(mobile).toMatch(/width:\s*100%/);
    expect(mobile).toMatch(/height:\s*min\(88dvh,\s*900px\)/);
    expect(mobile).toMatch(/border-radius:\s*14px 14px 0 0/);
  });

  it("оставляет scroll только логу и закрепляет composer", () => {
    expect(styles).toMatch(
      /\.assistant-chat-log\s*\{[\s\S]*?flex:\s*1 1 auto[\s\S]*?overflow:\s*auto/,
    );
    expect(styles).toMatch(
      /\.assistant-composer\s*\{[\s\S]*?position:\s*sticky[\s\S]*?bottom:\s*0/,
    );
  });
});
