import { describe, expect, it } from "vitest";

import { runGraphViewportCommand } from "@/features/runs/run-graph-viewport";

describe("run graph viewport", () => {
  it("сопоставляет клавиши с fit и масштабированием Vue Flow", () => {
    expect(runGraphViewportCommand({ key: "0" })).toEqual({ type: "FIT" });
    expect(runGraphViewportCommand({ key: "+" })).toEqual({
      type: "ZOOM_IN",
    });
    expect(runGraphViewportCommand({ key: "=" })).toEqual({
      type: "ZOOM_IN",
    });
    expect(runGraphViewportCommand({ key: "-" })).toEqual({
      type: "ZOOM_OUT",
    });
  });

  it("поддерживает клавиатурное перемещение viewport", () => {
    expect(runGraphViewportCommand({ key: "ArrowLeft" })).toEqual({
      type: "PAN",
      x: 42,
      y: 0,
    });
    expect(
      runGraphViewportCommand({ key: "ArrowDown", shiftKey: true }),
    ).toEqual({ type: "PAN", x: 0, y: -96 });
  });

  it("не перехватывает посторонние клавиши", () => {
    expect(runGraphViewportCommand({ key: "Enter" })).toBeUndefined();
  });
});
