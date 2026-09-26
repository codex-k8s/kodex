import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const binding = readFileSync(
  new URL("./BindingEditorDialog.vue", import.meta.url),
  "utf8",
);
const effective = readFileSync(
  new URL("./EffectiveAccessPanel.vue", import.meta.url),
  "utf8",
);

describe("access entity pickers", () => {
  it("ищет участников и роли на сервере в формах назначения", () => {
    expect(binding).toContain(':load-page="loadSubjects"');
    expect(binding).toContain(':load-page="loadRoles"');
    expect(binding).toContain(':context-key="form.subjectKind"');
    expect(binding).not.toContain('name="access-binding-subject"');
    expect(binding).not.toContain('name="access-binding-role-version"');
  });

  it("ищет участников и роли на сервере при проверке доступа", () => {
    expect(effective).toContain(':load-page="loadSubjects"');
    expect(effective).toContain(':load-page="loadRoles"');
    expect(effective).not.toContain('name="access-effective-subject"');
    expect(effective).not.toContain('name="access-effective-role"');
  });
});
