import type { SearchResult } from "@/shared/api/generated/openapi/types.gen";
import { runPath } from "@/shared/routes";
import {
  invalidSearchResult,
  isSearchResult,
} from "@/shared/api/search-result";

export const globalSearchDebounceMs = 500;

export function canonicalSearchRoute(result: SearchResult): string {
  if (!isSearchResult(result)) throw invalidSearchResult();
  const project = encodeURIComponent(result.projectRef);
  const reference = encodeURIComponent(result.ref);
  switch (result.kind) {
    case "PROJECT":
      return `/projects/${reference}`;
    case "AGENT":
      return `/projects/${project}/agents/${reference}`;
    case "WORKFLOW":
      return `/projects/${project}/workflows/${reference}`;
    case "RUN":
      return runPath(result.ref, result.projectRef);
  }
}

export class SearchCoordinator {
  private timer: ReturnType<typeof setTimeout> | undefined;

  schedule(term: string, search: (normalized: string) => void): void {
    this.cancel();
    const normalized = term.trim();
    if (normalized.length < 2) return;
    this.timer = globalThis.setTimeout(() => {
      this.timer = undefined;
      search(normalized);
    }, globalSearchDebounceMs);
  }

  cancel(): void {
    if (this.timer !== undefined) globalThis.clearTimeout(this.timer);
    this.timer = undefined;
  }

  flush(term: string, search: (normalized: string) => void): void {
    this.cancel();
    const normalized = term.trim();
    if (normalized.length >= 2) search(normalized);
  }
}
