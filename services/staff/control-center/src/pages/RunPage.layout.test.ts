import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./RunPage.vue", import.meta.url), "utf8");
const desktop = source.slice(
  source.indexOf("@media (min-width: 761px)"),
  source.indexOf("@media (max-width: 760px)"),
);
const mobile = source.slice(source.indexOf("@media (max-width: 760px)"));
const canvas = readFileSync(
  new URL("../features/runs/RunGraphCanvas.vue", import.meta.url),
  "utf8",
);

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
    expect(mobile).toMatch(/\.run-canvas-summary\s*\{\s*top: 136px;/);
    expect(mobile).toMatch(
      /\.run-workspace-toolbar,\s*\.run-workspace--activity \.run-workspace-toolbar\s*\{\s*top: 8px;/,
    );
    expect(source).toContain(':compact="activityOpen"');
    expect(source).not.toContain(':key="activityOpen ?');
    expect(mobile).toMatch(
      /\.run-workspace--activity \.graph-panel\s*\{\s*right: 0;/,
    );
  });

  it("резервирует фактическую сводку только для scroll области списка", () => {
    expect(source).toContain('ref="canvasSummary"');
    expect(source).toContain("observeRunOutlineClearance(summary,");
    expect(source).toContain("'--run-outline-summary-bottom'");
    const outline = canvas.match(/\.graph-outline \{([^}]+)\}/)?.[1];
    expect(outline).toContain(
      "top: max(144px, calc(var(--run-outline-summary-bottom, 0px) + 12px))",
    );
    expect(outline).toContain("padding: 14px;");
    // Легенда есть только в graph mode: outline не резервирует её пустые100px.
    expect(canvas).toContain("v-if=\"nodes.length && viewMode === 'graph'\"");
    expect(outline).toContain("scroll-padding-top: 14px");
    const viewport = canvas.match(/\.graph-viewport \{([^}]+)\}/)?.[1];
    expect(viewport).toContain("inset: 0");
    expect(viewport).not.toContain("--run-outline-summary-bottom");
    expect(canvas).toContain("v-show=\"viewMode === 'graph'\"");
  });

  it("сохраняет одну полную строку списка под раскрытой мобильной сводкой", () => {
    expect(mobile).toContain("max-height: max(46px, calc(100% - 280px))");
    expect(mobile).toContain("grid-template-rows: auto minmax(0, 1fr)");
    const details = mobile.match(
      /\.run-canvas-summary__details--expanded \{([^}]+)\}/,
    )?.[1];
    expect(details).toContain("min-height: 0");
    expect(details).toContain("overflow: auto");
    for (const workspaceHeight of [371, 420, 600]) {
      const summaryHeight = Math.max(46, workspaceHeight - 280);
      const outlineHeight = workspaceHeight - 136 - summaryHeight - 12;
      expect(outlineHeight).toBeGreaterThanOrEqual(104 + 28);
    }
  });

  it("не обрезает статус узла общей фиксированной высотой кнопки", () => {
    const node = canvas.match(/\.graph-outline-node \{([^}]+)\}/)?.[1];
    expect(node).toContain("height: auto");
    const preview = canvas.match(
      /\.graph-outline-node__connection \{([^}]+)\}/,
    )?.[1];
    expect(preview).toContain("text-overflow: ellipsis");
    expect(preview).toContain("white-space: nowrap");
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
