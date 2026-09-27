import { expect, it } from "vitest";
import { runTitleFromTask } from "./run-title";

it("ограничивает автоматический заголовок байтами UTF-8 без усечения текста задания", () => {
  const task = "Проверить интеграцию. ".repeat(20);
  const title = runTitleFromTask(task);
  expect(title.length).toBeLessThan(task.length);
  expect(new TextEncoder().encode(title).length).toBeLessThanOrEqual(240);
  expect(title).not.toContain("�");
});

it("сохраняет символы Unicode целыми и ограничивает ASCII по символам", () => {
  expect(runTitleFromTask("🙂".repeat(80))).toBe("🙂".repeat(60));
  expect(runTitleFromTask("a".repeat(300))).toBe("a".repeat(160));
  expect(
    runTitleFromTask("слово ".repeat(100))
      .split(" ")
      .every((word) => word === "слово"),
  ).toBe(true);
});
