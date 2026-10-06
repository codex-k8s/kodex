import { describe, expect, it } from "vitest";

import {
  persistAssistantConversationRef,
  persistAssistantScope,
  persistAssistantWorkspaceOpen,
  restoreAssistantConversationRef,
  restoreAssistantScope,
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
  it("сохраняет режим отдельно для каждого проекта и не выводит его из ref диалога", () => {
    const values = new Map<string, string>();
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      removeItem: (key: string) => values.delete(key),
      setItem: (key: string, value: string) => values.set(key, value),
    };
    persistAssistantScope("prj_sales", "PROJECT", storage);
    persistAssistantScope("prj_support", "SYSTEM", storage);
    persistAssistantConversationRef(
      "prj_other",
      "cnv_project",
      storage,
      "PROJECT",
    );
    expect(restoreAssistantScope("prj_sales", storage)).toBe("PROJECT");
    expect(restoreAssistantScope("prj_support", storage)).toBe("SYSTEM");
    expect(restoreAssistantScope("prj_other", storage)).toBe("SYSTEM");
    expect(restoreAssistantScope(undefined, storage)).toBe("SYSTEM");
    const count = values.size;
    persistAssistantScope(undefined, "PROJECT", storage);
    expect(values.size).toBe(count);
    persistAssistantScope("prj_sales", "SYSTEM", storage);
    expect(restoreAssistantScope("prj_sales", storage)).toBe("SYSTEM");
  });

  it.each([null, "", "project", "SYSTEM_OTHER", " PROJECT", "cnv_saved"])(
    "отклоняет неизвестное сохранённое предпочтение %s",
    (value) => {
      const storage = {
        getItem: () => value,
        setItem: () => undefined,
        removeItem: () => undefined,
      };
      expect(restoreAssistantScope("prj_sales", storage)).toBe("SYSTEM");
    },
  );

  it("недоступное хранилище не блокирует работу и не восстанавливает PROJECT", () => {
    const storage = {
      getItem: () => {
        throw new Error("storage unavailable");
      },
      setItem: () => {
        throw new Error("storage unavailable");
      },
      removeItem: () => undefined,
    };
    expect(restoreAssistantScope("prj_sales", storage)).toBe("SYSTEM");
    expect(() =>
      persistAssistantScope("prj_sales", "PROJECT", storage),
    ).not.toThrow();
  });

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
