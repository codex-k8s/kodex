import { describe, expect, it } from "vitest";

import {
  accessResourceKinds,
  isExactAgentScope,
  membershipForSubject,
  roleInput,
  subjectBindings,
  toAccessScope,
  toBindingInput,
  uniquePermissionKeys,
  validScope,
} from "@/features/access/model";
import type {
  AccessBinding,
  AccessSubject,
  Membership,
} from "@/shared/api/generated/openapi/types.gen";

const subject: AccessSubject = {
  ref: "subject_alice",
  kind: "USER",
  displayName: "Алиса",
  active: true,
  oidcGroupRefs: ["group_sales"],
};

function binding(
  ref: string,
  subjectRef: string,
  permissionKeys: string[],
  state: AccessBinding["state"] = "ACTIVE",
): AccessBinding {
  return {
    ref,
    version: 1,
    state,
    subject: {
      ref: subjectRef,
      kind: subjectRef.startsWith("group_") ? "OIDC_GROUP" : "USER",
      displayName: subjectRef,
      active: true,
      oidcGroupRefs: [],
    },
    roleVersion: {
      ref: `role_version_${ref}`,
      roleRef: `role_${ref}`,
      revision: 1,
      name: ref,
      description: "",
      permissionKeys,
      allowedScopes: ["PROJECT"],
      changeComment: "",
      createdAt: "2026-08-29T00:00:00Z",
      createdBy: { ref: "user_owner", displayName: "Владелец" },
    },
    scope: { kind: "PROJECT", projectRef: "project_sales" },
    conditions: { requireOwner: false },
    createdAt: "2026-08-29T00:00:00Z",
    updatedAt: "2026-08-29T00:00:00Z",
  };
}

describe("enterprise RBAC UI model", () => {
  it("предлагает instance-level окружения и секреты из канонического каталога", () => {
    expect(accessResourceKinds).toContain("RUNTIME_ENVIRONMENT");
    expect(accessResourceKinds).toContain("SECRET");
  });

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

  it("разделяет прямые и унаследованные от OIDC-группы assignments", () => {
    const assignments = subjectBindings(subject, [
      binding("direct", subject.ref, ["agent.view"]),
      binding("group", "group_sales", ["agent.launch"]),
      binding("other", "group_other", ["agent.manage"]),
      binding("revoked", subject.ref, ["workflow.view"], "REVOKED"),
    ]);

    expect(assignments.map((item) => item.ref)).toEqual(["direct", "group"]);
    expect(uniquePermissionKeys(assignments)).toEqual([
      "agent.launch",
      "agent.view",
    ]);
  });

  it("берёт platform role только из реального membership presentation", () => {
    const memberships: Membership[] = [
      {
        ref: "membership_alice",
        version: 2,
        user: {
          ref: subject.ref,
          displayName: subject.displayName,
        },
        platformRole: "OPERATOR",
        permissions: [],
        active: true,
        nextActions: [],
      },
    ];

    expect(membershipForSubject(subject, memberships)?.platformRole).toBe(
      "OPERATOR",
    );
  });
});
