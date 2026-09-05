import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  IntegrationConnection,
  InteractionIdentity,
} from "@/shared/api/generated/openapi/types.gen";
const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}));
vi.mock("@/shared/api/generated/openapi/client.gen", () => ({ client }));
vi.mock("@/shared/api/client", () => ({
  requestSignal: (signal?: AbortSignal) =>
    signal ?? new AbortController().signal,
}));
vi.mock("@/shared/api/mutation", () => ({
  mutate: (
    request: (headers: Record<string, string>) => Promise<unknown>,
    version: number,
  ) =>
    request({
      "If-Match": `"${String(version)}"`,
      "Idempotency-Key": "synthetic-key",
      "X-CSRF-Token": "synthetic-csrf",
    }),
}));
import {
  createInteractionIdentity,
  readInteractionIdentities,
  removeInteractionIdentity,
  validIdentityInput,
} from "./interaction-identities";
const connection: IntegrationConnection = {
  ref: "connection_mattermost",
  version: 17,
  definitionKey: "mattermost",
  name: "Mattermost",
  state: "CONNECTED",
  credentialsConfigured: true,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: [],
  definitionVersion: "1",
  definitionDigest: "a".repeat(64),
  publicConfiguration: {},
};
const binding: InteractionIdentity = {
  ref: "identity_synthetic",
  version: 1,
  connectionRef: connection.ref,
  connectionVersion: connection.version,
  externalTeamRef: "team_synthetic",
  externalChannelRef: "channel_synthetic",
  externalUserDigest: "b".repeat(64),
  subjectRef: "user_synthetic",
  state: "ACTIVE",
};
function response(data: unknown) {
  return { data, response: new Response(null, { status: 200 }) };
}
describe("interaction identity HTTP adapter", () => {
  beforeEach(() => vi.resetAllMocks());
  it("использует connection version для bind и закрытый body без actor", async () => {
    client.post.mockResolvedValue(response(binding));
    await expect(
      createInteractionIdentity(connection, binding),
    ).resolves.toEqual(binding);
    expect(client.post).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { connectionRef: connection.ref },
        headers: expect.objectContaining({ "If-Match": '"17"' }) as unknown,
        body: {
          externalTeamRef: binding.externalTeamRef,
          externalChannelRef: binding.externalChannelRef,
          externalUserDigest: binding.externalUserDigest,
          subjectRef: binding.subjectRef,
        },
      }),
    );
  });
  it("использует identity version для revoke и проверяет terminal receipt", async () => {
    client.delete.mockResolvedValue(
      response({ ...binding, state: "REVOKED", version: 2 }),
    );
    await expect(removeInteractionIdentity(binding)).resolves.toMatchObject({
      state: "REVOKED",
      version: 2,
    });
    expect(client.delete).toHaveBeenCalledWith(
      expect.objectContaining({
        path: { identityRef: binding.ref },
        headers: expect.objectContaining({ "If-Match": '"1"' }) as unknown,
      }),
    );
  });
  it("отклоняет другую connection и повтор курсора", async () => {
    client.get
      .mockResolvedValueOnce(
        response({
          items: [{ ...binding, connectionRef: "other" }],
          nextPageToken: "",
        }),
      )
      .mockResolvedValueOnce(response({ items: [], nextPageToken: "same" }));
    await expect(
      readInteractionIdentities(
        connection.ref,
        undefined,
        new AbortController().signal,
      ),
    ).rejects.toThrow();
    await expect(
      readInteractionIdentities(
        connection.ref,
        "same",
        new AbortController().signal,
      ),
    ).rejects.toThrow();
  });
  it("не принимает bind другой версии либо user и не отправляет неканонический digest", async () => {
    client.post.mockResolvedValue(
      response({ ...binding, connectionVersion: 18 }),
    );
    await expect(
      createInteractionIdentity(connection, binding),
    ).rejects.toThrow("mismatch");
    expect(
      validIdentityInput({ ...binding, externalUserDigest: "B".repeat(64) }),
    ).toBe(false);
    expect(
      validIdentityInput({ ...binding, externalTeamRef: "a".repeat(129) }),
    ).toBe(false);
  });
});
