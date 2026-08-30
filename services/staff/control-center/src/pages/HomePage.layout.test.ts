import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./HomePage.vue", import.meta.url), "utf8");
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("HomePage layout", () => {
  it("показывает полноширинное внимание раньше активной работы", () => {
    const attention = template.indexOf('class="home-attention-section"');
    const running = template.indexOf('class="home-running-section"');

    expect(attention).toBeGreaterThan(-1);
    expect(running).toBeGreaterThan(attention);
    expect(template).not.toContain("home-focus-grid");
  });

  it("не скрывает непокрытые источники за пустым результатом", () => {
    expect(template).toContain("<CapabilityCoverageList");
    expect(template).toContain('v-if="attention.length"');
    expect(template).toContain('class="home-section-empty"');
  });
});
