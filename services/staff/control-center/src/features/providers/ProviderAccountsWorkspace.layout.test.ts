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
const lifecycle = readFileSync(
  new URL("./ProviderAccountLifecyclePanel.vue", import.meta.url),
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
    expect(workspace).toContain("search.trim()");
    expect(workspace).toContain('"providers.searchEmptyTitle"');
    expect(workspace).toContain('"providers.searchEmptyText"');
    expect(workspace).toContain("requestRevoke(account)");
    expect(workspace).toContain('account.state === "AUTHORIZED"');
    expect(workspace).toContain('"providers.reauthorize"');
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

  it("показывает реестры таблицами с безопасными действиями и строковым курсором", () => {
    expect(workspace).toContain(
      'v-if="accounts.length > 6 || accountsNextPageToken"',
    );
    expect(workspace).toContain('class="provider-readiness__table"');
    expect(workspace).toContain('class="provider-account-list__table"');
    expect(workspace).toContain('itemSelector: ".provider-account-row"');
    expect(workspace).toContain("estimatedColumns: 1");
    expect(workspace).toContain("account.authorization?.method");
    expect(workspace).not.toContain("provider-account-card");
    expect(workspace).toContain("@media (max-width: 560px)");
    expect(selector).toContain("min-width: min(430px, calc(100vw - 32px))");
  });

  it("получает фоновые verification и deletion переходы из realtime-снимка без HTTP polling", () => {
    expect(workspace).not.toContain("verificationTimer");
    expect(workspace).not.toContain("observeVerification");
    expect(workspace).not.toContain("loadProviderAccount");
    expect(lifecycle).not.toContain("pollTimer");
    expect(lifecycle).not.toContain("scheduleObservation");
    expect(lifecycle).toContain("async function rereadAccount()");
    expect(lifecycle).toContain('@click="rereadAccount"');
    expect(lifecycle).toContain(
      "await loadProviderAccount(props.account.ref, signal)",
    );
    expect(lifecycle).toContain("props.account.deletion?.version");
  });

  it("переносит длинный статус внутри ячейки, сохраняя индикатор и локальный scroll таблицы", () => {
    expect(workspace).toMatch(
      /\.provider-account-row :deep\(\.status-badge\)\s*\{[^}]*max-width: 100%;[^}]*white-space: normal;/,
    );
    expect(workspace).toMatch(
      /\.provider-account-row :deep\(\.status-badge__dot\)\s*\{\s*flex-shrink: 0;/,
    );
    expect(workspace).toMatch(
      /\.provider-account-list\s*\{[^}]*min-width: 0;[^}]*overflow: auto;/,
    );
    expect(workspace).toContain("min-width: 1120px;");
  });
});
