import { onScopeDispose, ref } from "vue";
import type {
  ModelDiscoveryModel,
  ModelDiscoverySource,
} from "@/shared/api/generated/openapi/types.gen";
import { loadModelDiscovery } from "./model-discovery-api";
import { ownerInvalidationSignal } from "@/shared/api/owner-lifetime";

export function useModelDiscovery() {
  const query = ref("");
  const items = ref<ModelDiscoveryModel[]>([]);
  const source = ref<ModelDiscoverySource>();
  const total = ref("0");
  const nextPageToken = ref("");
  const loading = ref(false);
  const failed = ref(false);
  const loaded = ref(false);
  let controller: AbortController | undefined;
  let appliedQuery = "";
  let lifetime = ownerInvalidationSignal();

  function invalidated(): void {
    controller?.abort();
    controller = undefined;
    items.value = [];
    source.value = undefined;
    total.value = "0";
    nextPageToken.value = "";
    loaded.value = false;
    failed.value = false;
    loading.value = false;
  }
  lifetime.addEventListener("abort", invalidated, { once: true });

  async function search(more = false): Promise<void> {
    if (lifetime.aborted) {
      lifetime = ownerInvalidationSignal();
      lifetime.addEventListener("abort", invalidated, { once: true });
    }
    if (more && (!nextPageToken.value || loading.value)) return;
    controller?.abort();
    const active = new AbortController();
    controller = active;
    if (!more) {
      items.value = [];
      nextPageToken.value = "";
      source.value = undefined;
      total.value = "0";
      appliedQuery = query.value.trim();
    }
    loading.value = true;
    failed.value = false;
    try {
      const page = await loadModelDiscovery(
        appliedQuery,
        more ? nextPageToken.value : "",
        active.signal,
      );
      if (active.signal.aborted || controller !== active) return;
      items.value = more ? [...items.value, ...page.items] : page.items;
      source.value = page.source;
      total.value = page.total;
      nextPageToken.value = page.nextPageToken;
      loaded.value = true;
    } catch {
      if (!active.signal.aborted && controller === active) failed.value = true;
    } finally {
      if (controller === active) loading.value = false;
    }
  }

  onScopeDispose(() => {
    controller?.abort();
    lifetime.removeEventListener("abort", invalidated);
  });
  return {
    query,
    items,
    source,
    total,
    nextPageToken,
    loading,
    failed,
    loaded,
    search,
  };
}
