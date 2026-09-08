import { expect, test } from "vitest";
import { matchesAssistantSearch } from "./ui-readonly-search-proof";

test.each([
  ["GET", "/api/v1/assistant-conversations", false],
  ["GET", "/api/v1/assistant-conversations?query=old", false],
  ["GET", "/api/v1/assistant-conversations?query=exact&pageToken=next", false],
  ["POST", "/api/v1/assistant-conversations?query=exact", false],
  ["GET", "/api/v1/projects?query=exact", false],
  ["GET", "/api/v1/assistant-conversations?query=exact&pageSize=40", true],
])("точный поиск: %s %s", (method, path, expected) => {
  expect(
    matchesAssistantSearch(method, `https://kodex.test${path}`, "exact"),
  ).toBe(expected);
});
