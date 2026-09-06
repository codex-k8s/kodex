import { createHash } from "node:crypto";

// Только immutable metadata; credential bytes не входят в снимок PostgreSQL.
export const providerCleanupQuery = `
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object(
 'account', a.ref, 'organization', a.organization_id, 'state', a.state,
 'currentCredential', a.current_credential_revision_id,
 'deletionState', d.state, 'completedAt', d.completed_at,
 'blockers', (SELECT count(*) FROM control_plane.provider_account_blockers(a.organization_id,a.id)),
 'revisions', COALESCE((SELECT jsonb_agg(jsonb_build_object(
   'id',r.id,'SecretName',r.secret_name,'SecretUID',r.secret_uid,
   'SecretResourceVersion',r.secret_resource_version,'ContentSHA256',r.content_sha256))
   FROM control_plane.provider_credential_revisions r WHERE r.provider_account_id=a.id AND r.organization_id=a.organization_id),'[]'::jsonb),
 'authorizations', COALESCE((SELECT jsonb_agg(p.ref)
   FROM control_plane.provider_authorization_attempts p WHERE p.provider_account_id=a.id AND p.organization_id=a.organization_id),'[]'::jsonb),
 'tasks', COALESCE((SELECT jsonb_agg(jsonb_build_object(
   'ref',t.ref,'account',a.ref,'organization',t.organization_id,'kind',t.target_kind,
   'revision',t.provider_credential_revision_id,'authorization',p.ref,
   'materializer',t.materializer_attempt_ref,'uid',t.materializer_attempt_uid,
   'resourceVersion',t.materializer_attempt_resource_version,
   'SecretName',t.secret_name,'SecretUID',t.secret_uid,
   'SecretResourceVersion',t.secret_resource_version,'ContentSHA256',t.content_sha256,
   'predecessor',parent.ref,'generation',t.lease_generation,
   'recoveryRef',t.recovery_task_ref,'recoveryGeneration',t.recovery_generation,
   'legacyGeneration',t.recovery_legacy_last_generation,
   'state',t.state,'receipt',t.terminal_receipt,'error',t.safe_error_code,
   'completedAt',t.completed_at,'leaseOwner',t.lease_owner,'leaseExpiresAt',t.lease_expires_at,
   'completion',t.completion_descriptor))
   FROM control_plane.provider_credential_cleanup_tasks t
   LEFT JOIN control_plane.provider_authorization_attempts p ON p.id=t.provider_authorization_attempt_id
   LEFT JOIN control_plane.provider_credential_cleanup_tasks parent ON parent.id=t.predecessor_task_id
   WHERE t.provider_account_id=a.id),'[]'::jsonb)
)::text FROM control_plane.provider_accounts a
JOIN control_plane.provider_account_deletion_intents d ON d.provider_account_id=a.id AND d.organization_id=a.organization_id
WHERE a.ref=:'account_ref';
COMMIT;
`;

type RecordValue = Record<string, unknown>;
// kubectl не возвращает credential/user-code values. Единственный candidate fence
// проверяется локально и никогда не включается в ошибку или результат проверки.
export const providerCleanupProjection = `go-template={"metadata":{
"name":{{printf "%q" .metadata.name}},"namespace":{{printf "%q" .metadata.namespace}},
"uid":{{printf "%q" .metadata.uid}},"resourceVersion":{{printf "%q" .metadata.resourceVersion}},
"labels":{"provider-credentials.kodex.dev/cleanup-fence":{{printf "%q" (index .metadata.labels "provider-credentials.kodex.dev/cleanup-fence")}},
"app.kubernetes.io/managed-by":{{printf "%q" (index .metadata.labels "app.kubernetes.io/managed-by")}},
"app.kubernetes.io/part-of":{{printf "%q" (index .metadata.labels "app.kubernetes.io/part-of")}}}},
"type":{{printf "%q" .type}},"immutable":{{if .immutable}}true{{else}}false{{end}},
"stringDataPresent":{{if .stringData}}true{{else}}false{{end}},
"dataKeys":[{{$separator := ""}}{{range $key, $value := .data}}{{printf "%s%q" $separator $key}}{{$separator = ","}}{{end}}],
"fenceBase64":{{with index .data "fence.json"}}{{printf "%q" .}}{{else}}""{{end}}}`;
const failure = () => new Error("Provider cleanup proof is invalid");
function requireProof(condition: unknown): asserts condition {
  if (!condition) throw failure();
}
function record(value: unknown): RecordValue {
  requireProof(
    value !== null && typeof value === "object" && !Array.isArray(value),
  );
  return value as RecordValue;
}
function list(value: unknown): unknown[] {
  requireProof(Array.isArray(value) && value.length <= 1024);
  return value as unknown[];
}
function string(value: unknown): string {
  requireProof(typeof value === "string");
  return value;
}
function positive(value: unknown): number {
  requireProof(
    typeof value === "number" && Number.isSafeInteger(value) && value > 0,
  );
  return value;
}
function digest(value: string): string {
  return createHash("sha256").update(value).digest("hex").slice(0, 32);
}
function reference(value: unknown, prefix: string): string {
  const result = string(value);
  requireProof(new RegExp(`^${prefix}_[A-Za-z0-9_-]{8,88}$`).test(result));
  return result;
}
function pins(value: RecordValue): string {
  requireProof(
    /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(string(value.SecretName)) &&
      string(value.SecretName).length <= 63,
  );
  requireProof(/^[a-f0-9-]{36}$/.test(string(value.SecretUID)));
  requireProof(
    string(value.SecretResourceVersion).length > 0 &&
      string(value.SecretResourceVersion).length <= 128,
  );
  requireProof(/^[a-f0-9]{64}$/.test(string(value.ContentSHA256)));
  return JSON.stringify([
    value.SecretName,
    value.SecretUID,
    value.SecretResourceVersion,
    value.ContentSHA256,
  ]);
}

