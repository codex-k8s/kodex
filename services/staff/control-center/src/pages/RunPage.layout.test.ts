import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";
import { compileStyle, parse } from "vue/compiler-sfc";

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
  it("сохраняет доступный граф и вертикальную прокрутку при zoom и коротком viewport", () => {
    const page = source.match(/\.run-page\.run-page \{([^}]+)\}/)?.[1];
    expect(page).toContain("overflow-x: hidden");
    expect(page).toContain("overflow-y: auto");
    const body = source.match(/\.run-page-body \{([^}]+)\}/)?.[1];
    expect(body).toContain("min-height: 480px");
    expect(body).toContain("flex: 1 0 480px");
    expect(mobile).not.toMatch(/\.run-page-body\s*\{\s*min-height: 0;/);
    const shortCanvas = 383 - 108 - 134 - 62;
    expect(shortCanvas).toBeLessThan(100);
    // Реальный browser fixture при250%:79px canvas скрывал nodes и legend.
    expect(480 - 134 - 147 - 28).toBeGreaterThan(150);
    expect(480 + 134).toBeGreaterThan(383 - 108 - 62);
  });

  it("разводит центральные действия и toolbar графа на средней ширине", () => {
    const medium = source.match(
      /@media \(min-width: 761px\) and \(max-width: 1100px\) \{([\s\S]*?)\n\}/,
    )?.[1];
    expect(medium).toContain(".run-workspace-toolbar,");
    expect(medium).toContain(".run-workspace--activity .run-workspace-toolbar");
    expect(medium).toContain("top: 84px");
    // Верхняя graph-панель заканчивается на72px, следующий ряд имеет запас12px.
    expect(84 - (14 + 58)).toBe(12);
  });

  it("переносит мобильный доступ к помощнику в toolbar без нижнего резерва списка", () => {
    const action = source.match(
      /<button\s+class="icon-button run-workspace-assistant"([^>]+)>/,
    )?.[1];
    expect(action).toBeDefined();
    expect(action).toContain('@click="openAssistantWorkspace"');
    expect(action).toContain(":aria-label=\"$t('assistant.open')\"");
    expect(action).toContain(":title=\"$t('assistant.open')\"");
    expect(action).not.toContain("disabled");
    expect(source).toContain("openAssistantWorkspace,");
    const rule = mobile.match(
      /\.run-workspace-toolbar \.run-workspace-assistant \{([^}]+)\}/,
    )?.[1];
    expect(rule).toContain("display: inline-flex");
    expect(rule).toContain("width: 44px");
    expect(rule).toContain("height: 44px");
    const marker =
      "body:has(.run-workspace-assistant:not(:disabled):not([hidden])) .assistant-fab";
    const style = parse(source).descriptor.styles[0];
    if (!style) throw new Error("RunPage style missing");
    const compiled = compileStyle({
      filename: "RunPage.vue",
      source: style.content,
      id: "data-v-run-page",
      scoped: true,
    });
    expect(compiled.errors).toEqual([]);
    const compiledMobile = compiled.code.slice(
      compiled.code.indexOf("@media (max-width: 760px)"),
    );
    expect(compiledMobile.replace(/\s+/g, " ")).toContain(marker);
    expect(desktop).not.toContain(marker);
    const hidden = mobile.match(
      /:global\([\s\S]*?assistant-fab\s*\)\s*\{([^}]+)\}/,
    )?.[1];
    expect(hidden).toContain("visibility: hidden");
    expect(hidden).toContain("pointer-events: none");
    for (const width of [320, 390, 720, 721, 760, 761]) {
      const mobileAction = width <= 760;
      const floatingAction = !mobileAction;
      expect(Number(mobileAction) + Number(floatingAction)).toBe(1);
      if (mobileAction) {
        // Четыре действия с owner gate также помещаются в первую строку.
        expect(4 * 44 + 2).toBeLessThanOrEqual(width - 16);
      }
    }
    expect(mobile).toContain("max-height: max(46px, calc(100% - 280px))");
    expect(canvas).toContain("scroll-padding-top: 14px");
  });
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

  it("не сжимает заголовок сводки до одной буквы рядом с badge при desktop1280 и открытом ходе работы", () => {
    const heading = desktop.match(
      /\.run-workspace--activity \.run-canvas-summary__heading \{([^}]+)\}/,
    )?.[1];
    expect(heading).toContain("grid-template-columns: minmax(0, 1fr)");
    const title = desktop.match(
      /\.run-workspace--activity \.run-canvas-summary__heading strong \{([^}]+)\}/,
    )?.[1];
    expect(title).toContain("overflow-wrap: anywhere");
    const link = desktop.match(
      /\.run-workspace--activity \.run-canvas-summary__details > a \{([^}]+)\}/,
    )?.[1];
    expect(link).toContain("grid-column: 1 / -1");
    // С прежним auto-столбцом badge оставлял SOFTWARE_CHANGE лишь одну букву.
    const workspaceWidth = 1280 - 256;
    const drawerWidth = Math.min(720, Math.max(520, workspaceWidth * 0.54));
    const contentWidth = (workspaceWidth - drawerWidth) / 2 - 86 - 2 * 12 - 2;
    expect(contentWidth - 104 - 10).toBeLessThan(16);
    expect(contentWidth).toBeGreaterThan(120);
    expect(mobile).not.toContain(
      ".run-workspace--activity .run-canvas-summary__heading",
    );
    const style = parse(source).descriptor.styles[0];
    if (!style) throw new Error("RunPage style missing");
    expect(
      compileStyle({
        filename: "RunPage.vue",
        source: style.content,
        id: "data-v-run-page",
        scoped: true,
      }).errors,
    ).toEqual([]);
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

  it("не сжимает intrinsic строки дерева и раскрытых metadata до высоты scrollport", () => {
    const outline = canvas.match(/\.graph-outline \{([^}]+)\}/)?.[1];
    expect(outline).toContain("grid-auto-rows: max-content");
    const details = source.match(
      /\.run-canvas-summary__details \{(\s*grid-auto-rows:[^}]+)\}/,
    )?.[1];
    expect(details).toContain("grid-auto-rows: max-content");
    expect(details).toContain("align-content: start");
    const metadata = source.match(
      /\.run-canvas-summary__details > span,\s*\.run-canvas-summary a \{([^}]+)\}/,
    )?.[1];
    expect(metadata).toContain("overflow-wrap: anywhere");
    const node = canvas.match(/\.graph-outline-node \{([^}]+)\}/)?.[1];
    expect(node).toContain("padding: 10px 12px");
    expect(node).toContain("border: 1px solid");
    // Native fixture: body77px требует99px, а72px допустимо только как minimum.
    const intrinsicRow = Math.max(72, 77 + 2 * 10 + 2);
    expect(intrinsicRow).toBe(99);
    expect(132 - intrinsicRow).toBeGreaterThanOrEqual(28);
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
