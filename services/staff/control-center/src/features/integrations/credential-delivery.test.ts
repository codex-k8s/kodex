import { describe, expect, it } from "vitest";
import { readCredentialDelivery } from "./credential-delivery";

describe("credential delivery: закрытые metadata", () => {
  it("не превращает missing/unknown/mixed variant в TOKEN или NONE", () => {
    for (const value of [
      undefined,
      null,
      {},
      { kind: "UNKNOWN" },
      { kind: "token" },
      { kind: "TOKEN", brokerPurpose: "" },
      { kind: "BROKER_IDENTITY" },
      { kind: "BROKER_IDENTITY", brokerPurpose: "GENERIC_SECRET" },
      {
        kind: "BROKER_IDENTITY",
        brokerPurpose: "SSH_TARGET",
        secretKey: "token",
      },
    ])
      expect(readCredentialDelivery(value)).toBeUndefined();
    for (const value of [
      { kind: "NONE" },
      { kind: "TOKEN" },
      { kind: "BROKER_IDENTITY", brokerPurpose: "KUBERNETES_TARGET" },
      { kind: "BROKER_IDENTITY", brokerPurpose: "SSH_TARGET" },
    ])
      expect(readCredentialDelivery(value)).toEqual(value);
  });
});
