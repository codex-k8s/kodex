import { describe, expect, it } from "vitest";

import { observeHTTPResponse } from "./fixtures";

const mutationKey = "11111111-1111-4111-8111-111111111111";
const endpoint = "https://control.kodex.works/api/v1/example";

describe("browser diagnostics HTTP recovery", () => {
  it("закрывает 503 только подтверждённым успехом той же mutation", () => {
    const pending = new Map<string, string>();

    expect(
      observeHTTPResponse(pending, 503, "POST", endpoint, mutationKey),
    ).toEqual({});
    expect(pending.size).toBe(1);
    expect(
      observeHTTPResponse(pending, 200, "POST", endpoint, mutationKey),
    ).toEqual({
      recovered: `response:503:POST:${endpoint}`,
    });
    expect(pending.size).toBe(0);
  });

  it("сохраняет незавершённый 5xx и не связывает другой mutation key", () => {
    const pending = new Map<string, string>();
    observeHTTPResponse(pending, 503, "POST", endpoint, mutationKey);

    expect(
      observeHTTPResponse(
        pending,
        200,
        "POST",
        endpoint,
        "22222222-2222-4222-8222-222222222222",
      ),
    ).toEqual({});
    expect([...pending.values()]).toEqual([`response:503:POST:${endpoint}`]);
  });

  it("немедленно фиксирует 5xx без idempotency key", () => {
    const pending = new Map<string, string>();

    expect(observeHTTPResponse(pending, 503, "GET", endpoint)).toEqual({
      failure: `response:503:GET:${endpoint}`,
    });
    expect(pending.size).toBe(0);
  });
});
