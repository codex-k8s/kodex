import {
  prepareRoleImageImpactPlan,
  getRoleImageImpactPlan,
  rebindRoleImageConsumers,
} from "@/shared/api/generated/openapi/sdk.gen";
import type {
  ManagedConfiguration,
  ManagedConfigurationRevision,
  RoleImageImpactPlan,
  RoleImageImpactPage,
} from "@/shared/api/generated/openapi/types.gen";
import { requestSignal } from "@/shared/api/client";
import { etag, mutate } from "@/shared/api/mutation";
import { unwrap } from "@/shared/api/problem";

export function roleImagePlanIdentity(plan: RoleImageImpactPlan): string {
  return JSON.stringify([
    plan.ref,
    plan.configurationRef,
    plan.configurationVersion,
    plan.revisionRef,
    plan.revisionDigest,
    plan.recipeRef,
    plan.recipeGeneration,
    plan.buildRef,
    plan.artifactRef,
    plan.artifactDigest,
    plan.admissionPolicyDigest,
    plan.digest,
    plan.total,
    plan.createdAt,
    plan.expiresAt,
  ]);
}
function checkedPlan(plan: RoleImageImpactPlan): RoleImageImpactPlan {
  if (
    [
      plan.ref,
      plan.configurationRef,
      plan.revisionRef,
      plan.revisionDigest,
      plan.recipeRef,
      plan.buildRef,
      plan.artifactRef,
      plan.artifactDigest,
      plan.admissionPolicyDigest,
      plan.digest,
    ].some((value) => !value) ||
    [plan.version, plan.configurationVersion, plan.recipeGeneration].some(
      (value) => !Number.isSafeInteger(value) || value <= 0,
    ) ||
    !Number.isSafeInteger(plan.total) ||
    plan.total < 0 ||
    plan.total > 1000 ||
    !["PREPARED", "APPLIED", "EXPIRED"].includes(plan.state) ||
    !Number.isFinite(Date.parse(plan.expiresAt))
  )
    throw new Error("Invalid role image impact plan");
  return plan;
}
export async function prepareImageImpact(
  configuration: ManagedConfiguration,
  revision: ManagedConfigurationRevision,
): Promise<RoleImageImpactPlan> {
  if (configuration.kind !== "ROLE_IMAGE" || revision.state !== "PUBLISHED")
    throw new Error("Role image impact requires a published revision");
  const plan = checkedPlan(
    (
      await mutate(
        (headers) =>
          prepareRoleImageImpactPlan({
            path: {
              configurationRef: configuration.ref,
              revisionRef: revision.ref,
            },
            headers: { ...headers, "If-Match": etag(configuration.version) },
            signal: requestSignal(),
          }),
        configuration.version,
      )
    ).data,
  );
  if (
    plan.configurationRef !== configuration.ref ||
    plan.configurationVersion !== configuration.version ||
    plan.revisionRef !== revision.ref ||
    plan.revisionDigest !== revision.digest ||
    plan.state !== "PREPARED"
  )
    throw new Error("Role image impact preparation mismatch");
  return plan;
}
export function checkedImagePage(
  page: RoleImageImpactPage,
  expected: RoleImageImpactPlan,
  cursor?: string,
): RoleImageImpactPage {
  checkedPlan(page.plan);
  if (
    roleImagePlanIdentity(page.plan) !== roleImagePlanIdentity(expected) ||
    (cursor && page.plan.state !== expected.state) ||
    !Number.isSafeInteger(page.total) ||
    page.total < page.items.length ||
    page.total > page.plan.total ||
    (cursor && page.nextPageToken === cursor) ||
    new Set(page.items.map((item) => item.ref)).size !== page.items.length ||
    page.items.some(
      (item) =>
        !item.ref ||
        !item.environmentRef ||
        !item.projectRef ||
        !item.sourceVersionRef ||
        !item.sourceVersionDigest ||
        !Number.isSafeInteger(item.environmentVersion) ||
        item.environmentVersion <= 0 ||
        (item.consumer &&
          (item.consumer.projectRef !== item.projectRef ||
            item.consumer.versionRef !== item.sourceVersionRef)) ||
        ![
          "PENDING",
          "APPLIED",
          "CONFLICT",
          "FORBIDDEN",
          "NOT_SELECTED",
        ].includes(item.outcome) ||
        (item.outcome === "APPLIED" &&
          (page.plan.state !== "APPLIED" ||
            !item.resultEnvironmentVersionRef ||
            (item.consumer &&
              (item.resultBindingRef !== item.consumer.bindingRef ||
                !item.resultBindingVersion ||
                item.resultBindingVersion <= item.consumer.bindingVersion)))) ||
        (item.outcome !== "APPLIED" &&
          !!(
            item.resultEnvironmentVersionRef ||
            item.resultBindingRef ||
            item.resultBindingVersion
          )),
    )
  )
    throw new Error("Role image impact snapshot mismatch");
  return page;
}
export async function readImageImpact(
  plan: RoleImageImpactPlan,
  signal: AbortSignal,
  query = "",
  pageToken?: string,
): Promise<RoleImageImpactPage> {
  return checkedImagePage(
    (
      await unwrap(
        getRoleImageImpactPlan({
          path: { planRef: plan.ref },
          query: { query: query.trim(), pageSize: 40, pageToken },
          signal: requestSignal(signal),
          cache: "no-store",
        }),
      )
    ).data,
    plan,
    pageToken,
  );
}
export async function restoreImageImpact(
  planRef: string,
  signal: AbortSignal,
): Promise<RoleImageImpactPage> {
  const page = (
    await unwrap(
      getRoleImageImpactPlan({
        path: { planRef },
        query: { pageSize: 40 },
        signal: requestSignal(signal),
        cache: "no-store",
      }),
    )
  ).data;
  if (page.plan.ref !== planRef)
    throw new Error("Role image recovery reference mismatch");
  return checkedImagePage(page, checkedPlan(page.plan));
}
export function imageImpactSelection(
  plan: RoleImageImpactPlan,
  selectedItemRefs: string[],
) {
  checkedPlan(plan);
  if (
    plan.state !== "PREPARED" ||
    Date.parse(plan.expiresAt) <= Date.now() ||
    selectedItemRefs.length > plan.total ||
    selectedItemRefs.length > 1000 ||
    new Set(selectedItemRefs).size !== selectedItemRefs.length ||
    selectedItemRefs.some((ref) => !ref)
  )
    throw new Error("Invalid role image impact selection");
  return {
    planRef: plan.ref,
    impactDigest: plan.digest,
    selectedItemRefs: [...selectedItemRefs],
  };
}
export async function applyImageImpact(
  plan: RoleImageImpactPlan,
  selectedItemRefs: string[],
  key: string,
) {
  const input = imageImpactSelection(plan, selectedItemRefs);
  const result = (
    await mutate(
      (headers) =>
        rebindRoleImageConsumers({
          path: {
            configurationRef: plan.configurationRef,
            revisionRef: plan.revisionRef,
          },
          body: input,
          headers: { ...headers, "If-Match": etag(plan.configurationVersion) },
          signal: requestSignal(),
        }),
      plan.configurationVersion,
      key,
    )
  ).data;
  checkedPlan(result.plan);
  if (
    roleImagePlanIdentity(result.plan) !== roleImagePlanIdentity(plan) ||
    result.plan.state !== "APPLIED" ||
    result.configuration.ref !== plan.configurationRef ||
    result.configuration.version <= plan.configurationVersion ||
    result.revision.ref !== plan.revisionRef
  )
    throw new Error("Role image impact receipt mismatch");
  return result;
}
