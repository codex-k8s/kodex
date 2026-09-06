import type {
  ProviderAccountBlocker,
  ProviderAccountBlockerKind,
  ProviderAccountBlockerPage,
  ProviderAccountQueuedWorkCancellation,
} from "@/shared/api/generated/openapi/types.gen";
import {
  cancelProviderAccountQueuedWork,
  listProviderAccountBlockers,
} from "@/shared/api/generated/openapi/sdk.gen";
import { requestSignal } from "@/shared/api/client";
import { csrfToken, etag } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import type { ProviderLifecycleAttempt } from "./lifecycle-attempt";
import {
  forgetProviderLifecycleAttempt,
  rememberProviderLifecycleAttempt,
} from "./lifecycle-attempt";
import {
  deleteProviderAccountRecord,
  loadProviderAccount,
  reauthorizeProviderDevice,
  verifyDeviceAuthorization,
} from "./api";
import type { ProviderAccount } from "./model";
import { asProblem } from "@/shared/api/problem";
import { idempotencyKey } from "@/shared/api/mutation";
import {
  checkMutationRejection,
  KnownMutationRejection,
} from "@/shared/api/mutation-rejection";

export type ProviderLifecycleResult = {
  account: ProviderAccount;
  outcomes?: ProviderAccountQueuedWorkCancellation["outcomes"];
};
export type ProviderLifecycleAction =
  | { action: "DELETE" | "VERIFY" | "REAUTHORIZE" }
  | Pick<
      Extract<ProviderLifecycleAttempt, { action: "CANCEL_QUEUED" }>,
      "action" | "body"
    >;

export const providerBlockerKinds: readonly ProviderAccountBlockerKind[] = [
  "AGENT",
  "PROVIDER_POOL",
  "AUTOMATION",
  "ACTIVE_TURN",
  "QUEUED_TURN",
  "WARM_RUNTIME",
];
export function providerBlockerRoute(
  item: ProviderAccountBlocker,
): string | undefined {
  if (item.kind === "ACTIVE_TURN" || item.kind === "QUEUED_TURN")
    return `/runs/${encodeURIComponent(item.ref)}`;
  if (!item.projectRef) return undefined;
  const project = encodeURIComponent(item.projectRef);
  if (item.kind === "AGENT" || item.kind === "PROVIDER_POOL")
    return `/projects/${project}/agents/${encodeURIComponent(item.ref)}?tab=runtime`;
  if (item.kind === "AUTOMATION")
    return `/projects/${project}/automations?scheduleRef=${encodeURIComponent(item.ref)}`;
  return undefined;
}
export async function loadProviderBlockers(
  accountRef: string,
  filter: {
    kind?: ProviderAccountBlockerKind;
    query?: string;
    pageToken?: string;
  },
  signal: AbortSignal,
): Promise<ProviderAccountBlockerPage> {
  return (
    await unwrap(
      listProviderAccountBlockers({
        path: { providerAccountRef: accountRef },
        query: { ...filter, pageSize: 40 },
        signal: requestSignal(signal),
      }),
    )
  ).data;
}
export function checkedProviderBlockerPage(
  page: ProviderAccountBlockerPage,
  accountVersion: number,
  previous?: ProviderAccountBlockerPage,
): void {
  if (
    page.accountVersion !== accountVersion ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    !Number.isSafeInteger(page.hiddenCount) ||
    page.hiddenCount < 0 ||
    !/^[a-f0-9]{64}$/.test(page.contextDigest) ||
    new Set(page.items.map((item) => `${item.kind}:${item.ref}`)).size !==
      page.items.length ||
    (previous &&
      (previous.contextDigest !== page.contextDigest ||
        previous.accountVersion !== page.accountVersion ||
        previous.deletionIntentVersion !== page.deletionIntentVersion ||
        previous.total !== page.total ||
        previous.hiddenCount !== page.hiddenCount ||
        (previous.nextPageToken === page.nextPageToken &&
          !!page.nextPageToken)))
  )
    throw new Error("Provider blocker snapshot changed");
}
export async function cancelProviderQueuedAttempt(
  attempt: Extract<ProviderLifecycleAttempt, { action: "CANCEL_QUEUED" }>,
  signal: AbortSignal,
): Promise<ProviderAccountQueuedWorkCancellation> {
  const result = (
    await unwrap(
      cancelProviderAccountQueuedWork({
        path: { providerAccountRef: attempt.accountRef },
        body: attempt.body,
        headers: {
          "If-Match": etag(attempt.version),
          "Idempotency-Key": attempt.key,
          "X-CSRF-Token": csrfToken(),
        },
        signal: requestSignal(signal),
      }).then(checkMutationRejection),
    )
  ).data;
  if (
    result.account.ref !== attempt.accountRef ||
    result.outcomes.length !== attempt.body.selectedRunRefs.length ||
    result.outcomes.some(
      (item, index) => item.runRef !== attempt.body.selectedRunRefs[index],
    )
  )
    throw new Error("Invalid provider queue cancellation receipt");
  return result;
}

