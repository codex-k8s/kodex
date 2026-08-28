import { renderToString } from "@vue/server-renderer";
import { createSSRApp, h } from "vue";
import { describe, expect, it } from "vitest";

import NewRunSessionPolicy from "@/features/new-run/components/NewRunSessionPolicy.vue";

const labels = {
  legend: "Продолжение работы",
  newTitle: "Новая сессия",
  newDescription: "Начать с чистой историей",
  continueTitle: "Продолжить существующую",
  continueDescription: "Сохранить контекст",
};

describe("NewRunSessionPolicy", () => {
  it("рендерит нативную radio-группу с единственным checked состоянием", async () => {
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(NewRunSessionPolicy, {
            labels,
            modelValue: "CONTINUE",
          }),
      }),
    );

    expect(html).toContain("<fieldset");
    expect(html.match(/type="radio"/g)).toHaveLength(2);
    expect(html.match(/checked/g)).toHaveLength(1);
    expect(html).toContain('value="CONTINUE" checked');
    expect(html).toContain('name="new-run-session-policy"');
  });

  it("передаёт disabled только варианту продолжения", async () => {
    const html = await renderToString(
      createSSRApp({
        render: () =>
          h(NewRunSessionPolicy, {
            continueDisabled: true,
            labels,
            modelValue: "NEW",
          }),
      }),
    );

    expect(html).toContain('value="NEW" checked');
    expect(html).toContain('value="CONTINUE" disabled');
  });
});
