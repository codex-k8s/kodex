import { ref, onScopeDispose } from "vue";
import {
  listIntegrationApprovalConsents,
  revokeIntegrationApprovalConsent,
} from "@/shared/api/generated/openapi/sdk.gen";
import type { IntegrationApprovalConsent } from "@/shared/api/generated/openapi/types.gen";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";
import { requestSignal } from "@/shared/api/client";
import { mutate, etag } from "@/shared/api/mutation";
import { asProblem, unwrap, type AppProblem } from "@/shared/api/problem";

export function approvalConsentView(
  item: IntegrationApprovalConsent,
): IntegrationApprovalConsent {
  if (
    !/^ias_[a-f0-9]{32}$/.test(item.ref) ||
    !Number.isSafeInteger(item.version) ||
    item.version < 1 ||
    !Number.isSafeInteger(item.maxEffects) ||
    item.maxEffects < 1 ||
    item.maxEffects > 10000 ||
    !Number.isSafeInteger(item.reservedEffects) ||
    item.reservedEffects < 1 ||
    item.reservedEffects > item.maxEffects ||
    !["ACTIVE", "EXPIRED", "EXHAUSTED", "REVOKED"].includes(item.state) ||
    !["ROOT_RUN", "INITIATOR_RESOURCE"].includes(item.lifetime) ||
    !Array.isArray(item.scopePaths) ||
    item.scopePaths.length > 16 ||
    !item.scopePaths.every((p) => typeof p === "string") ||
    typeof item.scopeValuesJSON !== "string" ||
    item.scopeValuesJSON.length > 65536 ||
    !Number.isFinite(Date.parse(item.expiresAt)) ||
    !Number.isFinite(Date.parse(item.createdAt))
  )
    throw new Error("Approval consent response is invalid");
  const values: unknown = JSON.parse(item.scopeValuesJSON);
  if (!Array.isArray(values) || values.length > 16)
    throw new Error("Approval consent object identity is invalid");
  return { ...item, scopePaths: [...item.scopePaths] };
}

// После неизвестного revoke нужен readback: повтор с новым ключом автоматически не выполняется.
export function usePersonalApprovalConsents() {
  const items = ref<IntegrationApprovalConsent[]>([]),
    nextPageToken = ref(""),
    loading = ref(false),
    busyRef = ref(""),
    problem = ref<AppProblem>();
  const owner = ownerInvalidationSignal();
  let controller = new AbortController(),
    generation = 0,
    disposed = false;
  const cursors = new Set<string>();
  function current(epoch: number) {
    return !disposed && !owner.aborted && epoch === generation;
  }
  function suspend() {
    generation++;
    controller.abort();
    items.value = [];
    nextPageToken.value = "";
    loading.value = false;
    busyRef.value = "";
    problem.value = undefined;
    cursors.clear();
  }
  owner.addEventListener("abort", suspend, { once: true });
  onScopeDispose(() => {
    disposed = true;
    suspend();
    owner.removeEventListener("abort", suspend);
  });
  async function load(more = false) {
    if (
      disposed ||
      owner.aborted ||
      busyRef.value ||
      loading.value ||
      (more && !nextPageToken.value)
    )
      return;
    controller.abort();
    controller = new AbortController();
    const epoch = ++generation,
      token = more ? nextPageToken.value : undefined;
    loading.value = true;
    problem.value = undefined;
    try {
      const result = await unwrap(
        listIntegrationApprovalConsents({
          query: { pageSize: 50, ...(token ? { pageToken: token } : {}) },
          signal: requestSignal(AbortSignal.any([owner, controller.signal])),
        }),
      );
      if (!current(epoch)) return;
      const page = result.data;
      if (!Array.isArray(page.items) || page.items.length > 100)
        throw new Error("Approval consent page is invalid");
      const entries = page.items.map(approvalConsentView),
        next = page.nextPageToken ?? "";
      const combined = more ? [...items.value, ...entries] : entries;
      if (
        new Set(combined.map((x) => x.ref)).size !== combined.length ||
        next.length > 512 ||
        (next && (next === token || (more && cursors.has(next))))
      )
        throw new Error("Approval consent cursor is invalid");
      if (!more) cursors.clear();
      if (token) cursors.add(token);
      items.value = combined;
      nextPageToken.value = next;
    } catch (error) {
      if (current(epoch)) problem.value = asProblem(error);
    } finally {
      if (current(epoch)) loading.value = false;
    }
  }
  async function revoke(item: IntegrationApprovalConsent) {
    if (
      disposed ||
      owner.aborted ||
      busyRef.value ||
      loading.value ||
      problem.value ||
      item.state === "REVOKED"
    )
      return;
    const selected = items.value.find(
      (x) => x.ref === item.ref && x.version === item.version,
    );
    if (!selected) return;
    const epoch = ++generation;
    busyRef.value = item.ref;
    problem.value = undefined;
    try {
      const result = await mutate(
        (headers) =>
          revokeIntegrationApprovalConsent({
            path: { consentRef: item.ref },
            body: {},
            headers: { ...headers, "If-Match": etag(item.version) },
            signal: requestSignal(AbortSignal.any([owner, controller.signal])),
          }),
        item.version,
      );
      if (!current(epoch)) return;
      const updated = approvalConsentView(result.data);
      if (
        updated.ref !== item.ref ||
        updated.version !== item.version + 1 ||
        updated.state !== "REVOKED" ||
        updated.connectionRef !== item.connectionRef ||
        updated.capabilityKey !== item.capabilityKey
      )
        throw new Error("Approval consent revocation readback differs");
      items.value = items.value.map((x) => (x.ref === item.ref ? updated : x));
    } catch (error) {
      if (current(epoch)) problem.value = asProblem(error);
    } finally {
      if (current(epoch)) busyRef.value = "";
    }
  }
  return {
    items,
    nextPageToken,
    loading,
    busyRef,
    problem,
    load,
    revoke,
    suspend,
  };
}
