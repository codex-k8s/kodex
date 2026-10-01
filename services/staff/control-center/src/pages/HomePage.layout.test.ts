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
    expect(template).toContain('kind="RUN"');
    expect(template).not.toContain('v-show="showRuns"');
  });

  it("разделяет доступные источники и использует реальный статус провайдера", () => {
    expect(template).toContain(':gates="openGates"');
    expect(template).toContain(':failed-runs="failedRuns"');
    expect(template).toContain(
      ':provider-accounts="providerAccountsNeedingAuthorization"',
    );
    expect(template).toContain('kind="SESSION"');
    expect(template).not.toContain("CapabilityCoverageList");
    expect(template).not.toContain("PROVIDER_AUTH_EXPIRY");
    expect(source).toContain('account.state === "REAUTHORIZATION_REQUIRED"');
    expect(source).toContain('platform.realtimeSnapshot("PROVIDER_ACCOUNT")');
    expect(template).not.toContain("provider-next-page-token");
    expect(template).not.toContain("more-providers");
    expect(template).toContain('class="home-dashboard"');
    expect(template).toContain("dashboard");
    expect(template).not.toContain("HomeGateCatalog");
  });

  it("читает стартовые каталоги из realtime store без HTTP readback", () => {
    expect(source).toContain('platform.realtimeSnapshot("PROJECT")');
    expect(source).toContain('platform.realtimeSnapshot("RUN")');
    expect(source).not.toContain("platform.loadOverview()");
    expect(source).not.toContain("platform.loadRuns()");
    expect(source).not.toContain("listProviderAccounts");
    expect(source).not.toContain('searchProjects(""');
    expect(source).not.toContain("location.reload");
    expect(source).not.toContain("router.go");
  });
});
