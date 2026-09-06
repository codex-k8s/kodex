import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const workspace = readFileSync(
  new URL("./ProviderAccountsWorkspace.vue", import.meta.url),
  "utf8",
);
const selector = readFileSync(
  new URL("./ProviderAccountSelector.vue", import.meta.url),
  "utf8",
);
const runtime = readFileSync(
  new URL("../agents/detail/AgentRuntimePanel.vue", import.meta.url),
  "utf8",
);

describe("provider account layout", () => {
  it("не показывает API key повторно и не предлагает ввод внутренних ref", () => {
    expect(workspace).toContain('type="password"');
    expect(workspace).toContain('autocomplete="off"');
    expect(workspace).toContain('apiKey.value = ""');
    expect(workspace).toContain('<section v-else class="authorization-panel">');
    expect(workspace).toContain('<form v-else class="provider-form"');
    expect(workspace).not.toContain('name="providerAccountRef"');
    expect(selector).not.toContain('placeholder="pacc_');
  });

  it("использует server search, cursor scroll и безопасные account actions", () => {
    expect(selector).toContain("AsyncEntityPicker");
    expect(selector).toContain(':load-items="loadAccounts"');
    expect(selector).toContain(":multiple=\"policyMode !== 'FIXED'\"");
    expect(selector).toContain("nextCursor: page.nextPageToken");
    expect(selector).toContain("eligibleForSelection");
    expect(workspace).toContain("accountAllows(account");
    expect(workspace).toContain("safeVerificationUri");
    expect(workspace).toContain("definitionsNextPageToken");
    expect(workspace).toContain("requestRevoke(account)");
  });

  it("подключает богатый selector к runtime без старой заглушки", () => {
    expect(runtime).toContain("<ProviderAccountSelector");
    expect(runtime).toContain('v-model="form.providerAccounts"');
    expect(runtime).not.toContain("accountCatalogUnavailable");
    expect(runtime).not.toContain("ServerOff");
    expect(runtime).toContain(
      '@eligibility-state-change="providerAccountEligibility = $event"',
    );
    expect(selector).toContain('"CONNECTING"');
    expect(selector).toContain("controller.abort()");
  });

  it("сохраняет стабильную responsive компоновку", () => {
    expect(workspace).toContain(
      "grid-template-columns: minmax(240px, 1fr) minmax(220px, 0.8fr) auto",
    );
    expect(workspace).toContain("@media (max-width: 560px)");
    expect(selector).toContain("min-width: min(430px, calc(100vw - 32px))");
  });
});
