import { afterEach, describe, expect, it } from "vitest";

import { selectedProjectRef, selectProjectRef } from "@/shared/project-context";

describe("project context", () => {
  afterEach(() => selectProjectRef(undefined));

  it("хранит только выбранный opaque project reference", () => {
    selectProjectRef("project_sales01");
    expect(selectedProjectRef()).toBe("project_sales01");
    selectProjectRef(undefined);
    expect(selectedProjectRef()).toBeUndefined();
  });
});
