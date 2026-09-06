import { createHash } from "node:crypto";

import { describe, expect, it } from "vitest";

import {
  providerCleanupTargets,
  verifyProviderCleanupFence,
  type CleanupFenceTarget,
} from "./provider-cleanup-proof";

const account = "pacc_fixture1";
function present<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Required fixture item is missing");
  return value;
}
const authorization = "pauth_fixture1";
const hash = (value: string) =>
  createHash("sha256").update(value).digest("hex").slice(0, 32);
const materializer = `pmat_${hash(`${authorization}\0${account}`)}`;
const credential = {
  id: "revision-1",
  SecretName: `provider-credential-${hash(`${account}\0${authorization}`)}`,
  SecretUID: "11111111-1111-4111-8111-111111111111",
  SecretResourceVersion: "21",
  ContentSHA256: "a".repeat(64),
};

function fixture() {
  const task: Record<string, unknown> = {
    ref: "pcct_credential1",
    account,
    organization: "organization-1",
    kind: "CREDENTIAL",
    revision: credential.id,
    ...credential,
    authorization: null,
    materializer: null,
    uid: null,
    resourceVersion: null,
    predecessor: null,
    generation: 1,
    recoveryRef: "pcct_credential1",
    recoveryGeneration: 1,
    legacyGeneration: 0,
    state: "COMPLETED",
    receipt: "broker-cleanup-receipt",
    error: "",
    completedAt: "2026-09-06T12:00:00Z",
    leaseOwner: null,
    leaseExpiresAt: null,
    completion: null,
  };
  const metadata = {
    ...task,
    ref: "pcct_metadata1",
    kind: "AUTHORIZATION_METADATA",
    revision: null,
    SecretName: null,
    SecretUID: null,
    SecretResourceVersion: null,
    ContentSHA256: null,
    authorization,
    materializer,
    receipt: "provider-metadata:fixture",
    recoveryRef: "pcct_metadata1",
    completion: {
      TerminalReceipt: "",
      ProducedCredential: null,
      Observation: {
        State: "PRESENT",
        Target: {
          TaskRef: "pcct_metadata1",
          AccountRef: account,
          Generation: 1,
          AuthorizationAttemptRef: authorization,
          MaterializerAttemptRef: materializer,
          Kind: "AUTHORIZATION_ATTEMPT",
          UID: "22222222-2222-4222-8222-222222222222",
          ResourceVersion: "31",
        },
        ProducedCredential: null,
      },
    },
  };
  const attempt: Record<string, unknown> = {
    ...metadata,
    ref: "pcct_attempt1",
    kind: "AUTHORIZATION_ATTEMPT",
    predecessor: metadata.ref,
    generation: 2,
    uid: "22222222-2222-4222-8222-222222222222",
    resourceVersion: "31",
    completion: {
      TerminalReceipt: "broker-authorization-receipt",
      ProducedCredential: null,
      Observation: null,
    },
    receipt: "broker-authorization-receipt",
  };
  return {
    account,
    organization: "organization-1",
    state: "DELETED",
    currentCredential: null,
    deletionState: "DELETED",
    completedAt: "2026-09-06T12:00:00Z",
    blockers: 0,
    revisions: [{ ...credential }],
    authorizations: [authorization],
    tasks: [task, metadata, attempt] as Record<string, unknown>[],
  };
}

function fence(target: CleanupFenceTarget) {
  const original = target.originals[0];
  const descriptor = {
    Schema: "kodex.provider-cleanup-fence.v1",
    Kind: target.kind,
    AccountRef: account,
    AuthorizationAttemptRef:
      target.kind === "AUTHORIZATION" ? authorization : "",
    MaterializerAttemptRef: target.kind === "AUTHORIZATION" ? materializer : "",
    SecretName: target.name,
    OriginalUID: original?.uid ?? "",
    OriginalResourceVersion: original?.resourceVersion ?? "",
  };
  return {
    metadata: {
      name: target.name,
      namespace: "kodex-runtime",
      uid:
        target.kind === "AUTHORIZATION"
          ? original?.uid
          : "33333333-3333-4333-8333-333333333333",
      resourceVersion: "99",
      labels: {
        "provider-credentials.kodex.dev/cleanup-fence": "true",
        "app.kubernetes.io/managed-by": "secret-broker",
        "app.kubernetes.io/part-of": "kodex",
      },
    },
    type: "Opaque",
    immutable: true,
    stringDataPresent: false,
    dataKeys: ["fence.json"],
    fenceBase64: Buffer.from(JSON.stringify(descriptor)).toString("base64"),
  };
}

