import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./HomePage.vue", import.meta.url), "utf8");
const template = source.slice(
  source.indexOf("<template>"),
  source.indexOf("<style scoped>"),
);

describe("HomePage layout", () => {
  it("показывает полноширинное внимание раньше активной работы", () => {
    const attention = template.indexOf("<HomeAttentionCenter");
    const running = template.indexOf('class="home-running-section"');

    expect(attention).toBeGreaterThan(-1);
    expect(running).toBeGreaterThan(attention);
    expect(template).not.toContain("home-focus-grid");
  });

  it("разделяет доступные источники и не рисует недостоверные provider-карточки", () => {
    expect(template).toContain(':gates="openGates"');
    expect(template).toContain(':failed-runs="failedRuns"');
    expect(template).toContain('kind="SESSION"');
    expect(template).not.toContain("CapabilityCoverageList");
    expect(template).not.toContain("PROVIDER_AUTH_EXPIRY");
  });

  it("обновляет данные через store без route reload", () => {
    expect(source).toContain("platform.loadOverview()");
    expect(source).toContain("platform.loadRuns()");
    expect(source).not.toContain("location.reload");
    expect(source).not.toContain("router.go");
  });
});
