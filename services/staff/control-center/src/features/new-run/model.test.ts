import { describe, expect, it } from "vitest";

import {
  fileExtension,
  fileVisualKind,
  formatFileSize,
} from "@/features/new-run/model";

describe("new-run file model", () => {
  it("определяет тип по имени и MIME без зависимости от регистра", () => {
    expect(fileExtension("Отчёт.XLSX", "application/octet-stream")).toBe(
      "xlsx",
    );
    expect(fileVisualKind("Отчёт.XLSX", "application/octet-stream")).toBe(
      "spreadsheet",
    );
    expect(fileVisualKind("photo", "image/webp")).toBe("image");
    expect(fileVisualKind("contract.pdf", "application/pdf")).toBe("pdf");
  });

  it("форматирует размер через локализованный Intl unit formatter", () => {
    expect(formatFileSize(1536, "en-US")).toBe("1.5 kB");
    expect(formatFileSize(2 * 1024 * 1024, "en-US")).toBe("2 MB");
  });
});
