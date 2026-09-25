import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const fields = readFileSync(
  new URL("./ProjectFormFields.vue", import.meta.url),
  "utf8",
);
const manual = readFileSync(
  new URL("../../pages/ProjectsPage.vue", import.meta.url),
  "utf8",
);
const assistant = readFileSync(
  new URL("../assistant/components/AssistantPlanEditor.vue", import.meta.url),
  "utf8",
);

describe("Общая форма проекта", () => {
  it("используется ручным созданием и предпросмотром помощника", () => {
    expect(manual).toContain("<ProjectFormFields");
    expect(assistant).toContain("<ProjectFormFields");
    expect(assistant).toContain(
      "projectFormValidity.value[operation.value.ref] === true",
    );
  });

  it("сохраняет одни и те же лимиты и проверку обязательных полей", () => {
    expect(fields).toContain('maxlength="120"');
    expect(fields).toContain('maxlength="1000"');
    expect(fields).toContain("props.name.trim().length > 0");
    expect(fields).toContain("props.purpose.trim().length > 0");
  });
});