export interface CleanupFenceTarget {
  account: string;
  name: string;
  kind: "CREDENTIAL" | "AUTHORIZATION";
  authorization: string;
  materializer: string;
  originals: { uid: string; resourceVersion: string }[];
}

// Весь граф проверяется до обращения к Kubernetes, включая независимые produced roots.
export function providerCleanupTargets(
  raw: string,
  accountRef: string,
): CleanupFenceTarget[] {
  try {
    requireProof(raw.length > 0 && raw.length <= 256 * 1024);
    const snapshot = record(JSON.parse(raw));
    const account = reference(snapshot.account, "pacc");
    requireProof(
      account === accountRef &&
        snapshot.state === "DELETED" &&
        snapshot.currentCredential === null,
    );
    requireProof(
      snapshot.deletionState === "DELETED" &&
        snapshot.completedAt &&
        snapshot.blockers === 0,
    );
    const organization = string(snapshot.organization);
    const revisions = list(snapshot.revisions).map(record);
    const authorizations = list(snapshot.authorizations).map((value) =>
      reference(value, "pauth"),
    );
    requireProof(new Set(authorizations).size === authorizations.length);
    const tasks = list(snapshot.tasks).map(record);
    requireProof(
      tasks.length > 0 ||
        (revisions.length === 0 && authorizations.length === 0),
    );
    const byRef = new Map(
      tasks.map((task) => [reference(task.ref, "pcct"), task]),
    );
    requireProof(byRef.size === tasks.length);
    const credentialPins = new Map(
      revisions.map((revision) => [pins(revision), revision]),
    );
    requireProof(
      credentialPins.size === revisions.length &&
        new Set(revisions.map((r) => r.id)).size === revisions.length,
    );
    const targets = new Map<string, CleanupFenceTarget>();
    function target(
      name: string,
      kind: CleanupFenceTarget["kind"],
      authorization = "",
      materializer = "",
    ) {
      const existing = targets.get(name);
      requireProof(
        !existing ||
          (existing.kind === kind &&
            (!authorization ||
              !existing.authorization ||
              existing.authorization === authorization)),
      );
      const result = existing ?? {
        account,
        name,
        kind,
        authorization,
        materializer,
        originals: [],
      };
      if (authorization) Object.assign(result, { authorization, materializer });
      targets.set(name, result);
      return result;
    }
    function produced(value: unknown) {
      if (value === null || value === undefined) return;
      const revision = credentialPins.get(pins(record(value)));
      requireProof(
        revision &&
          tasks.some(
            (t) =>
              t.kind === "CREDENTIAL" &&
              t.revision === revision.id &&
              pins(t) === pins(revision),
          ),
      );
    }
    for (const revision of revisions) {
      requireProof(
        tasks.filter(
          (t) =>
            t.kind === "CREDENTIAL" &&
            t.revision === revision.id &&
            t.predecessor === null &&
            pins(t) === pins(revision),
        ).length === 1,
      );
      target(string(revision.SecretName), "CREDENTIAL").originals.push({
        uid: string(revision.SecretUID),
        resourceVersion: string(revision.SecretResourceVersion),
      });
    }
    for (const authorization of authorizations) {
      const materializer = `pmat_${digest(`${authorization}\0${account}`)}`;
      requireProof(
        tasks.some(
          (t) =>
            t.authorization === authorization &&
            t.kind === "AUTHORIZATION_METADATA",
        ),
      );
      target(
        `provider-auth-${digest(materializer)}`,
        "AUTHORIZATION",
        authorization,
        materializer,
      );
      target(
        `provider-credential-${digest(`${account}\0${authorization}`)}`,
        "CREDENTIAL",
        authorization,
        materializer,
      );
    }
    for (const task of tasks) {
      requireProof(
        task.account === account && task.organization === organization,
      );
      requireProof(
        task.state === "COMPLETED" &&
          task.completedAt &&
          task.leaseOwner === null &&
          task.leaseExpiresAt === null,
      );
      const receipt = string(task.receipt);
      requireProof(receipt.length > 0 && receipt.length <= 512);
      positive(task.generation);
      const visited = new Set<string>();
      let cursor = task;
      while (cursor.predecessor !== null) {
        const ref = string(cursor.predecessor);
        requireProof(!visited.has(ref) && byRef.has(ref));
        visited.add(ref);
        const predecessor = byRef.get(ref);
        requireProof(predecessor);
        cursor = predecessor;
      }
      requireProof(
        cursor.kind === "CREDENTIAL" ||
          cursor.kind === "AUTHORIZATION_METADATA",
      );
      const children = tasks.filter(
        (candidate) => candidate.predecessor === task.ref,
      );
      requireProof(children.length <= 1);
      const child = children[0];
      if (child) {
        requireProof(positive(child.generation) > positive(task.generation));
        requireProof(
          child.account === task.account &&
            child.organization === task.organization &&
            child.authorization === task.authorization &&
            child.materializer === task.materializer,
        );
      }
      if (task.kind === "CREDENTIAL") {
        requireProof(
          task.authorization === null &&
            task.materializer === null &&
            task.uid === null &&
            task.resourceVersion === null,
        );
        requireProof(
          revisions.some(
            (r) => r.id === task.revision && pins(r) === pins(task),
          ),
        );
      } else {
        requireProof(
          [
            "AUTHORIZATION_METADATA",
            "AUTHORIZATION_ATTEMPT",
            "AUTHORIZATION_ABSENCE",
          ].includes(string(task.kind)),
        );
        requireProof(
          authorizations.includes(string(task.authorization)) &&
            task.materializer ===
              `pmat_${digest(`${string(task.authorization)}\0${account}`)}`,
        );
        requireProof(
          task.revision === null &&
            task.SecretName === null &&
            task.SecretUID === null &&
            task.SecretResourceVersion === null &&
            task.ContentSHA256 === null,
        );
        if (task.kind === "AUTHORIZATION_ATTEMPT") {
          requireProof(
            /^[a-f0-9-]{36}$/.test(string(task.uid)) &&
              string(task.resourceVersion).length > 0,
          );
          target(
            `provider-auth-${digest(string(task.materializer))}`,
            "AUTHORIZATION",
          ).originals.push({
            uid: string(task.uid),
            resourceVersion: string(task.resourceVersion),
          });
        } else requireProof(task.uid === null && task.resourceVersion === null);
      }
      if (receipt.startsWith("superseded:dead-letter:")) {
        requireProof(
          child &&
            child.kind === task.kind &&
            child.revision === task.revision &&
            child.uid === task.uid &&
            child.resourceVersion === task.resourceVersion,
        );
        if (task.kind === "CREDENTIAL")
          requireProof(pins(child) === pins(task));
        reference(task.recoveryRef, "pcct");
        positive(task.recoveryGeneration);
        requireProof(
          child.recoveryRef === task.recoveryRef &&
            child.recoveryGeneration === task.recoveryGeneration &&
            child.legacyGeneration === task.legacyGeneration,
        );
        continue;
      }
      if (receipt.startsWith("no-effect-cas:")) {
        requireProof(
          task.error === "CAS_CHANGED" &&
            ["AUTHORIZATION_ATTEMPT", "AUTHORIZATION_ABSENCE"].includes(
              string(task.kind),
            ) &&
            child?.kind === "AUTHORIZATION_METADATA",
        );
        continue;
      }
      requireProof(
        task.error === "" &&
          !receipt.startsWith("superseded:") &&
          !receipt.startsWith("dead-letter:"),
      );
      const completion =
        task.completion === null ? {} : record(task.completion);
      produced(completion.ProducedCredential);
      if (task.kind === "AUTHORIZATION_METADATA") {
        requireProof(
          child &&
            receipt.startsWith("provider-metadata:") &&
            !completion.TerminalReceipt &&
            !completion.ProducedCredential,
        );
        const observation = record(completion.Observation);
        const observed = record(observation.Target);
        requireProof(
          observed.TaskRef === task.ref &&
            observed.AccountRef === account &&
            observed.Generation === task.generation &&
            observed.AuthorizationAttemptRef === task.authorization &&
            observed.MaterializerAttemptRef === task.materializer,
        );
        requireProof(
          child.kind === observed.Kind &&
            (child.uid ?? "") === observed.UID &&
            (child.resourceVersion ?? "") === observed.ResourceVersion,
        );
        requireProof(
          observation.State === "PRESENT"
            ? child.kind === "AUTHORIZATION_ATTEMPT"
            : ["ABSENT_UNFENCED", "CONFIRMED_ABSENT"].includes(
                string(observation.State),
              ) && child.kind === "AUTHORIZATION_ABSENCE",
        );
        produced(observation.ProducedCredential);
      } else requireProof(!child && !completion.Observation);
    }
    return [...targets.values()];
  } catch {
    throw failure();
  }
}

