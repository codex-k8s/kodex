import { afterEach, expect, it, vi } from "vitest";
import { downloadBlob } from "./download-blob";

afterEach(() => vi.unstubAllGlobals());

it("передаёт Blob и имя, нажимает скрытую ссылку и освобождает её после удаления", () => {
  const events: string[] = [];
  const body = new Blob(["synthetic"]);
  const anchor = {
    href: "",
    download: "",
    hidden: false,
    click() {
      expect(this.href).toBe("blob:fixture");
      expect(this.download).toBe("файл.txt");
      expect(this.hidden).toBe(true);
      events.push("click");
    },
    remove() {
      events.push("remove");
    },
  };
  vi.stubGlobal("URL", {
    createObjectURL(value: Blob) {
      expect(value).toBe(body);
      events.push("create");
      return "blob:fixture";
    },
    revokeObjectURL(value: string) {
      expect(value).toBe("blob:fixture");
      events.push("revoke");
    },
  });
  vi.stubGlobal("document", {
    createElement(name: string) {
      expect(name).toBe("a");
      events.push("element");
      return anchor;
    },
    body: {
      append(value: unknown) {
        expect(value).toBe(anchor);
        events.push("append");
      },
    },
  });
  downloadBlob(body, "файл.txt");
  expect(events).toEqual([
    "create",
    "element",
    "append",
    "click",
    "remove",
    "revoke",
  ]);
});

it("передаёт ошибку click вызывающему коду в исходном порядке", () => {
  const error = new Error("synthetic click failure");
  const revoke = vi.fn();
  const remove = vi.fn();
  vi.stubGlobal("URL", {
    createObjectURL: () => "blob:fixture",
    revokeObjectURL: revoke,
  });
  vi.stubGlobal("document", {
    createElement: () => ({
      click: () => {
        throw error;
      },
      remove,
    }),
    body: { append: vi.fn() },
  });
  expect(() => downloadBlob(new Blob(), "fixture")).toThrow(error);
  expect(remove).not.toHaveBeenCalled();
  expect(revoke).not.toHaveBeenCalled();
});
