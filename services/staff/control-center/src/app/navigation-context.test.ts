import { describe, expect, it } from "vitest";

import {
  activeNavigationSection,
  routeProjectRef,
} from "@/app/navigation-context";

describe("shell navigation context", () => {
  it.each([
    ["agents", "agents"],
    ["agent", "agents"],
    ["new-run", "project-runs"],
    ["project-runs", "project-runs"],
    ["project-run", "project-runs"],
    ["runtime-environments", "runtime-environments"],
    ["runtime-environment-new", "runtime-environments"],
    ["runtime-environment", "runtime-environments"],
  ])(
    "выделяет один раздел для list/detail/create route %s",
    (route, section) => {
      expect(activeNavigationSection(route)).toBe(section);
    },
  );

  it("не подсвечивает обзор Проекта на вложенных route", () => {
    expect(activeNavigationSection("agent")).not.toBe("project");
    expect(activeNavigationSection("workflow")).not.toBe("project");
  });

  it("сохраняет строковый projectRef на list/detail/create route", () => {
    for (const routeName of [
      "agents",
      "agent",
      "new-run",
      "runtime-environment-new",
    ]) {
      expect(routeProjectRef({ projectRef: "project_sales", routeName })).toBe(
        "project_sales",
      );
    }
  });

  it("закрыто отклоняет отсутствующий или неоднозначный projectRef", () => {
    expect(routeProjectRef({})).toBeUndefined();
    expect(
      routeProjectRef({ projectRef: ["first", "second"] }),
    ).toBeUndefined();
  });
});
