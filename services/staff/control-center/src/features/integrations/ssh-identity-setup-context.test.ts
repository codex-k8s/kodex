import { describe, expect, it } from "vitest";
import type { PersonalIntegrationConnection } from "@/shared/api/generated/openapi/types.gen";
import {
  personalSSHSetupContext,
  sharedSSHSetupContext,
} from "./ssh-identity-setup-context";

const base: PersonalIntegrationConnection = {
  connectionRef: "int_fixture01",
  connectionVersion: 3,
  definitionKey: "ssh-managed",
  name: "Disposable SSH",
  policy: "INITIATOR_REQUIRED",
  available: true,
  enabled: true,
  credentialDelivery: { kind: "BROKER_IDENTITY", brokerPurpose: "SSH_TARGET" },
};
const binding: NonNullable<PersonalIntegrationConnection["credential"]> = {
  connectionRef: base.connectionRef,
  connectionVersion: 3,
  bindingRef: "ipc_fixture01",
  version: 2,
  policy: "INITIATOR_REQUIRED",
  state: "CHECK_REQUIRED",
  credentialConfigured: true,
  verifiedCapabilityKeys: [],
  deniedCapabilityKeys: [],
  bindingGeneration: 4,
  credentialDelivery: { kind: "BROKER_IDENTITY", brokerPurpose: "SSH_TARGET" },
  brokerIdentity: { ref: "bki_fixture01", revision: 1 },
};

describe("личный SSH setup: только явные purpose metadata", () => {
  it("разрешает отдельную форму для первого binding и текущего owner snapshot", () => {
    expect(personalSSHSetupContext(base)).toEqual({
      connectionRef: base.connectionRef,
      connectionVersion: 3,
      bindingVersion: 0,
      policy: "INITIATOR_REQUIRED",
    });
    expect(
      personalSSHSetupContext({ ...base, credential: binding })?.bindingVersion,
    ).toBe(2);
    expect(personalSSHSetupContext({ ...base, enabled: false })).toBeDefined();
    expect(
      personalSSHSetupContext({ ...base, definitionKey: "ssh-execute" }),
    ).toBeDefined();
  });
  it("не выбирает форму по имени definition либо TOKEN fallback", () => {
    for (const change of [
      { credentialDelivery: undefined },
      { credentialDelivery: { kind: "TOKEN" } },
      { credentialDelivery: { kind: "NONE" } },
      {
        credentialDelivery: {
          kind: "BROKER_IDENTITY",
          brokerPurpose: "KUBERNETES_TARGET",
        },
      },
      { definitionKey: "ssh-compose" },
      { definitionKey: "foreign" },
      { available: false },
      { policy: "PROJECT_SHARED" },
      { connectionVersion: 0 },
      { connectionVersion: Number.MAX_SAFE_INTEGER + 1 },
      { connectionRef: "short" },
    ])
      expect(
        personalSSHSetupContext({
          ...base,
          ...change,
        } as PersonalIntegrationConnection),
      ).toBeUndefined();
  });
  it("закрывает старую, mixed либо проверяемую binding до owner readback", () => {
    for (const change of [
      { credentialDelivery: undefined },
      { credentialDelivery: { kind: "TOKEN" } },
      { connectionRef: "int_foreign01" },
      { connectionVersion: 2 },
      { bindingGeneration: undefined },
      { bindingGeneration: 0 },
      { bindingGeneration: Number.MAX_SAFE_INTEGER + 1 },
      { version: 0 },
      { state: "CHECKING" },
      { policy: "PROJECT_SHARED" },
    ])
      expect(
        personalSSHSetupContext({
          ...base,
          credential: { ...binding, ...change },
        } as PersonalIntegrationConnection),
      ).toBeUndefined();
  });
});

// Свежий shared snapshot содержит только безопасные metadata; selector SERVICE
// намеренно не переносится в upload context и не становится browser authority.
const shared = {
  ref: "int_fixture01",
  version: 3,
  definitionKey: "ssh-managed",
  name: "Shared SSH",
  state: "DISABLED",
  credentialPolicy: "PROJECT_SHARED",
  credentialsConfigured: false,
  credentialsHint: "",
  capabilities: [],
  grants: [],
  nextActions: ["CONFIGURE_CREDENTIAL"],
  definitionVersion: "1.0.0",
  definitionDigest: "sha256:source",
  publicConfiguration: {
    target_ref: "target_native_ssh",
    service_subject_ref: "subject_service_01",
  },
  credentialDelivery: { kind: "BROKER_IDENTITY", brokerPurpose: "SSH_TARGET" },
} satisfies import("@/shared/api/generated/openapi/types.gen").IntegrationConnection;

describe("общий SSH setup: отдельный protected purpose", () => {
  it("выбирает явный shared context без target/subject/credential payload", () => {
    expect(sharedSSHSetupContext(shared)).toEqual({
      connectionRef: shared.ref,
      connectionVersion: 3,
      bindingVersion: 0,
      policy: "PROJECT_SHARED",
    });
  });
  it("не выводит purpose из definition key, hint либо generic credential", () => {
    for (const change of [
      { credentialDelivery: undefined },
      { credentialDelivery: { kind: "TOKEN" } },
      {
        credentialDelivery: {
          kind: "BROKER_IDENTITY",
          brokerPurpose: "KUBERNETES_TARGET",
        },
      },
      { definitionKey: "ssh-compose" },
      { credentialPolicy: "INITIATOR_REQUIRED" },
      { nextActions: [] },
      { state: "TESTING" },
      { state: "UNKNOWN" },
      { ref: 123456789 },
      { version: 0 },
    ])
      expect(
        sharedSSHSetupContext({
          ...shared,
          ...change,
        } as unknown as import("@/shared/api/generated/openapi/types.gen").IntegrationConnection),
      ).toBeUndefined();
  });
});
