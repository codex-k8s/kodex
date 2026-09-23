import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./AssistantCapabilityPlanForm.vue", import.meta.url),
  "utf8",
);
const editor = readFileSync(
  new URL("./AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("форма права сотрудника в плане помощника", () => {
  it("проверяет сотрудника, проект, версию и серверный каталог", () => {
    expect(source).toContain("getAgent");
    expect(source).toContain("listPlatformCapabilities");
    expect(source).toContain("next.projectRef !== projectRef");
    expect(source).toContain(
      "props.operation.value.target.ref === agentRef.value",
    );
    expect(source).toContain("versionMatches.value");
    expect(source).toContain("selectedCapability.value");
    expect(editor).toContain("<AssistantCapabilityPlanForm");
    expect(editor).toContain(
      "capabilityFormValidity.value[operation.value.ref]",
    );
    expect(editor).toContain("!capabilityFormTouched.value");
  });
});
