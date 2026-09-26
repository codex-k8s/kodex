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
const assistant = readFileSync(
  new URL("../assistant/components/AssistantWorkspace.vue", import.meta.url),
  "utf8",
);

describe("вход из помощника в защищённую форму секрета", () => {
  it("отдаёт маршрут возврата только помощнику и не открывает второй диалог на странице", () => {
    expect(page).toContain(':project-ref="projectRef"');
    expect(page).not.toContain("assistantCreateSecret");
    expect(workspace).not.toContain("assistantCreateSecret");
    expect(workspace).not.toContain("consumeRuntimeSecretReauthSuggestion");
    expect(workspace).toContain(
      '<RuntimeSecretDraftDialog\n    v-if="createOpen"',
    );
    expect(assistant).toContain(
      'const creating = route.query.assistantCreateSecret === "1"',
    );
    expect(assistant).toContain('surface: "assistant"');
    expect(assistant).toContain(
      'v-if="open && secretDialogOpen && projectRef"',
    );
    expect(page).not.toContain("secretValue");
    expect(page).not.toContain("credentialValue");
  });
});
