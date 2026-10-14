import { ref, shallowRef } from "vue";
import {
  downloadRevisionContent,
  loadArtifactRevisionPage,
} from "@/shared/api/artifact-revisions";
import type {
  Artifact,
  ArtifactRevision,
} from "@/shared/api/generated/openapi/types.gen";
import { ownerRequestSignal } from "@/shared/api/owner-lifetime";
import { asProblem, type AppProblem } from "@/shared/api/problem";

export function createRevisionHistory() {
  const items = shallowRef<ArtifactRevision[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const nextPageToken = ref<string>();
  const error = shallowRef<AppProblem>();
  const downloading = ref("");
  let artifact: Artifact | undefined;
  let controller = new AbortController();
  let scope = controller.signal;
  let generation = 0;

  function clear() {
    generation++;
    items.value = [];
    nextPageToken.value = undefined;
    error.value = undefined;
    loading.value = false;
    loaded.value = false;
    downloading.value = "";
  }

  function reset(value?: Artifact) {
    scope.removeEventListener("abort", clear);
    controller.abort();
    clear();
    artifact = value;
    controller = new AbortController();
    scope = AbortSignal.any([controller.signal, ownerRequestSignal()]);
    scope.addEventListener("abort", clear, { once: true });
  }

  async function load(more = false) {
    if (
      !artifact ||
      scope.aborted ||
      loading.value ||
      (more && !nextPageToken.value)
    )
      return;
    const current = generation;
    const signal = scope;
    const cursor = more ? nextPageToken.value : undefined;
    loading.value = true;
    error.value = undefined;
    try {
      const page = await loadArtifactRevisionPage(artifact.ref, cursor, signal);
      if (signal.aborted || current !== generation) return;
      const previous = items.value.at(-1);
      if (
        more &&
        (page.nextPageToken === cursor ||
          page.items.some((item) =>
            items.value.some(
              (previous) =>
                previous.ref === item.ref ||
                previous.revision === item.revision,
            ),
          ) ||
          (page.items[0] &&
            previous &&
            page.items[0].revision >= previous.revision))
      )
        throw new Error("Artifact revision history cursor mismatch");
      items.value = more ? [...items.value, ...page.items] : page.items;
      nextPageToken.value = page.nextPageToken;
      loaded.value = true;
    } catch (cause) {
      if (signal.aborted || current !== generation) return;
      error.value = asProblem(cause);
      if (
        ["forbidden", "unauthorized", "not-found", "conflict"].includes(
          error.value.kind,
        )
      ) {
        items.value = [];
        nextPageToken.value = undefined;
        loaded.value = false;
      }
    } finally {
      if (!signal.aborted && current === generation) loading.value = false;
    }
  }

  async function download(value: ArtifactRevision): Promise<Blob | undefined> {
    if (
      !artifact ||
      scope.aborted ||
      downloading.value ||
      !artifact.nextActions.includes("DOWNLOAD") ||
      value.scanState !== "CLEAN" ||
      !items.value.some(
        (item) => item.ref === value.ref && item.digest === value.digest,
      )
    )
      return;
    const current = generation;
    const signal = scope;
    downloading.value = value.ref;
    error.value = undefined;
    try {
      const body = await downloadRevisionContent(value, "DOWNLOAD", signal);
      if (signal.aborted || current !== generation) return;
      return body;
    } catch (cause) {
      if (!signal.aborted && current === generation)
        error.value = asProblem(cause);
    } finally {
      if (!signal.aborted && current === generation) downloading.value = "";
    }
  }

  return {
    items,
    loading,
    loaded,
    nextPageToken,
    error,
    downloading,
    reset,
    load,
    download,
    dispose: () => {
      scope.removeEventListener("abort", clear);
      controller.abort();
      clear();
      artifact = undefined;
    },
  };
}
