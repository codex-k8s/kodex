import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./RuntimeSecretRevokeDialog.vue", import.meta.url),
  "utf8",
);

describe("RuntimeSecretRevokeDialog layout", () => {
  it("не помещает сообщение об ошибке в узкую колонку сводки", () => {
    expect(source).toContain('class="revoke-dialog__problem"');
    expect(source).toContain('class="revoke-dialog__summary"');
    expect(source.indexOf('class="revoke-dialog__problem"')).toBeLessThan(
      source.indexOf('class="revoke-dialog__summary"'),
    );
    expect(source).toContain(".revoke-dialog__problem {");
    expect(source).toContain("width: 100%;");
  });

  it("оставляет иконку и текст в отдельной двухколоночной сводке", () => {
    expect(source).toContain("grid-template-columns: auto minmax(0, 1fr);");
    expect(source).toContain("overflow-wrap: anywhere;");
    expect(source).toContain("align-self: stretch;");
  });
});
