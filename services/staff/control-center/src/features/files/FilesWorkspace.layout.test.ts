import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./FilesWorkspace.vue", import.meta.url),
  "utf8",
);

describe("FilesWorkspace contract", () => {
  it("выводит корзину из route mode и разделяет файлы, знания и результаты", () => {
    expect(source).toContain("mode: FileCollectionMode");
    expect(source).toContain(':to="trashMode ? filesPath : trashPath"');
    expect(source).toContain('value="KNOWLEDGE"');
    expect(source).toContain("artifactSourceKinds(collectionTab.value");
  });

  it("показывает массовое soft-delete как последовательные операции", () => {
    expect(source).toContain("mutateArtifactsSequentially");
    expect(source).toContain("@click=\"openSelectedBulk('DELETE')\"");
    expect(source).toContain("bulkReceipts");
  });

  it("не резервирует пустую details-колонку и показывает отменяемый progress", () => {
    expect(source).toContain("files-workspace__layout--details");
    expect(source).toContain("uploadArtifactItem");
    expect(source).toContain("uploadProgressPercent(item.progress)");
    expect(source).toContain("uploadControllers.get(id)?.abort()");
  });

  it("не принимает выбор файла до загрузки capability Проекта", () => {
    expect(source).toContain(':disabled="!canUpload || trashMode"');
  });

  it("занимает доступную ширину и отдаёт основную площадь коллекции", () => {
    expect(source).toContain(
      "grid-template-columns: minmax(0, 1fr) minmax(240px, 280px)",
    );
    expect(source).toContain('ref="scrollRoot"');
    expect(source).toContain('ref="sentinel"');
    expect(source).toContain("useCursorInfiniteScroll");
  });

  it("использует один toolbar для раздела, типа, состояния, источника и вида", () => {
    expect(source).toContain('v-model="activeTab"');
    expect(source).toContain('v-model="kind"');
    expect(source).toContain('v-model="scanState"');
    expect(source).toContain('v-model="source"');
    expect(source).toContain("<ViewModeToggle");
    expect(source).not.toContain("files-workspace__tabs");
  });
});
