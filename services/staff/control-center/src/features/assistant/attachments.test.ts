import { describe, expect, it } from "vitest";

import {
  formatAttachmentSize,
  removeAssistantAttachment,
  stageAssistantAttachments,
} from "@/features/assistant/attachments";

function file(
  name: string,
  size: number,
  type = "text/plain",
  lastModified = 1,
): File {
  return { name, size, type, lastModified } as File;
}

describe("assistant attachment composer model", () => {
  it("добавляет произвольное число файлов без продуктового лимита", () => {
    const files = Array.from({ length: 128 }, (_, index) =>
      file(`input-${String(index)}.txt`, index + 1, "text/plain", index),
    );

    expect(stageAssistantAttachments([], files)).toHaveLength(128);
  });

  it("не дублирует один и тот же browser file descriptor", () => {
    const source = file("contract.pdf", 4_096, "application/pdf", 42);

    expect(stageAssistantAttachments([], [source, source])).toHaveLength(1);
  });

  it("удаляет только выбранное локальное вложение", () => {
    const staged = stageAssistantAttachments(
      [],
      [file("a.txt", 1, "text/plain", 1), file("b.txt", 2, "text/plain", 2)],
    );

    expect(removeAssistantAttachment(staged, staged[0]?.key ?? "")).toEqual([
      staged[1],
    ]);
  });

  it("форматирует размер без чтения содержимого файла", () => {
    expect(formatAttachmentSize(512)).toBe("512 B");
    expect(formatAttachmentSize(2_048)).toBe("2.0 KB");
    expect(formatAttachmentSize(2_097_152)).toBe("2.0 MB");
  });
});
