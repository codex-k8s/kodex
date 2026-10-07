import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = readFileSync(
  new URL("./IntegrationGrantsPanel.vue", import.meta.url),
  "utf8",
);
const styles = source.slice(source.indexOf("<style scoped>"));
function rule(selector: string): string {
  return (
    styles.slice(styles.indexOf(`\n${selector} {`) + 1).split("}")[0] ?? ""
  );
}

describe("IntegrationGrantsPanel: место для действия в фиксированной таблице", () => {
  it.each([880, 974, 1280])(
    "сохраняет полный бюджет колонок и кнопки при ширине %dpx",
    (tableWidth) => {
      const actionWidth = Number(
        /width:\s*(\d+)px/.exec(rule(".grant-table th:nth-child(5)"))?.[1],
      );
      expect(actionWidth).toBe(144);
      const ratios = [1, 2, 3, 4].map((column) => {
        const match = /width:\s*calc\(\(100% - (\d+)px\) \* ([\d.]+)\)/.exec(
          rule(`.grant-table th:nth-child(${column.toString()})`),
        );
        expect(match).not.toBeNull();
        expect(Number(match?.[1])).toBe(actionWidth);
        return Number(match?.[2]);
      });
      expect(ratios.reduce((sum, ratio) => sum + ratio, 0)).toBeCloseTo(1);
      expect(
        ratios.reduce(
          (sum, ratio) => sum + (tableWidth - actionWidth) * ratio,
          actionWidth,
        ),
      ).toBeCloseTo(tableWidth);
      const padding = /padding:\s*\d+px (\d+)px/.exec(
        rule(".grant-table th,\n.grant-table td"),
      )?.[1];
      expect(padding).toBe("12");
      // Нативно измеренная кнопка требует 115px; обе горизонтальные отбивки сохранены.
      expect(actionWidth - 2 * Number(padding)).toBeGreaterThanOrEqual(115);
    },
  );

  it("оставляет горизонтальную прокрутку внутри таблицы на узком экране", () => {
    expect(rule(".grant-table")).toContain("min-width: 880px");
    expect(rule(".grant-table")).toContain("table-layout: fixed");
    expect(rule(".grant-table-scroll")).toContain("overflow-x: auto");
    expect(rule(".grant-table-scroll")).toContain("min-width: 0");
  });

  it("не заменяет текст действия и не меняет существующий допуск или busy gate", () => {
    expect(source).toContain("item.enabled &&");
    expect(source).toContain(
      "connectionAllows(item.connection, 'MANAGE_GRANTS')",
    );
    expect(source).toContain(':disabled="busy"');
    expect(source).toContain('{{ t("integrations.revoke") }}');
    expect(source).toContain("@click=\"emit('revoke', item)\"");
    expect(rule(".grant-actions .button")).toContain("white-space: nowrap");
  });
});
