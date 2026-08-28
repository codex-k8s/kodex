import { describe, expect, it } from "vitest";

import {
  fitRunGraphView,
  zoomRunGraphAtPoint,
} from "@/features/runs/run-graph-viewport";

describe("run graph viewport", () => {
  it("вписывает граф по центру доступной области", () => {
    expect(
      fitRunGraphView(
        { width: 1000, height: 500 },
        { width: 600, height: 500 },
        0.25,
        1.5,
        20,
      ),
    ).toEqual({ x: 20, y: 110, scale: 0.56 });
  });

  it("сохраняет точку под курсором при масштабировании", () => {
    const next = zoomRunGraphAtPoint(
      { x: 40, y: 30, scale: 1 },
      1.5,
      { x: 240, y: 180 },
      0.25,
      1.8,
    );

    expect(next).toEqual({ x: -60, y: -45, scale: 1.5 });
  });

  it("ограничивает масштаб установленным диапазоном", () => {
    expect(
      zoomRunGraphAtPoint({ x: 0, y: 0, scale: 1 }, 4, { x: 0, y: 0 }, 0.4, 1.6)
        .scale,
    ).toBe(1.6);
  });
});
