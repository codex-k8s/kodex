import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

import {
  confirmationState,
  requestConfirmation,
  resolveConfirmation,
} from "./confirmation";

describe("единый диалог подтверждения", () => {
  it("возвращает подтверждённое решение и закрывает состояние", async () => {
    const result = requestConfirmation({
      message: "Удалить объект?",
      confirmLabel: "Удалить",
      tone: "danger",
    });

    expect(confirmationState.open).toBe(true);
    expect(confirmationState.message).toBe("Удалить объект?");
    expect(confirmationState.tone).toBe("danger");

    resolveConfirmation(true);

    await expect(result).resolves.toBe(true);
    expect(confirmationState.open).toBe(false);
  });

  it("отменяет предыдущее ожидание при новом запросе", async () => {
    const previous = requestConfirmation("Первый запрос");
    const current = requestConfirmation("Второй запрос");

    await expect(previous).resolves.toBe(false);
    expect(confirmationState.message).toBe("Второй запрос");

    resolveConfirmation(false);
    await expect(current).resolves.toBe(false);
  });

  it("использует ModalDialog с закрытием по клику вне окна", () => {
    const host = readFileSync(
      new URL("./ConfirmDialogHost.vue", import.meta.url),
      "utf8",
    );
    const modal = readFileSync(
      new URL("./ModalDialog.vue", import.meta.url),
      "utf8",
    );

    expect(host).toContain('@close="resolveConfirmation(false)"');
    expect(modal).toContain("@mousedown.self=\"!busy && emit('close')\"");
  });
});