export async function startProviderLifecycle(
  account: ProviderAccount,
  action: ProviderLifecycleAction,
  storage: Storage,
  signal: AbortSignal,
): Promise<ProviderLifecycleResult> {
  if (action.action === "DELETE" && !account.nextActions.includes("DELETE"))
    throw new Error("Provider account deletion is unavailable");
  const attempt: ProviderLifecycleAttempt = {
    accountRef: account.ref,
    version: account.version,
    key: idempotencyKey(),
    ...action,
  };
  rememberProviderLifecycleAttempt(attempt, storage);
  return await performProviderLifecycle(
    attempt,
    account,
    storage,
    signal,
    false,
  );
}
export async function retryProviderLifecycle(
  attempt: ProviderLifecycleAttempt,
  storage: Storage,
  signal: AbortSignal,
): Promise<ProviderLifecycleResult> {
  const current = await loadProviderAccount(attempt.accountRef, signal);
  if (current.ref !== attempt.accountRef)
    throw new Error("Provider lifecycle recovery scope changed");
  if (attempt.action === "CANCEL_QUEUED") {
    const page = await loadProviderBlockers(current.ref, {}, signal);
    checkedProviderBlockerPage(page, current.version);
  }
  rememberProviderLifecycleAttempt(attempt, storage);
  return await performProviderLifecycle(
    attempt,
    current,
    storage,
    signal,
    true,
  );
}
async function performProviderLifecycle(
  attempt: ProviderLifecycleAttempt,
  current: ProviderAccount,
  storage: Storage,
  signal: AbortSignal,
  previousUnknown: boolean,
): Promise<ProviderLifecycleResult> {
  signal.throwIfAborted();
  if (current.ref !== attempt.accountRef)
    throw new Error("Provider lifecycle intent scope changed");
  const original = { ...current, version: attempt.version };
  try {
    let result: ProviderLifecycleResult;
    switch (attempt.action) {
      case "CANCEL_QUEUED":
        result = await cancelProviderQueuedAttempt(attempt, signal);
        break;
      case "DELETE":
        result = {
          account: await deleteProviderAccountRecord(original, attempt.key),
        };
        break;
      case "VERIFY":
        result = {
          account: await verifyDeviceAuthorization(original, attempt.key),
        };
        break;
      case "REAUTHORIZE":
        result = {
          account: await reauthorizeProviderDevice(original, attempt.key),
        };
        break;
    }
    if (result.account.ref !== attempt.accountRef)
      throw new Error("Invalid provider lifecycle receipt scope");
    forgetProviderLifecycleAttempt(attempt.accountRef, storage);
    return result;
  } catch (error) {
    const problem = asProblem(error);
    if (!previousUnknown && error instanceof KnownMutationRejection)
      forgetProviderLifecycleAttempt(attempt.accountRef, storage);
    throw problem;
  }
}
