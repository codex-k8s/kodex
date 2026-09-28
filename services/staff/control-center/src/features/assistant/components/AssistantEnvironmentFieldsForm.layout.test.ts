import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const assistant = readFileSync(
  new URL("./AssistantEnvironmentFieldsForm.vue", import.meta.url),
  "utf8",
);
const manual = readFileSync(
  new URL("../../../pages/RuntimeEnvironmentEditorPage.vue", import.meta.url),
  "utf8",
);
const shared = readFileSync(
  new URL(
    "../../runtime/RuntimeEnvironmentFieldListsEditor.vue",
    import.meta.url,
  ),
  "utf8",
);

describe("Поля окружения в чате и ручном редакторе", () => {
  it("использует одни и те же поля и выбор секрета из текущего проекта", () => {
    expect(assistant).toContain("<RuntimeEnvironmentFieldListsEditor");
    expect(manual).toContain("<RuntimeEnvironmentFieldListsEditor");
    expect(shared).toMatch(/loadRuntimeSecretPage\(\s*props\.projectRef/);
    expect(manual).toContain('mode="SECRETS"');
  });

  it("не переносит значение секрета в план и закрывает применение неверного списка", () => {
    expect(assistant).toContain("sensitiveName.test(item.name)");
    expect(assistant).toContain('emit("valid", valid)');
    expect(shared).not.toContain('v-model="item.secretValue"');
  });
});
