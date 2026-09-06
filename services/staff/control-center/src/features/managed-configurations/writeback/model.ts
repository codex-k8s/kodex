import type {
  ConfigurationWriteBack,
  ConfigurationWriteBackPage,
  ConfigurationWriteBackView,
  ManagedConfiguration,
} from "@/shared/api/generated/openapi/types.gen";

export type Proposal = ConfigurationWriteBack;
export type Action = Proposal["nextActions"][number]["action"];
export const maximumContentBytes = 256 * 1024;
const refPattern = /^[A-Za-z0-9_-]{1,128}$/;
const digestPattern = /^[a-f0-9]{64}$/;
export const states = [
  "WAITING_APPROVAL",
  "QUEUED",
  "CLAIMED",
  "EFFECT_STARTED",
  "SUCCEEDED",
  "REJECTED",
  "CANCELLED",
  "EXPIRED",
  "FAILED",
  "UNKNOWN_OUTCOME",
] as const;
const actionNames: Action[] = ["APPROVE", "REJECT", "CANCEL"];
const reasons = [
  "NONE",
  "FORBIDDEN",
  "STATE",
  "SOURCE_CHANGED",
  "EXPIRED",
  "OUTCOME_UNKNOWN",
];
const failures = [
  "UNAVAILABLE",
  "CREDENTIAL_REJECTED",
  "ACCESS_DENIED",
  "SOURCE_CHANGED",
  "CONTENT_INVALID",
  "RESPONSE_INVALID",
  "AUTHORITY_CHANGED",
  "DEADLINE_EXCEEDED",
  "BRANCH_CONFLICT",
  "OUTCOME_UNCONFIRMED",
];
export function contentBytes(content: string): number {
  return new TextEncoder().encode(content).length;
}
export async function contentDigest(content: string): Promise<string> {
  if (
    !content ||
    contentBytes(content) > maximumContentBytes ||
    content.includes("\0")
  )
    throw new Error("Invalid write-back content");
  const hash = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(content),
  );
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
export function preparationReason(
  configuration: ManagedConfiguration,
): "kind" | "git" | "source" | undefined {
  if (!["ROLE_IMAGE", "INTEGRATION_DEFINITION"].includes(configuration.kind))
    return "kind";
  if (configuration.managedBy !== "GIT") return "git";
  const source = configuration.gitSource;
  if (
    !source ||
    source.state !== "READY" ||
    !source.acceptedCommitSha ||
    !source.acceptedContentSha256 ||
    !source.acceptedRevisionRef
  )
    return "source";
  return undefined;
}
export function pollsNeeded(proposal: Proposal): boolean {
  return ["QUEUED", "CLAIMED", "EFFECT_STARTED", "UNKNOWN_OUTCOME"].includes(
    proposal.state,
  );
}
export function actionReason(
  proposal: Proposal,
  action: Action,
  now = Date.now(),
): string | undefined {
  if (proposal.state === "UNKNOWN_OUTCOME") return "OUTCOME_UNKNOWN";
  if (
    ![
      "WAITING_APPROVAL",
      ...(action === "CANCEL" ? ["QUEUED", "CLAIMED"] : []),
    ].includes(proposal.state)
  )
    return "STATE";
  const value = proposal.nextActions.find((item) => item.action === action);
  if (!value || !value.enabled) return value?.reason ?? "STATE";
  if (now >= Date.parse(proposal.expiresAt)) return "EXPIRED";
  return undefined;
}
export function safePullRequestUrl(
  value: string | undefined,
): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password
      ? url.href
      : undefined;
  } catch {
    return undefined;
  }
}
const immutableFields = [
  "configurationRef",
  "sourceRef",
  "connectionRef",
  "configurationVersion",
  "sourceVersion",
  "connectionVersion",
  "kind",
  "repositoryRef",
  "sourceRefName",
  "path",
  "baseCommitSha",
  "baseContentSha256",
  "proposedContentSha256",
  "approvalDigest",
  "contentFormat",
  "proposalBranch",
  "createdAt",
  "expiresAt",
] as const;
export function checkedProposal(
  proposal: Proposal,
  configurationRef: string,
  previous?: Proposal,
): Proposal {
  const actions: readonly Proposal["nextActions"][number][] =
    proposal.nextActions;
  if (
    proposal.configurationRef !== configurationRef ||
    !refPattern.test(proposal.ref) ||
    !["ROLE_IMAGE", "INTEGRATION_DEFINITION"].includes(proposal.kind) ||
    !states.includes(proposal.state) ||
    ![
      proposal.version,
      proposal.configurationVersion,
      proposal.sourceVersion,
      proposal.connectionVersion,
    ].every((n) => Number.isSafeInteger(n) && n > 0) ||
    ![
      proposal.baseContentSha256,
      proposal.proposedContentSha256,
      proposal.approvalDigest,
    ].every((value) => digestPattern.test(value)) ||
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(proposal.baseCommitSha) ||
    !["JSON", "YAML"].includes(proposal.contentFormat) ||
    !Number.isFinite(Date.parse(proposal.expiresAt)) ||
    !Number.isFinite(Date.parse(proposal.createdAt)) ||
    [
      proposal.approvedAt,
      proposal.completedAt,
      proposal.branchConfirmedAt,
      proposal.pullRequestConfirmedAt,
    ].some(
      (value) => value !== undefined && !Number.isFinite(Date.parse(value)),
    ) ||
    actions.length !== 3 ||
    new Set(proposal.nextActions.map((item) => item.action)).size !== 3 ||
    proposal.nextActions.some(
      (item) =>
        !actionNames.includes(item.action) ||
        !reasons.includes(item.reason) ||
        typeof item.enabled !== "boolean" ||
        (item.enabled && item.reason !== "NONE"),
    ) ||
    (proposal.failureCode && !failures.includes(proposal.failureCode)) ||
    (proposal.pullRequestUrl && !safePullRequestUrl(proposal.pullRequestUrl)) ||
    (proposal.state === "SUCCEEDED" &&
      (!proposal.pullRequestRef ||
        !proposal.pullRequestUrl ||
        !proposal.candidateCommitSha ||
        !proposal.branchConfirmedAt ||
        !proposal.pullRequestConfirmedAt))
  )
    throw new Error("Invalid write-back proposal readback");
  if (
    previous &&
    (proposal.ref !== previous.ref ||
      proposal.version < previous.version ||
      immutableFields.some((key) => proposal[key] !== previous[key]))
  )
    throw new Error("Write-back proposal lineage changed");
  return proposal;
}
export async function checkedView(
  view: ConfigurationWriteBackView,
  configurationRef: string,
  previous?: Proposal,
): Promise<ConfigurationWriteBackView> {
  checkedProposal(view.proposal, configurationRef, previous);
  if (
    (await contentDigest(view.baseContent)) !==
      view.proposal.baseContentSha256 ||
    (await contentDigest(view.proposedContent)) !==
      view.proposal.proposedContentSha256
  )
    throw new Error("Write-back document digest mismatch");
  return view;
}
export function checkedPage(
  page: ConfigurationWriteBackPage,
  configurationRef: string,
  cursor?: string,
  previous: Proposal[] = [],
): ConfigurationWriteBackPage {
  if (
    !Number.isSafeInteger(page.total) ||
    page.total < 0 ||
    page.items.length > 30 ||
    page.total < page.items.length ||
    (cursor && page.nextPageToken === cursor) ||
    new Set([...previous, ...page.items].map((item) => item.ref)).size !==
      previous.length + page.items.length
  )
    throw new Error("Invalid write-back history page");
  page.items.forEach((item) => checkedProposal(item, configurationRef));
  return page;
}

