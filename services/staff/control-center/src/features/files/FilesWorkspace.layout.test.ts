import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./FilesWorkspace.vue", import.meta.url),
  "utf8",
);

describe("FilesWorkspace contract", () => {
  it("выводит корзину из route mode и не показывает ложный режим знаний", () => {
    expect(source).toContain("mode: FileCollectionMode");
    expect(source).toContain(':to="trashMode ? filesPath : trashPath"');
    expect(source).not.toContain('value="KNOWLEDGE"');
  });

  it("показывает массовое soft-delete как последовательные операции", () => {
    expect(source).toContain("mutateArtifactsSequentially");
    expect(source).toContain("@click=\"openSelectedBulk('DELETE')\"");
    expect(source).toContain("bulkReceipts");
  });
});
