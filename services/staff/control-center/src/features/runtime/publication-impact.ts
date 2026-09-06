import { getRevisionImpactPlan } from "@/shared/api/generated/openapi/sdk.gen";
import type {
  RevisionImpactPlan,
  RevisionImpactPage,
  RevisionImpactPublicationInput,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { unwrap } from "@/shared/api/problem";

const positive = (value: number) => Number.isSafeInteger(value) && value > 0;
export function publicationPlanIdentity(plan: RevisionImpactPlan): string {
  return JSON.stringify([
    plan.ref,
    plan.kind,
    plan.sourceRef ?? "",
    plan.sourceVersion,
    plan.sourceRevisionRef ?? "",
    plan.draftRef,
    plan.draftVersion,
    plan.targetDigest,
    plan.digest,
    plan.total,
    plan.createdAt,
    plan.expiresAt,
  ]);
}

export function checkedPublicationPlan(
  plan: RevisionImpactPlan,
): RevisionImpactPlan {
  if (
    !plan.ref ||
    !plan.draftRef ||
    !positive(plan.version) ||
    !positive(plan.draftVersion) ||
    !Number.isSafeInteger(plan.sourceVersion) ||
    plan.sourceVersion < 0 ||
    !plan.targetDigest ||
    !plan.digest ||
    !["RUNTIME_ENVIRONMENT", "PROMPT_TEMPLATE", "AGENT_INSTRUCTIONS"].includes(
      plan.kind,
    ) ||
    !["PREPARED", "APPLIED", "EXPIRED"].includes(plan.state) ||
    !Number.isSafeInteger(plan.total) ||
    plan.total < 0 ||
    plan.total > 1000 ||
    !Number.isFinite(Date.parse(plan.createdAt)) ||
    !Number.isFinite(Date.parse(plan.expiresAt)) ||
    (plan.state === "APPLIED" && !plan.publishedRevisionRef)
  )
    throw new Error("Invalid publication impact plan");
  return plan;
}

export function checkedPublicationPage(
  page: RevisionImpactPage,
  expected: RevisionImpactPlan,
  cursor?: string,
): RevisionImpactPage {
  checkedPublicationPlan(page.plan);
  if (
    publicationPlanIdentity(page.plan) !== publicationPlanIdentity(expected) ||
    (cursor && page.plan.state !== expected.state) ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    page.total > page.plan.total ||
    (cursor && page.nextPageToken === cursor) ||
    new Set(page.items.map((item) => item.ref)).size !== page.items.length ||
    page.items.some(
      (item) =>
        !item.ref ||
        !item.consumerRef ||
        !item.bindingRef ||
        !positive(item.consumerVersion) ||
        !positive(item.bindingVersion) ||
        !["AGENT", "AGENT_CONTINUATION", "WORKFLOW", "SCHEDULE"].includes(
          item.consumerKind,
        ) ||
        ![
          "PENDING",
          "APPLIED",
          "CONFLICT",
          "FORBIDDEN",
          "NOT_SELECTED",
        ].includes(item.outcome) ||
        (item.outcome === "APPLIED" &&
          (page.plan.state !== "APPLIED" ||
            item.resultRevisionRef !== page.plan.publishedRevisionRef ||
            item.resultBindingRef !== item.bindingRef ||
            !item.resultBindingVersion ||
            item.resultBindingVersion <= item.bindingVersion ||
            !item.resultConsumerVersion ||
            (page.plan.kind === "PROMPT_TEMPLATE"
              ? item.resultConsumerVersion < item.consumerVersion
              : item.resultConsumerVersion <= item.consumerVersion))) ||
        (item.outcome !== "APPLIED" &&
          !!(
            item.resultRevisionRef ||
            item.resultBindingRef ||
            item.resultBindingVersion ||
            item.resultConsumerVersion
          )),
    )
  )
    throw new Error("Publication impact snapshot mismatch");
  return page;
}

export async function readPublicationImpact(
  plan: RevisionImpactPlan,
  signal: AbortSignal,
  query = "",
  pageToken?: string,
): Promise<RevisionImpactPage> {
  const page = (
    await unwrap(
      getRevisionImpactPlan({
        path: { planRef: plan.ref },
        query: { query: query.trim(), pageSize: 40, pageToken },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  return checkedPublicationPage(page, plan, pageToken);
}

export async function restorePublicationImpact(
  planRef: string,
  signal: AbortSignal,
): Promise<RevisionImpactPage> {
  const page = (
    await unwrap(
      getRevisionImpactPlan({
        path: { planRef },
        query: { pageSize: 40 },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (page.plan.ref !== planRef)
    throw new Error("Publication recovery reference mismatch");
  return checkedPublicationPage(page, checkedPublicationPlan(page.plan));
}

export function publicationSelection(
  plan: RevisionImpactPlan,
  selected: string[],
): RevisionImpactPublicationInput {
  checkedPublicationPlan(plan);
  if (
    plan.state !== "PREPARED" ||
    Date.parse(plan.expiresAt) <= Date.now() ||
    selected.length > 1000 ||
    selected.length > plan.total ||
    new Set(selected).size !== selected.length ||
    selected.some((ref) => !ref)
  )
    throw new Error("Invalid publication impact selection");
  return { planRef: plan.ref, selectedItemRefs: [...selected] };
}
