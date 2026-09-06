import { beforeEach, expect, it, vi } from "vitest";
import type { Membership } from "@/shared/api/generated/openapi/types.gen";
const sdk = vi.hoisted(() => ({
  listOrganizationProjectMemberships: vi.fn<
    (options: {
      query: {
        query: string;
        pageToken?: string;
        projectRef?: string;
        pageSize: number;
      };
      signal: AbortSignal;
    }) => Promise<{
      data: { items: Membership[]; nextPageToken?: string };
      response: Response;
    }>
  >(),
}));
vi.mock("@/shared/api/generated/openapi/sdk.gen", () => sdk);
vi.mock("@/shared/api/client", () => ({
  requestSignal: () => new AbortController().signal,
}));
import { loadCatalog } from "./api";
const membership: Membership = {
  ref: "membership_synthetic",
  projectRef: "project_synthetic",
  version: 2,
  user: { ref: "user_synthetic", displayName: "Участник проверки" },
  platformRole: "MEMBER",
  permissions: ["VIEW"],
  active: true,
  nextActions: [],
};
beforeEach(() => vi.resetAllMocks());
it.each([undefined, "project_synthetic"])(
  "передаёт query/cursor/scope %s владельцу, не агрегирует проектные списки",
  async (projectRef) => {
    sdk.listOrganizationProjectMemberships.mockResolvedValue({
      data: { items: [membership], nextPageToken: "next" },
      response: new Response(null),
    });
    const owner = new AbortController();
    const page = await loadCatalog(
      "members",
      "Участник",
      owner.signal,
      "cursor",
      projectRef,
    );
    expect(sdk.listOrganizationProjectMemberships).toHaveBeenCalledOnce();
    const sent = sdk.listOrganizationProjectMemberships.mock.calls[0]?.[0];
    expect(sent?.query).toEqual({
      query: "Участник",
      pageToken: "cursor",
      projectRef,
      pageSize: 30,
    });
    expect(sent?.signal).toBeInstanceOf(AbortSignal);
    expect(page.nextPageToken).toBe("next");
    expect(page.items[0]).toMatchObject({
      projectRef: membership.projectRef,
      title: membership.user.displayName,
      role: "MEMBER",
      path: "/projects/project_synthetic/members",
    });
    owner.abort();
    expect(
      sdk.listOrganizationProjectMemberships.mock.calls[0]?.[0].signal.aborted,
    ).toBe(true);
  },
);
it.each([undefined, "project_foreign"])(
  "закрыто отклоняет несовместимую project projection %s",
  async (projectRef) => {
    sdk.listOrganizationProjectMemberships.mockResolvedValue({
      data: { items: [{ ...membership, projectRef }] },
      response: new Response(null),
    });
    await expect(
      loadCatalog(
        "members",
        "",
        new AbortController().signal,
        undefined,
        "project_synthetic",
      ),
    ).rejects.toThrow("membership catalog scope");
  },
);
