import { readFileSync } from "node:fs";
import { renderToString } from "@vue/server-renderer";
import { createSSRApp, defineComponent, h } from "vue";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import type { AssistantPlan } from "@/shared/api/generated/openapi/types.gen";
import Record from "./AssistantPlanRecord.vue";

function plan(state: AssistantPlan["state"] = "APPLIED"): AssistantPlan {
  return {
    ref: "pln_synthetic",
    conversationRef: "cnv_synthetic",
    version: 3,
    revision: 2,
    contentDigest: "a".repeat(64),
    state,
    auditSummary: "Проверенная настройка помощника",
    applied: state === "APPLIED",
    operations: [
      {
        ref: "op_synthetic",
        type: "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION",
        action: "UPDATE",
        title: "Режим поиска",
        summary: "Опубликованный режим",
        target: { kind: "AGENT", ref: "agt_synthetic", name: "Помощник" },
        parameters: {},
        before: {},
        after: {},
        selected: true,
        permitted: true,
        validationProblems: [],
      },
    ],
    nextActions: [],
    validationProblems: [],
  };
}

async function render(state: AssistantPlan["state"]) {
  let initialized = 0;
  const controls = defineComponent({
    setup() {
      initialized++;
      return () =>
        h(
          "button",
          { type: "button", "data-existing-control": "true" },
          "Открыть сущность",
        );
    },
  });
  const app = createSSRApp({
    render: () =>
      h(
        Record,
        { plan: plan(state), variant: 3 },
        { default: () => h(controls) },
      ),
  });
  app.use(i18n);
  return { html: await renderToString(app), initialized };
}

describe("AssistantPlanRecord", () => {
  it("сворачивает только подтверждённый APPLIED, оставляя readback и controls mounted", async () => {
    const { html, initialized } = await render("APPLIED");
    expect(html).toContain("<details");
    expect(html).toContain("<summary");
    expect(html).not.toMatch(/<details[^>]*\sopen(?:\s|=|>)/);
    expect(html).toContain("Режим поиска");
    expect(html).toContain('data-state="APPLIED"');
    expect(html).toContain("data-existing-control");
    expect(initialized).toBe(1);
    expect(html.indexOf("data-existing-control")).toBeGreaterThan(
      html.indexOf("</summary>"),
    );
  });

  it.each(["DRAFT", "VALID", "INVALID", "STALE", "REJECTED"] as const)(
    "%s не скрывает reviewed controls и не объявляет план применённым",
    async (state) => {
      const { html, initialized } = await render(state);
      expect(html).not.toContain("<details");
      expect(html).toContain('<section class="assistant-plan-card"');
      expect(html).toContain("data-existing-control");
      expect(html).not.toContain('data-state="APPLIED"');
      expect(initialized).toBe(1);
    },
  );

  it("не интерпретирует operation title как HTML и не раскрывает внутренний ref в заголовке", async () => {
    const input = plan();
    const operation = input.operations[0];
    if (!operation) throw new Error("Synthetic operation is missing");
    operation.title = "<script>private-sentinel</script>";
    const app = createSSRApp({
      render: () => h(Record, { plan: input, variant: 1 }),
    });
    app.use(i18n);
    const html = await renderToString(app);
    const summary = html.slice(
      html.indexOf("<summary"),
      html.indexOf("</summary>"),
    );
    expect(summary).not.toContain("<script>");
    expect(summary).toContain("&lt;script&gt;");
    expect(summary).not.toContain("agt_synthetic");
    expect(summary).not.toContain("pln_synthetic");
  });

  it("сохраняет все существующие entity cards, плановые controls и хронологические rows в прежнем месте", () => {
    const workspace = readFileSync(
      new URL("./AssistantWorkspace.vue", import.meta.url),
      "utf8",
    );
    const record = workspace.slice(
      workspace.indexOf("<AssistantPlanRecord"),
      workspace.indexOf("</AssistantPlanRecord>"),
    );
    for (const component of [
      "AssistantRoleImageBuildCard",
      "AssistantCreatedEntityCard",
      "AssistantCreatedProjectFileCard",
      "AssistantInstructionDraftCard",
      "AssistantEnvironmentDraftCard",
      "AssistantAgentEnvironmentBindingCard",
      "AssistantIntegrationConnectionCard",
      "AssistantCreatedScheduleCard",
      "AssistantCreatedWorkflowCard",
      "AssistantLaunchedRunCard",
    ])
      expect(record).toContain(`<${component}`);
    expect(record).toContain("openPlan(turn.plan, $event)");
    expect(record).toContain(
      '@prepare-credential="credentialConnectionRef = $event"',
    );
    expect(workspace).toContain(':data-turn-sequence="turn.sequence"');
    expect(workspace).toContain(':key="turn.ref"');
    expect(workspace).toContain("<RunActivityView");
    expect(workspace).toContain(
      'v-if="showWorkingFallback && !store.loading && !store.problem"',
    );
    const source = readFileSync(
      new URL("./AssistantPlanRecord.vue", import.meta.url),
      "utf8",
    );
    expect(source).toContain(":focus-visible");
    expect(source).not.toContain("store.apply");
    expect(source).not.toContain("fetch(");
    expect(source).not.toContain("localStorage");
  });
});