// Даже ошибка разбора не должна возвращать Secret.data, stdout или JSON excerpt.
export function verifyProviderCleanupFence(
  raw: string,
  target: CleanupFenceTarget,
): void {
  try {
    requireProof(raw.length > 0 && raw.length <= 64 * 1024);
    const secret = record(JSON.parse(raw));
    const metadata = record(secret.metadata);
    const labels = record(metadata.labels);
    requireProof(
      metadata.name === target.name &&
        metadata.namespace === "kodex-runtime" &&
        /^[a-f0-9-]{36}$/.test(string(metadata.uid)) &&
        string(metadata.resourceVersion).length > 0 &&
        string(metadata.resourceVersion).length <= 128,
    );
    requireProof(
      secret.immutable === true &&
        secret.type === "Opaque" &&
        secret.stringDataPresent === false,
    );
    requireProof(
      labels["provider-credentials.kodex.dev/cleanup-fence"] === "true" &&
        labels["app.kubernetes.io/managed-by"] === "secret-broker" &&
        labels["app.kubernetes.io/part-of"] === "kodex",
    );
    const keysPresent = list(secret.dataKeys);
    requireProof(keysPresent.length === 1 && keysPresent[0] === "fence.json");
    const encoded = string(secret.fenceBase64);
    const bytes = Buffer.from(encoded, "base64");
    requireProof(
      bytes.length > 0 &&
        bytes.length <= 8192 &&
        bytes.toString("base64") === encoded,
    );
    const rawFence = bytes.toString("utf8");
    const fence = record(JSON.parse(rawFence));
    const keys = [
      "Schema",
      "Kind",
      "AccountRef",
      "AuthorizationAttemptRef",
      "MaterializerAttemptRef",
      "SecretName",
      "OriginalUID",
      "OriginalResourceVersion",
    ];
    requireProof(
      Object.keys(fence).length === keys.length &&
        keys.every((key) => typeof fence[key] === "string"),
    );
    requireProof(
      JSON.stringify(
        Object.fromEntries(keys.map((key) => [key, fence[key]])),
      ) === rawFence,
    );
    requireProof(
      fence.Schema === "kodex.provider-cleanup-fence.v1" &&
        fence.Kind === target.kind &&
        fence.AccountRef === target.account &&
        fence.SecretName === target.name,
    );
    const exactAuthorization =
      target.authorization !== "" &&
      fence.AuthorizationAttemptRef === target.authorization &&
      fence.MaterializerAttemptRef === target.materializer;
    requireProof(
      exactAuthorization ||
        (target.kind === "CREDENTIAL" &&
          fence.AuthorizationAttemptRef === "" &&
          fence.MaterializerAttemptRef === ""),
    );
    const emptyOriginal =
      fence.OriginalUID === "" && fence.OriginalResourceVersion === "";
    requireProof(
      (emptyOriginal && exactAuthorization) ||
        target.originals.some(
          (original) =>
            original.uid === fence.OriginalUID &&
            original.resourceVersion === fence.OriginalResourceVersion,
        ),
    );
    if (target.kind === "CREDENTIAL")
      requireProof(
        target.originals.every((original) => metadata.uid !== original.uid),
      );
  } catch {
    throw failure();
  }
}
