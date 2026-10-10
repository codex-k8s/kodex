import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it, vi } from "vitest";
vi.mock("@/shared/locale", () => ({ currentLocale: () => "ru" }));
import { i18n } from "@/app/i18n";
import type { AssistantPlanOperation } from "@/shared/api/generated/openapi/types.gen";
import { platformCapabilityMessages } from "@/shared/ui/server-message-catalog";
import Card from "./AssistantPlanCard.vue";

function operation(index: number): AssistantPlanOperation {
  return {
    ref: `op_${String(index)}`,
    type: "CHANGE_INTEGRATION_GRANT",
    action: "UPDATE",
    title: "Включить прежний grant",
    summary: "Разрешение",
    target: {
      kind: "INTEGRATION_CONNECTION",
      name: "Kodex | Dev · GitHub",
      ref: "int_private",
    },
    parameters: {
      capabilityKey: `github.read_${String(index)}`,
      agentRef: "agt_private",
      enabled: true,
      approvalPolicy: "NONE",
    },
    before: { agentName: "Менеджер" },
    after: {},
    selected: true,
    permitted: true,
    validationProblems: [],
  };
}
async function render(operations: AssistantPlanOperation[]) {
  const app = createSSRApp({
    render: () =>
      h(
        Card,
        { operations },
        {
          operation: ({ operation }: { operation: AssistantPlanOperation }) =>
            h("p", operation.summary),
        },
      ),
  });
  app.use(i18n);
  return renderToString(app);
}
describe("AssistantPlanCard", () => {
  it.each([true, false])(
    "показывает точные последствия платформенного права без изменения summary, enabled=%s",
    async (enabled) => {
      const item = operation(0);
      item.type = "CHANGE_CAPABILITY";
      item.title = "platform.artifact.manage";
      item.summary =
        "Сотрудник agt_private сможет изменить файлы; <script>не выполнять</script>";
      item.target = { kind: "AGENT", name: "Аналитик", ref: "agt_private" };
      item.expectedVersion = 17;
      item.parameters = {
        agentRef: "agt_private",
        capabilityKey: "platform.artifact.manage",
        enabled,
      };
      item.before = { enabled: !enabled };
      item.after = { enabled };
      const original = structuredClone(item);
      const html = await render([item]);
      const visible = html.slice(0, html.indexOf("<details"));
      expect(visible).toContain("Аналитик · Работа с файлами");
      expect(visible).toContain(enabled ? "Выдать" : "Отозвать");
      expect(visible).toContain(
        "Чтение, создание, изменение и удаление файлов",
      );
      expect(visible).toContain("в разрешённой области");
      expect(visible).toContain("&lt;script&gt;не выполнять&lt;/script&gt;");
      expect(visible).not.toContain("platform.artifact.manage");
      expect(html).toContain("Технические детали изменения");
      expect(html).toContain("platform.artifact.manage");
      expect(html).toContain("agt_private");
      expect(html).toContain("17");
      expect(html).not.toMatch(/<details[^>]*\sopen(?:\s|=|>)/);
      expect(item).toEqual(original);
    },
  );
  it.each(["platform.unknown.manage", "constructor", "__proto__"])(
    "не угадывает подпись неизвестного права %s",
    async (key) => {
      const item = operation(0);
      item.type = "CHANGE_CAPABILITY";
      item.parameters.capabilityKey = key;
      item.summary = "Полное описание неизвестного права";
      expect(platformCapabilityMessages(key)).toBeUndefined();
      expect(await render([item])).toContain(item.summary);
      expect(await render([item])).not.toContain("Работа с файлами");
    },
  );
  it.each([
    ["platform.run.delegate", "Делегирование другим сотрудникам"],
    ["platform.run.launch", "Запуск сотрудников и процессов"],
  ])("использует существующую подпись права %s", async (key, name) => {
    const item = operation(0);
    item.type = "CHANGE_CAPABILITY";
    item.parameters.capabilityKey = key;
    item.target.name = "Аналитик";
    expect(await render([item])).toContain(`Аналитик · ${name}`);
  });
  it("не выдумывает направление или имя получателя из технических параметров", async () => {
    const item = operation(0);
    item.type = "CHANGE_CAPABILITY";
    item.parameters.capabilityKey = "platform.artifact.manage";
    item.target.name = "";
    item.summary = "Операция требует проверки";
    expect(await render([item])).toContain(item.summary);
    expect(await render([item])).not.toContain("Работа с файлами");
    item.target.name = "Аналитик";
    delete item.parameters.enabled;
    expect(await render([item])).not.toContain("Работа с файлами");
  });
  it("показывает первые пять из 21, сохраняет остальные mounted под доступным закрытым details", async () => {
    const input = Array.from({ length: 21 }, (_, index) => operation(index));
    const html = await render(input);
    const first = html.slice(0, html.indexOf("<details"));
    expect(first.match(/data-operation-ref=/g)).toHaveLength(5);
    expect(html.match(/data-operation-ref=/g)).toHaveLength(21);
    expect(html).toContain("Ещё 16 операций");
    expect(html).not.toMatch(/<details[^>]*\sopen(?:\s|=|>)/);
    expect(html).toContain("Выбрано 21 из 21 операций");
    expect(html).toContain("github.read_20");
    expect(html).toContain("Менеджер · Kodex | Dev · GitHub");
    expect(html).not.toContain("agt_private");
    expect(html).not.toContain("int_private");
    expect(input.every((item) => item.selected)).toBe(true);
  });
  it("сохраняет сводный warning вне раскрытия для invalid/unavailable и полный selected count", async () => {
    const input = Array.from({ length: 7 }, (_, index) => operation(index));
    const invalid = input[6];
    const unavailable = input[5];
    if (!invalid || !unavailable)
      throw new Error("Synthetic operations are missing");
    invalid.validationProblems = ["SNAPSHOT_CONFLICT"];
    unavailable.permitted = false;
    unavailable.selected = false;
    const html = await render(input);
    expect(html.slice(0, html.indexOf("<details"))).toContain(
      "Требуют проверки: 2",
    );
    expect(html).toContain("Выбрано 6 из 7 операций");
    expect(html).toContain("Не выбрана");
    expect(html).not.toContain("SNAPSHOT_CONFLICT");
  });
  it.each([
    "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT",
    "CHANGE_PROJECT_ASSISTANT_INTEGRATION_GRANT",
  ] as const)(
    "%s показывает revoke/policy без выдуманного получателя",
    async (type) => {
      const item = operation(0);
      item.type = type;
      item.before = {};
      item.parameters.enabled = false;
      item.parameters.approvalPolicy = "HUMAN_EACH_EFFECT";
      const html = await render([item]);
      expect(html).toContain("Отозвать");
      expect(html).toContain("github.read_0");
      expect(html).not.toContain("Менеджер");
      expect(html).not.toContain("<details");
    },
  );
  it("обычный summary сохраняется без HTML-интерпретации", async () => {
    const item = operation(0);
    item.type = "UPDATE_PROJECT";
    item.summary = "<script>не выполнять</script>";
    expect(await render([item])).toContain("&lt;script&gt;");
  });
});
