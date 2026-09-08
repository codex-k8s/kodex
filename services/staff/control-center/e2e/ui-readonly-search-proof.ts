export function matchesAssistantSearch(
  method: string,
  url: string,
  query: string,
) {
  const parsed = new URL(url);
  return (
    method === "GET" &&
    parsed.pathname === "/api/v1/assistant-conversations" &&
    parsed.searchParams.get("query") === query &&
    !parsed.searchParams.get("pageToken")
  );
}
