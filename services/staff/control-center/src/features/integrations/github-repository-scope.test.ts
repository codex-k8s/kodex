import { describe, expect, it } from "vitest";
import { githubDiscoveryView } from "./github-repository-scope";
import type { GitHubRepositoryDiscoveryPage } from "@/shared/api/generated/openapi/types.gen";

function required<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Synthetic fixture value is absent");
  return value;
}

const fixture = (): GitHubRepositoryDiscoveryPage => ({
  identityId: 7,
  login: "synthetic",
  page: 1,
  nextPage: 0,
  items: [
    {
      id: 17,
      fullName: "team/repo",
      ownerKind: "Organization",
      private: true,
      archived: false,
      canRead: true,
      canPush: false,
    },
  ],
});

describe("безопасная страница GitHub", () => {
  it("сохраняет пустую последнюю страницу, отрицательные права и только типизированные поля", () => {
    const input = fixture();
    const safe = githubDiscoveryView({
      ...input,
      token: "synthetic-secret",
    } as GitHubRepositoryDiscoveryPage);
    expect(safe).toEqual(input);
    expect(safe).not.toHaveProperty("token");
    expect(safe.items[0]?.canPush).toBe(false);
    expect(githubDiscoveryView({ ...input, items: [] })).toEqual({
      ...input,
      items: [],
    });
    required(input.items[0]).fullName = "other/repo";
    expect(required(safe.items[0]).fullName).toBe("team/repo");
  });
  it.each([
    "identity",
    "cursor",
    "duplicate",
    "foreignName",
    "missingPermission",
  ])("закрыто отклоняет %s", (kind) => {
    const input = fixture();
    switch (kind) {
      case "identity":
        input.identityId = 0;
        break;
      case "cursor":
        input.nextPage = 3;
        break;
      case "duplicate":
        input.items.push({ ...required(input.items[0]) });
        break;
      case "foreignName":
        required(input.items[0]).fullName = "team/../repo";
        break;
      case "missingPermission":
        required(input.items[0]).canRead = false;
        break;
    }
    expect(() => githubDiscoveryView(input)).toThrow();
  });
});
