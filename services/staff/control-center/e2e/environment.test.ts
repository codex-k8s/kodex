import { describe, expect, test } from "vitest";

import { discoveryChromiumLaunchOptions } from "./environment";

describe("настройка Chromium для удалённого discovery E2E", () => {
  test("по умолчанию сохраняет публичное разрешение имени", () => {
    expect(
      discoveryChromiumLaunchOptions("https://control.kodex.works", ""),
    ).toBeUndefined();
  });

  test("привязывает только базовый host к loopback", () => {
    expect(
      discoveryChromiumLaunchOptions("https://control.kodex.works", "loopback"),
    ).toEqual({
      args: ["--host-resolver-rules=MAP control.kodex.works 127.0.0.1"],
    });
  });

  test("закрыто отклоняет неизвестный режим", () => {
    expect(() =>
      discoveryChromiumLaunchOptions("https://control.kodex.works", "external"),
    ).toThrow("must be empty or loopback");
  });
});
