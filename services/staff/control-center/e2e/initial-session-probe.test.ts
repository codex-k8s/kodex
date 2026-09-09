import { describe, expect, test } from "vitest";
import { InitialSessionProbeObservation } from "./initial-session-probe";

const origin = "https://kodex.test";
function request(
  url = `${origin}/api/v1/session`,
  method = "GET",
  type = "fetch",
) {
  return { url: () => url, method: () => method, resourceType: () => type };
}

describe("точное происхождение начального session probe", () => {
  test("запоздалый 401 до входа связан с тем же Request, а не URL", () => {
    const observation = new InitialSessionProbeObservation();
    const before = request();
    observation.observe(before);
    observation.startTransition();
    const after = request();
    observation.observe(after);
    expect(observation.acceptInitial401(before, 401, origin)).toBe(true);
    expect(observation.acceptInitial401(after, 401, origin)).toBe(false);
    expect(observation.acceptInitial401(request(), 401, origin)).toBe(false);
    observation.startTransition();
    observation.observe(after);
    expect(observation.acceptInitial401(after, 401, origin)).toBe(false);
    expect(observation.observed401s).toBe(1);
  });

  test("наблюдение до начала перехода не классифицируется как перекрытие", () => {
    const observation = new InitialSessionProbeObservation();
    const probe = request();
    observation.observe(probe);
    expect(observation.acceptInitial401(probe, 401, origin)).toBe(false);
  });

  test.each([
    [403, `${origin}/api/v1/session`, "GET", "fetch"],
    [503, `${origin}/api/v1/session`, "GET", "fetch"],
    [401, `${origin}/api/v1/session/callback`, "POST", "fetch"],
    [401, `${origin}/api/v1/session/authorization`, "POST", "fetch"],
    [401, `${origin}/api/v1/session`, "DELETE", "fetch"],
    [401, `${origin}/api/v1/session`, "GET", "document"],
    [401, "https://identity.test/api/v1/session", "GET", "fetch"],
    [401, `${origin}/api/v1/session?opaque=fixture`, "GET", "fetch"],
    [401, `${origin}/api/v1/session#fixture`, "GET", "fetch"],
    [401, "invalid-url", "GET", "fetch"],
  ] as const)("не скрывает соседний отказ %#", (status, url, method, type) => {
    const observation = new InitialSessionProbeObservation();
    const probe = request(url, method, type);
    observation.observe(probe);
    observation.startTransition();
    expect(observation.acceptInitial401(probe, status, origin)).toBe(false);
    expect(observation.observed401s).toBe(0);
  });
});
