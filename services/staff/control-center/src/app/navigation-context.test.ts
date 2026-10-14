import { computed, defineComponent, h } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { describe, expect, it } from "vitest";

import {
  activeNavigationSection,
  configurationRouteProjectRef,
  routeProjectRef,
} from "@/app/navigation-context";

describe("shell navigation context", () => {
  it.each(["ROLE_IMAGE", "PROMPT_TEMPLATE"])(
    "%s detail сохраняет точный query projectRef, а не scope другого экрана",
    (kind) => {
      expect(
        configurationRouteProjectRef(
          "configuration",
          { kind, configurationRef: "mcfg_fixture" },
          { projectRef: "prj_fixture" },
        ),
      ).toBe("prj_fixture");
    },
  );
  it.each(["SYSTEM_STT", "INTEGRATION_DEFINITION", "UNKNOWN", ["ROLE_IMAGE"]])(
    "не превращает organization/unknown kind %s в PROJECT через query",
    (kind) => {
      expect(
        configurationRouteProjectRef(
          "configuration",
          { kind },
          { projectRef: "prj_fixture" },
        ),
      ).toBeUndefined();
    },
  );
  it.each([undefined, "", null, ["prj_first", "prj_second"]])(
    "не принимает отсутствующий/неоднозначный query projectRef %s",
    (projectRef) => {
      expect(
        configurationRouteProjectRef(
          "configuration",
          { kind: "ROLE_IMAGE" },
          { projectRef },
        ),
      ).toBeUndefined();
    },
  );
  it("не расширяет query scope на другие route", () => {
    expect(
      configurationRouteProjectRef(
        "home",
        { kind: "ROLE_IMAGE" },
        { projectRef: "prj_fixture" },
      ),
    ).toBeUndefined();
  });
  it("recipe→history→другой PROJECT→ORG реагирует на маршрут без сохранения старого scope", async () => {
    const component = defineComponent({ render: () => h("div") });
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        {
          path: "/projects/:projectRef/role-images/:recipeRef",
          name: "role-image",
          component,
        },
        {
          path: "/configurations/:kind/:configurationRef",
          name: "configuration",
          component,
        },
      ],
    });
    const currentProject = computed(() => {
      const route = router.currentRoute.value;
      return (
        routeProjectRef(route.params) ??
        configurationRouteProjectRef(route.name, route.params, route.query)
      );
    });
    await router.push("/projects/prj_first/role-images/imgrec_fixture");
    expect(currentProject.value).toBe("prj_first");
    await router.push(
      "/configurations/ROLE_IMAGE/mcfg_fixture?projectRef=prj_first",
    );
    expect(currentProject.value).toBe("prj_first");
    await router.push(
      "/configurations/ROLE_IMAGE/mcfg_other?projectRef=prj_second",
    );
    expect(currentProject.value).toBe("prj_second");
    await router.push(
      "/configurations/INTEGRATION_DEFINITION/mcfg_org?projectRef=prj_first",
    );
    expect(currentProject.value).toBeUndefined();
    await router.push("/configurations/ROLE_IMAGE/mcfg_fixture");
    expect(currentProject.value).toBeUndefined();
  });
  it.each([
    ["agents", "agents"],
    ["organization-members", "project-access"],
    ["project-access", "project-access"],
    ["agent", "agents"],
    ["new-run", "project-runs"],
    ["project-runs", "project-runs"],
    ["project-run", "project-runs"],
    ["runtime-environments", "runtime-environments"],
    ["runtime-environment-new", "runtime-environments"],
    ["runtime-environment", "runtime-environments"],
    ["runtime-secrets", "runtime-secrets"],
    ["files-trash", "files"],
    ["provider-accounts", "administration"],
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
