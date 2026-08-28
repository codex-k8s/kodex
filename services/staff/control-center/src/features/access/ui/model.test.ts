import { describe, expect, it } from "vitest";

import {
  buildSystemRoles,
  filterMemberships,
  membershipAllows,
  orderedProjectPermissions,
} from "@/features/access/ui/model";
import type { Membership } from "@/shared/api/generated/openapi/types.gen";

function membership(
  ref: string,
  overrides: Partial<Membership> = {},
): Membership {
  return {
    ref,
    version: 1,
    user: {
      ref: `user-${ref}`,
      displayName: `Участник ${ref}`,
      emailHint: `${ref}@example.test`,
    },
    platformRole: "MEMBER",
    permissions: [],
    active: true,
    nextActions: [],
    ...overrides,
  };
}

describe("access presentation model", () => {
  it("показывает полный закрытый набор системных ролей", () => {
    const roles = buildSystemRoles([
      membership("owner", { platformRole: "OWNER" }),
      membership("member-active"),
      membership("member-disabled", { active: false }),
    ]);

    expect(roles.map((item) => item.role)).toEqual([
      "OWNER",
      "ADMINISTRATOR",
      "OPERATOR",
      "MEMBER",
      "AUDITOR",
    ]);
    expect(roles.find((item) => item.role === "MEMBER")).toMatchObject({
      memberCount: 2,
      activeMemberCount: 1,
    });
  });

  it("ищет по имени, email и прямому permission", () => {
    const source = [
      membership("anna", {
        user: {
          ref: "user-anna",
          displayName: "Анна Волкова",
          emailHint: "owner@example.test",
        },
        permissions: ["VIEW_AUDIT"],
      }),
      membership("mikhail"),
    ];

    expect(filterMemberships(source, "волк")).toHaveLength(1);
    expect(filterMemberships(source, "owner@")).toHaveLength(1);
    expect(filterMemberships(source, "view_audit")).toHaveLength(1);
  });

  it("сортирует только назначенные проектные полномочия", () => {
    const source = membership("operator", {
      permissions: ["MANAGE", "VIEW", "RESOLVE_GATES"],
    });

    expect(orderedProjectPermissions(source)).toEqual([
      "VIEW",
      "RESOLVE_GATES",
      "MANAGE",
    ]);
  });

  it("не выводит отсутствующее server action как доступное", () => {
    const source = membership("owner", { nextActions: ["EDIT"] });

    expect(membershipAllows(source, "EDIT")).toBe(true);
    expect(membershipAllows(source, "REVOKE")).toBe(false);
  });
});