describe("Доказательство очистки provider credential", () => {
  it("допускает пустой граф только до любой authorization/materialization, а не по клиентскому ACK", () => {
    const value = fixture();
    value.tasks = [];
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow();
    value.revisions = [];
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow();
    value.authorizations = [];
    expect(providerCleanupTargets(JSON.stringify(value), account)).toEqual([]);
  });
  it("проверяет materialized revision и metadata→attempt, допускает прежний UID только у authorization fence", () => {
    const targets = providerCleanupTargets(JSON.stringify(fixture()), account);
    expect(targets).toHaveLength(2);
    for (const target of targets)
      expect(() =>
        verifyProviderCleanupFence(JSON.stringify(fence(target)), target),
      ).not.toThrow();
  });

  it("принимает завершённый superseded predecessor только с сохранёнными pins и recovery tuple", () => {
    const value = fixture();
    const old = present(value.tasks[0]);
    old.receipt = "superseded:dead-letter:fixture";
    old.error = "CREDENTIAL_CLEANUP_FAILED";
    value.tasks.push({
      ...old,
      ref: "pcct_successor1",
      predecessor: old.ref,
      generation: 2,
      receipt: "broker-cleanup-receipt",
      error: "",
    });
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).not.toThrow();
    present(value.tasks[3]).recoveryGeneration = 2;
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow("Provider cleanup proof is invalid");
  });

  it("доказывает очистку replacement UID отдельной revision и её root, а не только predecessor chain", () => {
    const value = fixture();
    const replacement = {
      ...credential,
      id: "revision-2",
      SecretUID: "44444444-4444-4444-8444-444444444444",
      SecretResourceVersion: "41",
      ContentSHA256: "b".repeat(64),
    };
    value.revisions.push(replacement);
    const original = present(value.tasks[0]);
    original.completion = {
      TerminalReceipt: original.receipt,
      ProducedCredential: replacement,
      Observation: null,
    };
    value.tasks.push({
      ...original,
      ...replacement,
      ref: "pcct_replacement1",
      revision: replacement.id,
      completion: null,
    });
    const target = present(
      providerCleanupTargets(JSON.stringify(value), account)[0],
    );
    const secret = fence(target);
    expect(() =>
      verifyProviderCleanupFence(JSON.stringify(secret), target),
    ).not.toThrow();
    secret.metadata.uid = replacement.SecretUID;
    expect(() =>
      verifyProviderCleanupFence(JSON.stringify(secret), target),
    ).toThrow();
    value.tasks.pop();
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow();
  });

  it("принимает отсутствие material только с exact authorization и credential fences, закрывает чужую lineage", () => {
    const value = fixture();
    value.revisions = [];
    value.tasks.shift();
    const metadata = present(value.tasks[0]);
    metadata.completion = {
      TerminalReceipt: "",
      ProducedCredential: null,
      Observation: {
        State: "ABSENT_UNFENCED",
        Target: {
          TaskRef: metadata.ref,
          AccountRef: account,
          Generation: 1,
          AuthorizationAttemptRef: authorization,
          MaterializerAttemptRef: materializer,
          Kind: "AUTHORIZATION_ABSENCE",
          UID: "",
          ResourceVersion: "",
        },
        ProducedCredential: null,
      },
    };
    Object.assign(present(value.tasks[1]), {
      kind: "AUTHORIZATION_ABSENCE",
      uid: null,
      resourceVersion: null,
    });
    const targets = providerCleanupTargets(JSON.stringify(value), account);
    expect(targets).toHaveLength(2);
    for (const target of targets) {
      const secret = fence(target);
      secret.metadata.uid = "33333333-3333-4333-8333-333333333333";
      const descriptor = JSON.parse(
        Buffer.from(secret.fenceBase64, "base64").toString(),
      ) as Record<string, unknown>;
      descriptor.AuthorizationAttemptRef = authorization;
      descriptor.MaterializerAttemptRef = materializer;
      secret.fenceBase64 = Buffer.from(JSON.stringify(descriptor)).toString(
        "base64",
      );
      expect(() =>
        verifyProviderCleanupFence(JSON.stringify(secret), target),
      ).not.toThrow();
      descriptor.MaterializerAttemptRef = "pmat_foreign";
      secret.fenceBase64 = Buffer.from(JSON.stringify(descriptor)).toString(
        "base64",
      );
      expect(() =>
        verifyProviderCleanupFence(JSON.stringify(secret), target),
      ).toThrow();
    }
  });

  it("проверяет CAS_CHANGED→новое наблюдение→absence и отдельный produced credential root", () => {
    const value = fixture();
    const old = present(value.tasks[2]);
    old.receipt = "no-effect-cas:fixture";
    old.error = "CAS_CHANGED";
    old.completion = null;
    const metadata = {
      ...present(value.tasks[1]),
      ref: "pcct_metadata2",
      predecessor: old.ref,
      generation: 3,
      completion: {
        TerminalReceipt: "",
        ProducedCredential: null,
        Observation: {
          State: "CONFIRMED_ABSENT",
          Target: {
            TaskRef: "pcct_metadata2",
            AccountRef: account,
            Generation: 3,
            AuthorizationAttemptRef: authorization,
            MaterializerAttemptRef: materializer,
            Kind: "AUTHORIZATION_ABSENCE",
            UID: "",
            ResourceVersion: "",
          },
          ProducedCredential: credential,
        },
      },
    };
    value.tasks.push(metadata, {
      ...old,
      ref: "pcct_absence1",
      kind: "AUTHORIZATION_ABSENCE",
      predecessor: metadata.ref,
      generation: 4,
      uid: null,
      resourceVersion: null,
      receipt: "broker-absence-receipt",
      error: "",
      completion: {
        TerminalReceipt: "broker-absence-receipt",
        ProducedCredential: credential,
        Observation: null,
      },
    });
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).not.toThrow();
    value.tasks.shift();
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow();
  });

  it.each([
    [
      "foreign organization",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[0]).organization = "foreign";
      },
    ],
    [
      "dangling predecessor",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[2]).predecessor = "pcct_missing1";
      },
    ],
    [
      "cycle",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[1]).predecessor = present(v.tasks[2]).ref;
      },
    ],
    [
      "metadata leaf",
      (v: ReturnType<typeof fixture>) => {
        v.tasks.pop();
      },
    ],
    [
      "uncompleted successor",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[2]).state = "DEAD_LETTER";
      },
    ],
    [
      "live lease",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[0]).leaseOwner = "worker";
      },
    ],
    [
      "unmatched material",
      (v: ReturnType<typeof fixture>) => {
        v.revisions.push({
          ...credential,
          id: "revision-2",
          SecretUID: "44444444-4444-4444-8444-444444444444",
        });
      },
    ],
    [
      "superseded without successor",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[0]).receipt = "superseded:dead-letter:fixture";
      },
    ],
    [
      "unknown safe error",
      (v: ReturnType<typeof fixture>) => {
        present(v.tasks[0]).error = "CAS_CHANGED";
      },
    ],
    [
      "retained current credential",
      (v: ReturnType<typeof fixture>) => {
        Object.assign(v, { currentCredential: credential.id });
      },
    ],
    [
      "blocker",
      (v: ReturnType<typeof fixture>) => {
        v.blockers = 1;
      },
    ],
  ] as const)("отклоняет %s", (_label, change) => {
    const value = fixture();
    change(value);
    expect(() =>
      providerCleanupTargets(JSON.stringify(value), account),
    ).toThrow("Provider cleanup proof is invalid");
  });

  it.each([
    "material",
    "foreign",
    "mutable",
    "original UID",
    "missing",
    "corrupt",
    "unknown field",
  ])("отклоняет fence: %s без раскрытия data", (reason) => {
    const target = present(
      providerCleanupTargets(JSON.stringify(fixture()), account)[0],
    );
    const secret = fence(target);
    const canary = "PRIVATE-CREDENTIAL-MATERIAL";
    if (reason === "material") secret.dataKeys.push("credential.json");
    if (reason === "foreign") secret.metadata.name = "foreign-secret";
    if (reason === "mutable") secret.immutable = false;
    if (reason === "original UID") secret.metadata.uid = credential.SecretUID;
    if (reason === "corrupt")
      secret.fenceBase64 = Buffer.from(canary).toString("base64");
    if (reason === "unknown field") {
      const parsed = JSON.parse(
        Buffer.from(secret.fenceBase64, "base64").toString(),
      ) as Record<string, unknown>;
      parsed.unexpected = canary;
      secret.fenceBase64 = Buffer.from(JSON.stringify(parsed)).toString(
        "base64",
      );
    }
    try {
      verifyProviderCleanupFence(
        reason === "missing" ? "" : JSON.stringify(secret),
        target,
      );
      expect.fail("Expected closed rejection");
    } catch (error) {
      expect(error).toBeInstanceOf(Error);
      expect((error as Error).message).toBe(
        "Provider cleanup proof is invalid",
      );
      expect(JSON.stringify(error)).not.toContain(canary);
    }
  });
});
