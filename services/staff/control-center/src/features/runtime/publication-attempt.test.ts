import { expect, it } from "vitest";
import {
  publicationRefusalClearsIntent,
  clearPublicationAttempts,
  readPublicationAttempt,
  rememberPublicationAttempt,
  forgetPublicationAttempt,
  type PublicationAttempt,
} from "./publication-attempt";
it.each([400, 401, 403, 404, 409, 412, 422, 500, 503, 504])(
  "сохраняет прежний неизвестный исход при отказе повторного запроса %i",
  (status) => {
    expect(publicationRefusalClearsIntent(true, status)).toBe(false);
    expect(publicationRefusalClearsIntent(false, status)).toBe(
      status === 400 || status === 422,
    );
  },
);
function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
}
const attempt: PublicationAttempt = {
  kind: "RUNTIME_ENVIRONMENT",
  ownerRef: "draft",
  planRef: "plan",
  version: 7,
  selectedItemRefs: ["item-two", "item-one"],
  key: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
};
it("при смене владельца очищает intent всех закрытых форм, сохраняя остальные настройки", () => {
  const storage = memoryStorage();
  rememberPublicationAttempt(attempt, storage);
  rememberPublicationAttempt(
    { ...attempt, kind: "ROLE_IMAGE", ownerRef: "image" },
    storage,
  );
  storage.setItem("locale", "ru");
  clearPublicationAttempts(storage);
  expect(storage.length).toBe(1);
  expect(storage.getItem("locale")).toBe("ru");
});
it("сохраняет original If-Match/key/selection после закрытия формы без content", () => {
  const storage = memoryStorage();
  rememberPublicationAttempt(attempt, storage);
  expect(
    readPublicationAttempt(attempt.kind, attempt.ownerRef, storage),
  ).toEqual(attempt);
  expect(
    readPublicationAttempt("PROMPT_TEMPLATE", attempt.ownerRef, storage),
  ).toBeUndefined();
  expect(storage.getItem(storage.key(0) ?? "")).not.toContain("content");
});
it("не заменяет неизвестный outcome новым key/version/selection", () => {
  const storage = memoryStorage();
  rememberPublicationAttempt(attempt, storage);
  for (const replacement of [
    { ...attempt, version: 8 },
    { ...attempt, selectedItemRefs: [] },
    { ...attempt, key: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" },
  ])
    expect(() => rememberPublicationAttempt(replacement, storage)).toThrow();
  expect(() =>
    rememberPublicationAttempt({ ...attempt }, storage),
  ).not.toThrow();
});
it("удаляет завершённую попытку и отклоняет повреждённую local projection", () => {
  const storage = memoryStorage();
  rememberPublicationAttempt(attempt, storage);
  const key = storage.key(0) ?? "";
  storage.setItem(key, JSON.stringify({ ...attempt, version: 0 }));
  expect(() =>
    readPublicationAttempt(attempt.kind, attempt.ownerRef, storage),
  ).toThrow();
  forgetPublicationAttempt(attempt.kind, attempt.ownerRef, storage);
  expect(storage.length).toBe(0);
});
