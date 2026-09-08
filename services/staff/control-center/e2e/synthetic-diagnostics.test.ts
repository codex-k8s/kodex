import { describe, expect, it } from "vitest";
import {
  browserHTTPConsoleStatus,
  expectedSyntheticHTTPFailure,
  isFirefoxScrollAdvisory,
  isWebKitFontAdvisory,
  isConfirmedSyntheticCancellation,
} from "./synthetic-diagnostics";
import config from "../playwright.synthetic.config";
import viteConfig from "../vite.synthetic.config";

describe("synthetic browser evidence", () => {
  it("требует точный browser code и подтверждённую отмену поколения", () => {
    for (const [browser, code] of [
      ["chromium", "net::ERR_ABORTED"],
      ["firefox", "NS_BINDING_ABORTED"],
      ["webkit", "Load request cancelled"],
    ] as const) {
      expect(isConfirmedSyntheticCancellation(browser, code, true)).toBe(true);
      expect(isConfirmedSyntheticCancellation(browser, code, false)).toBe(
        false,
      );
      expect(
        isConfirmedSyntheticCancellation(browser, "NETWORK_ERROR", true),
      ).toBe(false);
    }
    expect(
      isConfirmedSyntheticCancellation("firefox", "net::ERR_ABORTED", true),
    ).toBe(false);
  });
  it("сохраняет точную границу WebKit preload advisory", () => {
    const message =
      "The resource https://kodex.test/fonts/ibm-plex-sans-latin-400-normal.woff2 was preloaded using link preload but not used within a few seconds from the window's load event. Please make sure it wasn't preloaded for nothing.";
    expect(isWebKitFontAdvisory("webkit", message)).toBe(true);
    expect(isWebKitFontAdvisory("firefox", message)).toBe(false);
    expect(
      isWebKitFontAdvisory(
        "webkit",
        message.replace("kodex.test", "other.test"),
      ),
    ).toBe(false);
    expect(isWebKitFontAdvisory("webkit", message + " Unknown failure")).toBe(
      false,
    );
  });

  it("выбирает engine явно и не передаёт Chromium flags соседям", () => {
    expect(config.projects?.map((project) => project.use?.browserName)).toEqual(
      ["chromium", "firefox", "webkit"],
    );
    expect(
      config.projects
        ?.slice(1)
        .every((project) => !project.use?.launchOptions?.args),
    ).toBe(true);
    expect(config.projects?.[2]?.metadata?.voiceRecorder).toBe("fixture");
    expect(viteConfig).toMatchObject({
      preview: { allowedHosts: ["kodex.test"] },
    });
  });
  it("считает HTTP отказы только в точном synthetic scope", () => {
    const address =
      "https://kodex.test/api/v1/runtime-environments/environment_synthetic/readiness";
    expect(expectedSyntheticHTTPFailure(address, 503, 503)).toBe("inspector");
    expect(expectedSyntheticHTTPFailure(address, 503)).toBeUndefined();
    expect(
      expectedSyntheticHTTPFailure(
        address.replace("kodex.test", "other.test"),
        503,
        503,
      ),
    ).toBeUndefined();
    expect(
      expectedSyntheticHTTPFailure(
        address.replace("environment_synthetic", "other"),
        503,
        503,
      ),
    ).toBeUndefined();
    expect(expectedSyntheticHTTPFailure(address, 500, 500)).toBeUndefined();
    expect(
      expectedSyntheticHTTPFailure(
        "https://kodex.test/api/v1/runs?resumableSessionsOnly=true&pageToken=session-snapshot",
        412,
      ),
    ).toBe("snapshot");
    expect(
      expectedSyntheticHTTPFailure(
        "https://kodex.test/api/v1/runs?resumableSessionsOnly=true",
        412,
      ),
    ).toBeUndefined();
    expect(
      browserHTTPConsoleStatus(
        "Failed to load resource: the server responded with a status of 503 (Service Unavailable)",
      ),
    ).toBe(503);
    expect(browserHTTPConsoleStatus("Application failure 503")).toBeUndefined();
  });
  it("не маскирует ошибки приложения под native advisory", () => {
    const advisory =
      '[JavaScript Warning: "This site appears to use a scroll-linked positioning effect. This may not work well with asynchronous panning; see https://firefox-source-docs.mozilla.org/performance/scroll-linked_effects.html for further details and to join the discussion on related tools and features!" {file: "https://kodex.test/e2e/fixtures/runtime-detail.html" line: 0}]';
    expect(isFirefoxScrollAdvisory("firefox", advisory)).toBe(true);
    expect(isFirefoxScrollAdvisory("chromium", advisory)).toBe(false);
    expect(
      isFirefoxScrollAdvisory("firefox", advisory + " unknown warning"),
    ).toBe(false);
    expect(
      isFirefoxScrollAdvisory("firefox", "Unknown application warning"),
    ).toBe(false);
  });
});
