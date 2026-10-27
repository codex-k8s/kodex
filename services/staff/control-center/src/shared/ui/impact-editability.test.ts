import { ref } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";

import { createImpactEditability } from "./impact-editability";

afterEach(() => vi.useRealTimers());

describe("доступность редактирования impact-плана", () => {
  it("учитывает состояние, ошибки и точную границу срока, затем останавливает часы", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2030-01-01T00:00:00Z"));
    const busy = ref(false);
    const loading = ref(false);
    const problem = ref<unknown>();
    const page = ref<{ plan: { state: string; expiresAt: string } }>();
    const state = createImpactEditability({
      busy: () => busy.value,
      loading: () => loading.value,
      problem: () => problem.value,
      page: () => page.value,
    });
    try {
      expect(state.editable.value).toBe(false);
      page.value = {
        plan: { state: "PREPARED", expiresAt: "2030-01-01T00:00:02Z" },
      };
      expect(state.editable.value).toBe(true);
      busy.value = true;
      expect(state.editable.value).toBe(false);
      busy.value = false;
      loading.value = true;
      expect(state.editable.value).toBe(false);
      loading.value = false;
      problem.value = new Error("synthetic failure");
      expect(state.editable.value).toBe(false);
      problem.value = undefined;
      page.value.plan.state = "APPLIED";
      expect(state.editable.value).toBe(false);
      page.value.plan.state = "PREPARED";
      vi.advanceTimersByTime(1000);
      expect(state.editable.value).toBe(true);
      vi.advanceTimersByTime(1000);
      expect(state.editable.value).toBe(false);
    } finally {
      state.stop();
    }
    expect(vi.getTimerCount()).toBe(0);
  });
});
