import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const page = readFileSync(
  new URL("../../pages/RuntimeSecretsPage.vue", import.meta.url),
  "utf8",
);
const workspace = readFileSync(
  new URL("./RuntimeSecretsWorkspace.vue", import.meta.url),
  "utf8",
);

describe("вход из помощника в защищённую форму секрета", () => {
  it("использует только флаг открытия и текущий проект, не принимает значение из URL", () => {
    expect(page).toContain('route.query.assistantCreateSecret === "1"');
    expect(page).toContain(':project-ref="projectRef"');
    expect(page).toContain(':assistant-create-secret="assistantCreateSecret"');
    expect(workspace).toContain(
      "if (requested && !createOpen.value) openCreate()",
    );
    expect(workspace).toContain(
      '<RuntimeSecretDraftDialog\n    v-if="createOpen"',
    );
    expect(page).not.toContain("secretValue");
    expect(page).not.toContain("credentialValue");
  });
});
