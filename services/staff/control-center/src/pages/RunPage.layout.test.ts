import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./RunPage.vue", import.meta.url), "utf8");
const desktop = source.slice(
  source.indexOf("@media (min-width: 761px)"),
  source.indexOf("@media (max-width: 760px)"),
);
const mobile = source.slice(source.indexOf("@media (max-width: 760px)"));

describe("RunPage layout при открытом ходе работы", () => {
  it("ограничивает сводку доступной половиной графа с запасом для трёх действий", () => {
    const width = desktop.match(
      /width: min\(\s*(\d+)px,\s*calc\(\(100% - var\(--run-activity-width\)\) \/ 2 - (\d+)px\)\s*\)/,
    );
    expect(width).not.toBeNull();
    if (!width) return;
    const maximum = Number(width[1]);
    const drawer = source.match(
      /--run-activity-width: clamp\((\d+)px, (\d+)%, (\d+)px\)/,
    );
    expect(drawer).not.toBeNull();
    if (!drawer) return;
    const drawerMinimum = Number(drawer[1]);
    const drawerPercent = Number(drawer[2]);
    const drawerMaximum = Number(drawer[3]);
    const reserve = Number(width[2]);
    for (const workspaceWidth of [1100, 1435, 1800]) {
      const canvasWidth =
        workspaceWidth -
        Math.min(
          drawerMaximum,
          Math.max(drawerMinimum, (workspaceWidth * drawerPercent) / 100),
        );
      const summaryWidth = Math.min(maximum, canvasWidth / 2 - reserve);
      const summaryRight = 14 + summaryWidth;
      const toolbarLeft = canvasWidth / 2 - 100 / 2;
      expect(summaryWidth).toBeGreaterThan(0);
      expect(toolbarLeft - summaryRight).toBeGreaterThanOrEqual(22);
    }
  });

  it("оставляет центральные действия ниже верхней панели управления графом", () => {
    expect(desktop).toMatch(
      /\.run-workspace--activity \.run-canvas-summary\s*\{\s*top: 70px;/,
    );
    expect(desktop).toMatch(
      /\.run-workspace--activity \.run-workspace-toolbar\s*\{\s*top: 70px;/,
    );
    expect(source).toContain(
      "left: calc((100% - var(--run-activity-width)) / 2)",
    );
  });

  it("сохраняет отдельные мобильные строки и существующий режим fit графа", () => {
    expect(mobile).toMatch(/\.run-canvas-summary\s*\{\s*top: 62px;/);
    expect(mobile).toMatch(/\.run-workspace-toolbar\s*\{\s*top: 8px;/);
    expect(source).toContain(':compact="activityOpen"');
    expect(source).not.toContain(':key="activityOpen ?');
    expect(mobile).toMatch(
      /\.run-workspace--activity \.graph-panel\s*\{\s*right: 0;/,
    );
  });

  it("использует одну фактическую ширину drawer для границы canvas на desktop", () => {
    expect(source).toMatch(
      /\.run-workspace--activity \.graph-panel\s*\{\s*right: var\(--run-activity-width\);/,
    );
    expect(source).toMatch(
      /\.run-activity-overlay\s*\{[^}]*width: var\(--run-activity-width\);/,
    );
    expect(source).not.toContain("min-width: 520px;");
    for (const width of [761, 900, 1100, 1921]) {
      const drawerWidth = Math.min(720, Math.max(520, width * 0.54));
      expect(width - drawerWidth).toBeGreaterThan(0);
    }
  });
});
