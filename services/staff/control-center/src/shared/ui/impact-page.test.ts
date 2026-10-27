import { describe, expect, it } from "vitest";
import { appendImpactPage } from "./impact-page";

function fixture(refs: string[], nextPageToken?: string) {
  return {
    plan: { state: "PREPARED", revision: 3 },
    items: refs.map((ref) => ({ ref, outcome: "PENDING", version: 7 })),
    total: 3,
    nextPageToken,
    metadata: "source",
  };
}

describe("присоединение проверенной страницы impact-плана", () => {
  it("сохраняет порядок и metadata нового ответа без изменения страниц", () => {
    const previous = fixture(["first"], "cursor-one");
    const next = { ...fixture(["second"], "cursor-two"), metadata: "latest" };
    const cursors = new Set<string>();
    const result = appendImpactPage(previous, next, cursors);

    expect(result).toEqual({
      ...next,
      items: [...previous.items, ...next.items],
    });
    expect(result).not.toBe(previous);
    expect(result).not.toBe(next);
    expect(result.items).not.toBe(previous.items);
    expect(result.items).not.toBe(next.items);
    expect(previous.items.map((item) => item.ref)).toEqual(["first"]);
    expect(next.items.map((item) => item.ref)).toEqual(["second"]);
    expect(cursors).toEqual(new Set(["cursor-one"]));
  });

  it.each([undefined, ""])("сохраняет terminal cursor %s", (cursor) => {
    const cursors = new Set<string>();
    const result = appendImpactPage(
      fixture([]),
      fixture(["last"], cursor),
      cursors,
    );

    expect(result.nextPageToken).toBe(cursor);
    expect(result.items.map((item) => item.ref)).toEqual(["last"]);
    expect(cursors.size).toBe(0);
  });

  it.each([
    ["повтор текущего cursor", { nextPageToken: "cursor-one" }],
    ["повтор более раннего cursor", { nextPageToken: "older" }],
    ["изменение total", { total: 4 }],
    ["изменение состояния", { plan: { state: "EXPIRED", revision: 3 } }],
    [
      "переполнение total",
      { items: fixture(["second", "third", "fourth"]).items },
    ],
    ["пересечение ref", { items: fixture(["first"]).items }],
  ])("отклоняет %s после фиксации использованного cursor", (_, patch) => {
    const previous = fixture(["first"], "cursor-one");
    const next = { ...fixture(["second"], "cursor-two"), ...patch };
    const cursors = new Set(["older"]);

    expect(() => appendImpactPage(previous, next, cursors)).toThrow(
      "Publication impact pagination changed",
    );
    expect(cursors).toEqual(new Set(["older", "cursor-one"]));
    expect(previous.items.map((item) => item.ref)).toEqual(["first"]);
  });
});
