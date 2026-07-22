import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import AutomationHistory from "./AutomationHistory.vue";
import {
  parseAutomationCallbackReceipts,
  type AutomationCallbackReceipt,
} from "./contract";

const pending: AutomationCallbackReceipt = {
  schedule_run_id: "scheduled-run-11111111111111111111111111111111",
  status: "waiting_owner",
  outcome: "requires_human",
  duplicate: false,
  owner_attention_id: 71,
  human_decision_status: "open",
  delivery_status: "delivered",
  next_action: "wait_for_owner_response",
};

const resolved: AutomationCallbackReceipt = {
  ...pending,
  status: "succeeded",
  duplicate: true,
  human_decision_status: "resolved",
  next_action: "none",
};

describe("AutomationHistory", () => {
  it("показывает pending human decision и безопасное следующее действие", () => {
    const wrapper = mount(AutomationHistory, { props: { items: [pending] } });

    expect(wrapper.text()).toContain("Ожидается решение владельца");
    expect(wrapper.text()).toContain(pending.schedule_run_id);
    expect(wrapper.text()).toContain("#71 · open");
    expect(wrapper.text()).toContain("Ответить в точном треде запуска");
  });

  it("показывает resolved без создания второй записи", async () => {
    const wrapper = mount(AutomationHistory, { props: { items: [pending] } });
    await wrapper.setProps({ items: [resolved] });

    expect(wrapper.findAll(".run")).toHaveLength(1);
    expect(wrapper.text()).toContain("Решение принято");
    expect(wrapper.text()).toContain("точный replay");
    expect(wrapper.text()).toContain("Действий не требуется");
  });

  it("отбрасывает payload, который не соответствует MCP transport", () => {
    expect(
      parseAutomationCallbackReceipts([{ ...pending, status: "success" }]),
    ).toEqual([]);
    expect(
      parseAutomationCallbackReceipts([
        { ...pending, schedule_run_id: "run-1" },
      ]),
    ).toEqual([]);
    expect(parseAutomationCallbackReceipts([pending])).toEqual([pending]);
  });
});
