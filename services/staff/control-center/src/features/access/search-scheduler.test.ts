import { afterEach, describe, expect, it, vi } from "vitest";

import { createSearchScheduler } from "./search-scheduler";

afterEach(() => vi.useRealTimers());

describe("отложенный поиск доступа", () => {
  it("отменяет предыдущий запрос и читает актуальный размер страницы при отправке", () => {
    vi.useFakeTimers();
    const emitted = vi.fn();
    let pageSize = 20;
    const scheduler = createSearchScheduler((query) => {
      emitted(query, pageSize);
    });
    scheduler.schedule(" первый ");
    vi.advanceTimersByTime(200);
    scheduler.schedule(" второй ");
    pageSize = 40;
    vi.advanceTimersByTime(249);
    expect(emitted).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(emitted).toHaveBeenCalledOnce();
    expect(emitted).toHaveBeenCalledWith("второй", 40);
  });

  it("отменяет ожидающий запрос при закрытии", () => {
    vi.useFakeTimers();
    const emitted = vi.fn();
    const scheduler = createSearchScheduler(emitted);
    scheduler.schedule("запрос");
    scheduler.cancel();
    vi.runAllTimers();
    expect(emitted).not.toHaveBeenCalled();
  });
});
