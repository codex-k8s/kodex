import { requestSignal } from "@/shared/api/client";
import {
  prepareRuntimeSecretDraftImpact,
  getRuntimeSecretDraftImpact,
  publishRuntimeSecretDraft,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RuntimeSecretDraftImpactPlan,
  RuntimeSecretDraftImpactPage,
  RuntimeSecretDraftPublication,
} from "@/shared/api/generated/openapi/types.gen";
import { mutate, type MutationHeaders } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";
import { checkedDraft, type RuntimeSecretDraft } from "./draft-api";
import { normalizeSecretPage } from "./model";

export type { RuntimeSecretDraftImpactPlan, RuntimeSecretDraftImpactPage };
const positive = (value: number) => Number.isSafeInteger(value) && value > 0;
function headers(value: MutationHeaders) {
  if (!value["If-Match"]) throw new Error("Draft version is unavailable");
  return { ...value, "If-Match": value["If-Match"] };
}

export function checkedPlan(
  plan: RuntimeSecretDraftImpactPlan,
  draft: RuntimeSecretDraft,
): RuntimeSecretDraftImpactPlan {
  if (
    !plan.ref ||
    plan.draftRef !== draft.ref ||
    plan.secretRef !== draft.secretRef ||
    plan.draftVersion !== draft.version ||
    plan.secretVersion !== draft.secretVersion ||
    !positive(plan.draftVersion) ||
    !positive(plan.secretVersion) ||
    !Number.isSafeInteger(plan.sourceRevision) ||
    plan.sourceRevision < 0 ||
    !plan.digest ||
    !Number.isSafeInteger(plan.total) ||
    plan.total < 0 ||
    plan.total > 1000 ||
    !Number.isFinite(Date.parse(plan.expiresAt)) ||
    !["PREPARED", "APPLIED", "EXPIRED", "CANCELLED"].includes(plan.state)
  )
    throw new Error("Secret draft impact plan mismatch");
  return plan;
}

export async function prepareDraftImpact(
  draft: RuntimeSecretDraft,
  key: string,
): Promise<RuntimeSecretDraftImpactPlan> {
  const result = await mutate(
    (value) =>
      prepareRuntimeSecretDraftImpact({
        path: { draftRef: draft.ref },
        headers: headers(value),
        signal: requestSignal(),
      }),
    draft.version,
    key,
  );
  return checkedPlan(result.data, draft);
}

export async function readDraftImpact(
  plan: RuntimeSecretDraftImpactPlan,
  signal: AbortSignal,
  query = "",
  pageToken?: string,
): Promise<RuntimeSecretDraftImpactPage> {
  const result = (
    await unwrap(
      getRuntimeSecretDraftImpact({
        path: { planRef: plan.ref },
        query: {
          pageSize: 40,
          ...(query.trim() ? { query: query.trim() } : {}),
          ...(pageToken ? { pageToken } : {}),
        },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  return checkedImpactPage(result, plan, pageToken);
}

export async function restoreDraftImpact(
  draft: RuntimeSecretDraft,
  planRef: string,
  signal: AbortSignal,
): Promise<RuntimeSecretDraftImpactPage> {
  const result = (
    await unwrap(
      getRuntimeSecretDraftImpact({
        path: { planRef },
        query: { pageSize: 40 },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (result.plan.ref !== planRef)
    throw new Error("Secret draft impact reference mismatch");
  // Публикация продвигает текущий Draft; план сохраняет исходные immutable pins.
  checkedPlan(result.plan, {
    ...draft,
    version: result.plan.draftVersion,
    secretVersion: result.plan.secretVersion,
  });
  return checkedImpactPage(result, result.plan);
}

function checkedImpactPage(
  result: RuntimeSecretDraftImpactPage,
  plan: RuntimeSecretDraftImpactPlan,
  pageToken?: string,
): RuntimeSecretDraftImpactPage {
  if (
    result.plan.ref !== plan.ref ||
    result.plan.digest !== plan.digest ||
    result.plan.draftRef !== plan.draftRef ||
    result.plan.draftVersion !== plan.draftVersion ||
    result.plan.secretRef !== plan.secretRef ||
    result.plan.secretVersion !== plan.secretVersion ||
    result.plan.total !== plan.total ||
    result.plan.sourceRevision !== plan.sourceRevision ||
    !["PREPARED", "APPLIED", "EXPIRED", "CANCELLED"].includes(
      result.plan.state,
    ) ||
    !Array.isArray(result.items) ||
    !Number.isSafeInteger(result.total) ||
    result.total < result.items.length ||
    result.total > plan.total ||
    typeof result.nextPageToken !== "string" ||
    (pageToken && result.nextPageToken === pageToken) ||
    new Set(result.items.map((item) => item.ref)).size !==
      result.items.length ||
    result.items.some((item) => {
      const row = item.consumer;
      const agent = row.consumer;
      return (
        !item.ref ||
        !row.environmentRef ||
        !row.environmentVersionRef ||
        !row.projectRef ||
        !positive(row.environmentVersion) ||
        !Array.isArray(row.secretRevisions) ||
        row.secretRevisions.some((revision) => !positive(revision)) ||
        (!!agent &&
          (!agent.agentRef ||
            !agent.bindingRef ||
            !agent.versionRef ||
            !positive(agent.agentVersion) ||
            !positive(agent.bindingVersion) ||
            agent.projectRef !== row.projectRef)) ||
        ![
          "PENDING",
          "APPLIED",
          "CONFLICT",
          "FORBIDDEN",
          "NOT_SELECTED",
        ].includes(item.outcome) ||
        (result.plan.state !== "APPLIED" &&
          !["PENDING", "NOT_SELECTED"].includes(item.outcome)) ||
        (item.outcome === "APPLIED" &&
          (!item.resultEnvironmentVersionRef ||
            (!!agent &&
              (!item.resultBindingRef ||
                !positive(item.resultBindingVersion ?? 0)))))
      );
    })
  )
    throw new Error("Secret draft impact page mismatch");
  return result;
}

export async function publishSecretDraft(
  draft: RuntimeSecretDraft,
  plan: RuntimeSecretDraftImpactPlan,
  selectedItemRefs: string[],
  key: string,
): Promise<RuntimeSecretDraftPublication> {
  checkedPlan(plan, draft);
  if (
    plan.state !== "PREPARED" ||
    selectedItemRefs.length > 1000 ||
    new Set(selectedItemRefs).size !== selectedItemRefs.length
  )
    throw new Error("Secret draft publication selection is invalid");
  const result = (
    await mutate(
      (value) =>
        publishRuntimeSecretDraft({
          path: { draftRef: draft.ref },
          headers: headers(value),
          body: {
            expectedSecretVersion: draft.secretVersion,
            impactPlanRef: plan.ref,
            selectedItemRefs,
          },
          signal: requestSignal(),
        }),
      draft.version,
      key,
    )
  ).data;
  const receipt = checkedDraft(result.draft, draft.projectRef, draft);
  const secret = normalizeSecretPage({ items: [result.secret] }).items[0];
  if (
    receipt.state !== "PUBLISHED" ||
    !secret ||
    secret.ref !== draft.secretRef ||
    secret.projectRef !== draft.projectRef ||
    secret.currentRevision !== receipt.publishedRevision
  )
    throw new Error("Secret draft publication receipt mismatch");
  return { draft: receipt, secret };
}
