import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const card = readFileSync(
  new URL("./AssistantAgentEnvironmentBindingCard.vue", import.meta.url),
  "utf8",
);
const form = readFileSync(
  new URL("./AssistantAgentEnvironmentBindingForm.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("привязка окружения через план помощника", () => {
  it("читает подтверждённый binding и показывает маршрут сотрудника", () => {
    expect(card).toContain("assistantAgentEnvironmentBindingTarget");
    expect(card).toContain(
      "view.value.environmentBinding.versionRef === target.value.versionRef",
    );
    expect(card).toContain("query: { tab: 'environment', assistantForm: '1' }");
    expect(card).not.toContain("emit('navigate')");
    expect(workspace).toContain(
      "item.type === 'BIND_AGENT_RUNTIME_ENVIRONMENT'",
    );
  });

  it("выбирает только готовые окружения точного проекта без поля секрета", () => {
    expect(form).toContain("item.projectRef === props.projectRef");
    expect(form).toContain('item.state === "ACTIVE"');
    expect(form).toContain("item.ready");
    expect(form).toContain("getRuntimeEnvironmentSet");
    expect(form).toContain("getAgent");
    expect(form).not.toContain("secretValue");
  });
});
