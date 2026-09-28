import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./OpenAPIImportDialog.vue", import.meta.url),
  "utf8",
);

describe("OpenAPIImportDialog layout", () => {
  it("именует все поля защищённого импорта для browser diagnostics", () => {
    expect(source).toContain('name="openapi-import-file"');
    expect(source).toContain('name="openapi-import-source"');
    expect(source).toContain('name="openapi-import-name"');
    expect(source).toContain('name="openapi-import-version"');
    expect(source).toContain(':name="`openapi-import-operation-${index}`"');
    expect(source).toContain(':name="`openapi-import-risk-${index}`"');
    expect(source).toContain(':name="`openapi-import-approval-${index}`"');
    expect(source).toContain('name="openapi-import-health-operation"');
  });
});