export interface Intent {
  configurationRef: string;
  kind: "ROLE_IMAGE" | "INTEGRATION_DEFINITION";
  action: "PREPARE" | Action;
  key: string;
  version: number;
  sourceVersion?: number;
  sourceRef?: string;
  contentDigest?: string;
  proposalRef?: string;
  approvalDigest?: string;
}
export type RecoveryStorage = Pick<
  Storage,
  "getItem" | "setItem" | "removeItem"
>;
const storagePrefix = "kodex.writeback.intent.";
// Вызывается общим owner logout/invalidation вместе с остальными recovery keys.
export function clearWriteBackRecovery(
  storage: Pick<Storage, "length" | "key" | "removeItem">,
): void {
  const keys: string[] = [];
  for (let index = 0; index < storage.length; index++) {
    const key = storage.key(index);
    if (key?.startsWith(storagePrefix)) keys.push(key);
  }
  keys.forEach((key) => storage.removeItem(key));
}
export function checkedIntent(intent: Intent): Intent {
  const keys = Object.keys(intent).sort().join(",");
  const expected =
    intent.action === "PREPARE"
      ? "action,configurationRef,contentDigest,key,kind,sourceRef,sourceVersion,version"
      : "action,approvalDigest,configurationRef,key,kind,proposalRef,version";
  if (
    keys !== expected ||
    !refPattern.test(intent.configurationRef) ||
    !["ROLE_IMAGE", "INTEGRATION_DEFINITION"].includes(intent.kind) ||
    !Number.isSafeInteger(intent.version) ||
    intent.version < 1 ||
    !/^[a-f0-9-]{36}$/.test(intent.key) ||
    (intent.action === "PREPARE"
      ? !Number.isSafeInteger(intent.sourceVersion) ||
        (intent.sourceVersion ?? 0) < 1 ||
        !refPattern.test(intent.sourceRef ?? "") ||
        !digestPattern.test(intent.contentDigest ?? "")
      : !actionNames.includes(intent.action) ||
        !refPattern.test(intent.proposalRef ?? "") ||
        !digestPattern.test(intent.approvalDigest ?? ""))
  )
    throw new Error("Invalid write-back recovery metadata");
  return intent;
}
export function loadIntent(
  storage: RecoveryStorage,
  configurationRef: string,
): Intent | undefined {
  const raw = storage.getItem(storagePrefix + configurationRef);
  if (!raw) return undefined;
  if (raw.length > 2048)
    throw new Error("Invalid write-back recovery metadata");
  const intent = checkedIntent(JSON.parse(raw) as Intent);
  if (intent.configurationRef !== configurationRef)
    throw new Error("Write-back recovery scope mismatch");
  return intent;
}
export function saveIntent(storage: RecoveryStorage, intent: Intent): void {
  checkedIntent(intent);
  const previous = loadIntent(storage, intent.configurationRef);
  if (previous && JSON.stringify(previous) !== JSON.stringify(intent))
    throw new Error("Write-back mutation intent changed");
  storage.setItem(
    storagePrefix + intent.configurationRef,
    JSON.stringify(intent),
  );
}
export function clearIntent(storage: RecoveryStorage, intent: Intent): void {
  if (loadIntent(storage, intent.configurationRef)?.key === intent.key) {
    storage.removeItem(storagePrefix + intent.configurationRef);
    storage.removeItem(storagePrefix + intent.configurationRef + ".rejected");
  }
}
export function matchesPreparation(
  intent: Intent,
  proposal: Proposal,
): boolean {
  return (
    intent.action === "PREPARE" &&
    proposal.configurationRef === intent.configurationRef &&
    proposal.kind === intent.kind &&
    proposal.configurationVersion === intent.version &&
    proposal.sourceRef === intent.sourceRef &&
    proposal.sourceVersion === intent.sourceVersion &&
    proposal.proposedContentSha256 === intent.contentDigest
  );
}
export function rejectedPreparation(
  storage: RecoveryStorage,
  intent: Intent,
): boolean {
  return (
    intent.action === "PREPARE" &&
    storage.getItem(storagePrefix + intent.configurationRef + ".rejected") ===
      intent.key
  );
}
export function markRejectedPreparation(
  storage: RecoveryStorage,
  intent: Intent,
): void {
  if (
    intent.action !== "PREPARE" ||
    loadIntent(storage, intent.configurationRef)?.key !== intent.key
  )
    throw new Error("Write-back rejection scope mismatch");
  storage.setItem(
    storagePrefix + intent.configurationRef + ".rejected",
    intent.key,
  );
}
export function clearRejectedPreparation(
  storage: RecoveryStorage,
  intent: Intent,
): void {
  if (loadIntent(storage, intent.configurationRef)?.key !== intent.key)
    throw new Error("Write-back rejection scope mismatch");
  storage.removeItem(storagePrefix + intent.configurationRef + ".rejected");
}
