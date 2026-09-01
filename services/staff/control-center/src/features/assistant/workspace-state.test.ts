import { describe, expect, it } from "vitest";

import {
  persistAssistantWorkspaceOpen,
  restoreAssistantWorkspaceOpen,
} from "./workspace-state";

function memoryStorage(
  initial?: string,
): Pick<Storage, "getItem" | "removeItem" | "setItem"> & { value?: string } {
  return {
    value: initial,
    getItem() {
      return this.value ?? null;
    },
    removeItem() {
      this.value = undefined;
    },
    setItem(_key, value) {
      this.value = value;
    },
  };
}

describe("assistant workspace state", () => {
  it("восстанавливает только явно открытый workspace", () => {
    expect(restoreAssistantWorkspaceOpen(memoryStorage("1"))).toBe(true);
    expect(restoreAssistantWorkspaceOpen(memoryStorage("0"))).toBe(false);
    expect(restoreAssistantWorkspaceOpen(memoryStorage())).toBe(false);
  });

  it("сохраняет открытие и удаляет состояние при закрытии", () => {
    const storage = memoryStorage();

    persistAssistantWorkspaceOpen(true, storage);
    expect(storage.value).toBe("1");

    persistAssistantWorkspaceOpen(false, storage);
    expect(storage.value).toBeUndefined();
  });

  it("закрыто обрабатывает недоступное session storage", () => {
    const storage = {
      getItem: () => {
        throw new Error("storage unavailable");
      },
      removeItem: () => {
        throw new Error("storage unavailable");
      },
      setItem: () => {
        throw new Error("storage unavailable");
      },
    };

    expect(restoreAssistantWorkspaceOpen(storage)).toBe(false);
    expect(() => persistAssistantWorkspaceOpen(true, storage)).not.toThrow();
  });
});
