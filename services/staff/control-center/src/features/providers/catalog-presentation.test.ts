import { describe, expect, it } from "vitest";
import { catalogDate, orderedReasoningEfforts } from "./catalog-presentation";

describe("model catalog presentation", () => {
  it("упорядочивает усилия по возрастанию без изменения каталога", () => {
    const input = ["high", "low", "max", "medium", "ultra", "xhigh"];
    expect(orderedReasoningEfforts(input)).toEqual([
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
      "ultra",
    ]);
    expect(input).toEqual(["high", "low", "max", "medium", "ultra", "xhigh"]);
  });
  it("сохраняет новые значения провайдера после известных", () => {
    expect(
      orderedReasoningEfforts(["future", "high", "none", "minimal", "low"]),
    ).toEqual(["none", "minimal", "low", "high", "future"]);
  });
  it("форматирует дату согласно выбранной локали", () => {
    expect(catalogDate("2026-10-03T12:00:00Z", "ru")).toContain("2026");
    expect(catalogDate("2026-10-03T12:00:00Z", "en")).toContain("Oct");
    expect(catalogDate("2026-10-03T12:00:00Z", "ru")).not.toEqual(
      catalogDate("2026-10-03T12:00:00Z", "en"),
    );
  });
  it("не выводит отсутствующие и повреждённые даты", () => {
    expect(catalogDate(undefined, "ru")).toBe("");
    expect(catalogDate("invalid", "ru")).toBe("");
  });
});
