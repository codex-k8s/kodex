import { describe, expect, it } from "vitest";

import {
  isExactAgentScope,
  roleInput,
  toAccessScope,
  toBindingInput,
  validScope,
} from "@/features/access/model";

describe("enterprise RBAC UI model", () => {
  it("создаёт обязательную точную область одного ИИ-сотрудника", () => {
    const scope = toAccessScope({
      kind: "RESOURCE_INSTANCE",
      projectRef: "project_sales",
      resourceKind: "AGENT",
      resourceRef: "agent_analyst",
    });

    expect(scope).toEqual({
      kind: "RESOURCE_INSTANCE",
      projectRef: "project_sales",
      resourceKind: "AGENT",
      resourceRef: "agent_analyst",
    });
    expect(isExactAgentScope(scope)).toBe(true);
  });

  it("не принимает instance scope без точного ресурса", () => {
    expect(
      validScope({
        kind: "RESOURCE_INSTANCE",
        projectRef: "project_sales",
        resourceKind: "AGENT",
        resourceRef: "",
      }),
    ).toBe(false);
  });

  it("строит version-pinned binding без старой checkbox-модели membership", () => {
    expect(
      toBindingInput({
        subjectKind: "OIDC_GROUP",
        subjectRef: "subject_sales",
        roleVersionRef: "role_version_launchers_v2",
        scope: {
          kind: "RESOURCE_INSTANCE",
          projectRef: "project_sales",
          resourceKind: "AGENT",
          resourceRef: "agent_analyst",
        },
        validFrom: "",
        validUntil: "",
        requireOwner: false,
      }),
    ).toEqual({
      subjectKind: "OIDC_GROUP",
      subjectRef: "subject_sales",
      roleVersionRef: "role_version_launchers_v2",
      scope: {
        kind: "RESOURCE_INSTANCE",
        projectRef: "project_sales",
        resourceKind: "AGENT",
        resourceRef: "agent_analyst",
      },
      conditions: { requireOwner: false },
    });
  });

  it("нормализует новую immutable-версию роли", () => {
    expect(
      roleInput(
        "  Оператор продаж  ",
        "  Запускает выбранных сотрудников  ",
        ["agent.launch", "agent.view", "agent.launch"],
        ["RESOURCE_INSTANCE", "PROJECT", "RESOURCE_INSTANCE"],
        "  Уточнена область  ",
      ),
    ).toEqual({
      name: "Оператор продаж",
      description: "Запускает выбранных сотрудников",
      permissionKeys: ["agent.launch", "agent.view"],
      allowedScopes: ["PROJECT", "RESOURCE_INSTANCE"],
      changeComment: "Уточнена область",
    });
  });
});
