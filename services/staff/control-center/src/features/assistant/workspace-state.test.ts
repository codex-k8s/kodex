import { describe, expect, it } from "vitest";

import {
  persistAssistantConversationRef,
  persistAssistantWorkspaceOpen,
  restoreAssistantConversationRef,
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
  it("хранит последний выбранный диалог отдельно для общего и проектного контекста", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => {
        values.set(key, value);
      },
    };

    persistAssistantConversationRef(undefined, "cnv_all", storage);
    persistAssistantConversationRef("prj_sales", "cnv_sales", storage);
    persistAssistantConversationRef(
      "prj_sales",
      "cnv_project_helper",
      storage,
      "PROJECT",
    );
    expect(
      restoreAssistantConversationRef("prj_sales", storage, "PROJECT"),
    ).toBe("cnv_project_helper");
    expect(restoreAssistantConversationRef(undefined, storage)).toBe("cnv_all");
    expect(restoreAssistantConversationRef("prj_sales", storage)).toBe(
      "cnv_sales",
    );
    expect(
      restoreAssistantConversationRef("prj_other", storage),
    ).toBeUndefined();
  });

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
    expect(restoreAssistantConversationRef(undefined, storage)).toBeUndefined();
    expect(() =>
      persistAssistantConversationRef(undefined, "cnv_selected", storage),
    ).not.toThrow();
  });
});
