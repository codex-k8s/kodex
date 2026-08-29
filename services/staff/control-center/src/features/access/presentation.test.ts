import { describe, expect, it } from "vitest";

import {
  accessScopeKind,
  permissionMessage,
} from "@/features/access/presentation";

describe("access presentation", () => {
  it("resolves dotted permission keys without treating them as a path", () => {
    const messages = {
      "agent.view": {
        name: "Просматривать ИИ-сотрудников",
        description: "Читать профиль сотрудника.",
      },
    };

    expect(permissionMessage(messages, "agent.view", "name")).toBe(
      "Просматривать ИИ-сотрудников",
    );
    expect(permissionMessage(messages, "agent.view", "description")).toBe(
      "Читать профиль сотрудника.",
    );
    expect(permissionMessage(messages, "agent.launch", "name")).toBe(
      "agent.launch",
    );
  });

  it("не показывает отсутствующую область в отказе", () => {
    expect(accessScopeKind(undefined)).toBeUndefined();
    expect(accessScopeKind({})).toBeUndefined();
    expect(accessScopeKind({ kind: "RESOURCE_INSTANCE" })).toBe(
      "RESOURCE_INSTANCE",
    );
  });
});
