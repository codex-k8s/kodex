import { describe, expect, it } from "vitest";
import type { OwnerSessionMetadata } from "@/shared/api/generated/openapi/types.gen";
import {
  authorizationCallback,
  authorizationRedirect,
  browserSessionIdentity,
  browserSessionTiming,
} from "./browser-session";

const metadata: OwnerSessionMetadata = {
  generation: "11111111-1111-4111-8111-111111111111",
  version: 2,
  sessionRevision: 7,
  serverTime: "2026-09-06T10:00:00Z",
  expiresAt: "2026-09-06T10:10:00Z",
  accessExpiresAt: "2026-09-06T10:05:00Z",
  absoluteExpiresAt: "2026-09-06T11:00:00Z",
  renewAfter: "2026-09-06T10:04:30Z",
  renewalMode: "BACKEND_REFRESH",
};
describe("BFF browser boundary", () => {
  it("считает сроки по serverTime с учётом задержки, не по локальному UTC", () => {
    expect(browserSessionTiming(metadata, 1000, 100)).toEqual({
      deadline: 600900,
      renewAt: 270900,
    });
    expect(
      browserSessionTiming(
        { ...metadata, renewalMode: "REAUTHENTICATION" },
        1000,
      ),
    ).toEqual({ deadline: 301000, renewAt: 301000 });
  });
  it("не выдаёт sessionRevision за refresh version", () => {
    expect(browserSessionIdentity(metadata)).not.toBe(
      browserSessionIdentity({ ...metadata, version: 3 }),
    );
    expect(browserSessionIdentity(metadata)).toBe(
      browserSessionIdentity({ ...metadata, sessionRevision: 8 }),
    );
  });
  it("закрыто отклоняет истёкший срок и malformed metadata", () => {
    expect(() =>
      browserSessionTiming({ ...metadata, expiresAt: metadata.serverTime }),
    ).toThrow();
    expect(() => browserSessionTiming({ ...metadata, version: NaN })).toThrow();
    expect(() =>
      browserSessionTiming({ ...metadata, renewAfter: "bad" }),
    ).toThrow();
  });
  it("принимает только HTTPS переход к настроенному issuer", () => {
    expect(
      authorizationRedirect(
        "https://identity.test/auth?state=abc",
        "https://identity.test/realm",
      ),
    ).toContain("identity.test");
    for (const url of [
      "javascript:alert(1)",
      "https://foreign.test/auth",
      "http://identity.test/auth",
      "https://user@identity.test/auth",
    ])
      expect(() =>
        authorizationRedirect(url, "https://identity.test/realm"),
      ).toThrow();
  });
  it("не принимает повторённые callback параметры и token fragment", () => {
    const callback = `https://kodex.test/auth/callback?code=one&state=${"a".repeat(43)}`;
    expect(authorizationCallback(new URL(callback))).toEqual({
      code: "one",
      state: "a".repeat(43),
    });
    for (const suffix of [
      "&code=two",
      "&state=second",
      "#access_token=not-allowed",
      "&error=access_denied",
    ])
      expect(() => authorizationCallback(new URL(callback + suffix))).toThrow();
  });
});
