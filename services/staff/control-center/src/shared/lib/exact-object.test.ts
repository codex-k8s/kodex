import { describe, expect, it } from "vitest";
import { hasExactKeys, isRecord } from "./exact-object";

describe("проверка точной формы объекта", () => {
  it("сохраняет допустимую форму record и отклоняет массивы и примитивы", () => {
    for (const value of [{}, Object.create(null), new Date(0)])
      expect(isRecord(value)).toBe(true);
    for (const value of [null, undefined, [], "object", 1, true, () => {}])
      expect(isRecord(value)).toBe(false);
  });

  it("учитывает только собственные перечисляемые ключи и не меняет ожидание", () => {
    const value = { b: 2, a: 1 };
    Object.setPrototypeOf(value, { inherited: 1 });
    Object.defineProperty(value, "hidden", { value: 3, enumerable: false });
    const expected = Object.freeze(["b", "a"]);
    expect(hasExactKeys(value, expected)).toBe(true);
    expect(expected).toEqual(["b", "a"]);
    expect(hasExactKeys(value, ["a"])).toBe(false);
    expect(hasExactKeys(value, ["a", "a"])).toBe(false);
    expect(hasExactKeys(value, ["a", "b", "hidden"])).toBe(false);
    expect(hasExactKeys({}, [])).toBe(true);
  });
});
