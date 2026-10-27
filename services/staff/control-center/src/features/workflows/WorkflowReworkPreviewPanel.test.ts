import { renderToString } from "@vue/server-renderer";
import { createSSRApp } from "vue";
import { expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import Panel from "./WorkflowReworkPreviewPanel.vue";
import Policy from "./WorkflowReworkPolicyControl.vue";
import type { OwnerGate } from "@/shared/api/generated/openapi/types.gen";

it.each(["ru", "en"] as const)("рисует exact план и отдельную supersession без private pins (%s)", async (locale) => {
  const previous = i18n.global.locale.value; i18n.global.locale.value = locale;
  const gate = { resolutionKind: "SUPERSEDED", reworkPlan: { schema: "kodex.workflow-upstream-rework-preview.v1", sourceStepKey: "review", sourceAttempt: 1, authors: [{ stepKey: "author-exact", previousAttempt: 1, nextAttempt: 2, originNodeId: "PRIVATE_NODE" }], rechecks: [{ stepKey: "qa-exact", previousAttempt: 1, nextAttempt: 2 }] } } as unknown as OwnerGate;
  const before = structuredClone(gate);
  try {
    const html = await renderToString(createSSRApp(Panel, { gate }).use(i18n));
    expect(html).toContain("author-exact"); expect(html).toContain("qa-exact"); expect(html).toContain("1 → 2");
    expect(html).not.toContain("PRIVATE_NODE"); expect(html).not.toContain("originNodeId"); expect(html).not.toContain("workflowResults.");
    expect(html).not.toContain("<button"); expect(gate).toEqual(before);
  } finally { i18n.global.locale.value = previous; }
});

it("показывает реальный disabled selector и сохраняет неизвестную политику", async () => {
  const html = await renderToString(createSSRApp(Policy, { value: "UNKNOWN_PROFILE", disabled: true }).use(i18n));
  expect(html).toContain("<select"); expect(html).toContain("disabled"); expect(html).toContain("UNKNOWN_PROFILE");
  expect(html).toContain("OWNER_APPROVED_UPSTREAM_V1"); expect(html).not.toContain("workflowResults.");
});
