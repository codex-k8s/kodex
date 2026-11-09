import { describe, expect, it } from "vitest";
import { runtimeQueueMessage } from "./runtime-queue";

describe("серверная ресурсная очередь", () => {
  it("показывает ожидание и слишком большой запрос по отдельной причине владельца", () => {
    expect(
      runtimeQueueMessage({
        state: "QUEUED",
        queueReason: "WAITING_RESOURCES",
      }),
    ).toBe("i18n:RUNTIME_WAITING_CAPACITY");
    expect(
      runtimeQueueMessage({
        state: "QUEUED",
        queueReason: "REQUEST_EXCEEDS_CAPACITY",
      }),
    ).toBe("i18n:RUNTIME_REQUEST_EXCEEDS_CAPACITY");
  });
  it("не выдаёт обычный запуск или завершение за ожидание", () => {
    expect(runtimeQueueMessage({ state: "RUNNING" })).toBeUndefined();
    expect(
      runtimeQueueMessage({
        state: "RUNNING",
        queueReason: "WAITING_RESOURCES",
      }),
    ).toBeUndefined();
    expect(
      runtimeQueueMessage({
        state: "CANCELLED",
        queueReason: "WAITING_RESOURCES",
      }),
    ).toBeUndefined();
    expect(runtimeQueueMessage()).toBeUndefined();
  });
});
