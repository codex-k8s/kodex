import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, describe, expect, it, vi } from "vitest";

import App from "./App.vue";
import { client } from "./generated/client.gen";
import type {
  AutomationHistoryItem,
  AutomationHistoryResponse,
} from "./generated/types.gen";

const automationHistoryRefreshIntervalMs = 5_000;

const pendingItem: AutomationHistoryItem = {
  schedule_run_id: "scheduled-run-11111111111111111111111111111111",
  status: "waiting_owner",
  outcome: "requires_human",
  owner_attention_id: 71,
  human_decision_status: "open",
  delivery_status: "delivered",
  next_action: "wait_for_owner_response",
  updated_at: "2026-07-22T12:00:00Z",
};

const pending: AutomationHistoryResponse = {
  items: [pendingItem],
};

const resolved: AutomationHistoryResponse = {
  items: [
    {
      ...pendingItem,
      status: "succeeded",
      human_decision_status: "resolved",
      next_action: "none",
      updated_at: "2026-07-22T12:01:00Z",
    },
  ],
};

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Control Center automation history data path", () => {
  it("загружает generated OpenAPI client и обновляет pending после решения владельца", async () => {
    vi.useFakeTimers();
    client.setConfig({ baseUrl: "http://control-center.test/api/control-center/v1" });
    const fetchMock = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse(pending))
      .mockResolvedValueOnce(jsonResponse(resolved));
    vi.stubGlobal("fetch", fetchMock);

    const wrapper = mount(App);
    await wrapper.get("#history-read-token").setValue("synthetic-read-token");
    await wrapper.get("form").trigger("submit");
    await flushPromises();

    expect(wrapper.text()).toContain("Ожидается решение владельца");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const firstRequest = fetchMock.mock.calls[0]![0] as Request;
    expect(firstRequest.url).toContain("/api/control-center/v1/automation-runs?limit=100");
    expect(firstRequest.headers.get("Authorization")).toBe("Bearer synthetic-read-token");

    await vi.advanceTimersByTimeAsync(automationHistoryRefreshIntervalMs);
    await flushPromises();

    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(wrapper.findAll(".run")).toHaveLength(1);
    expect(wrapper.text()).toContain("Решение принято");
    expect(wrapper.text()).toContain("Действий не требуется");
    wrapper.unmount();
  });
});

function jsonResponse(body: AutomationHistoryResponse): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
