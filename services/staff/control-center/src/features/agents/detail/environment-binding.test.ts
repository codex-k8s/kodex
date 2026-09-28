import { describe, expect, it } from "vitest";

import { needsEnvironmentBinding } from "./environment-binding";

describe("needsEnvironmentBinding", () => {
  it("предлагает обновить закреплённую ревизию того же окружения", () => {
    expect(
      needsEnvironmentBinding("renv_1", "rev_1", "renv_1", "rev_2", true),
    ).toBe(true);
  });

  it("не предлагает повторно назначить уже закреплённую ревизию", () => {
    expect(
      needsEnvironmentBinding("renv_1", "rev_2", "renv_1", "rev_2", true),
    ).toBe(false);
  });

  it("не позволяет назначить неготовое окружение", () => {
    expect(
      needsEnvironmentBinding("renv_1", "rev_1", "renv_2", "rev_1", false),
    ).toBe(false);
  });
});
