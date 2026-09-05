import type { SearchResult } from "@/shared/api/generated/openapi/types.gen";
import { AppProblem } from "@/shared/api/problem";

export function invalidSearchResult(): AppProblem {
  return new AppProblem({
    status: 502,
    code: "INVALID_SEARCH_RESULT",
    retryable: false,
    kind: "unknown",
  });
}

export function isSearchResult(value: unknown): value is SearchResult {
  if (!value || typeof value !== "object") return false;
  const item = value as Partial<SearchResult>;
  return (
    ["PROJECT", "AGENT", "WORKFLOW", "RUN"].includes(item.kind ?? "") &&
    typeof item.ref === "string" &&
    item.ref.length > 0 &&
    typeof item.projectRef === "string" &&
    item.projectRef.length > 0 &&
    typeof item.title === "string" &&
    typeof item.subtitle === "string" &&
    typeof item.state === "string" &&
    typeof item.updatedAt === "string"
  );
}
