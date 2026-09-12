import { describe, expect, it } from "vitest";
import {
  browserHTTPConsoleStatus,
  expectedSyntheticHTTPFailure,
  isFirefoxBounceTrackerAdvisory,
  isFirefoxAvailabilityBodyAbortAdvisory,
  isFirefoxScrollAdvisory,
  isWebKitFontAdvisory,
  isConfirmedSyntheticCancellation,
  matchesConfirmedFirefoxAvailabilityBodyAborts,
  isCompletedChromiumTicketTerminal,
  validFirefoxBounceTrackerAdvisoryCount,
} from "./synthetic-diagnostics";
import config from "../playwright.synthetic.config";
import viteConfig from "../vite.synthetic.config";

describe("synthetic browser evidence", () => {
  it("отсутствие advisory допустимо, неизвестный браузер и превышение бюджета закрыты", () => {
    expect(validFirefoxBounceTrackerAdvisoryCount("firefox", 0)).toBe(true);
    expect(validFirefoxBounceTrackerAdvisoryCount("firefox", 1)).toBe(true);
    expect(validFirefoxBounceTrackerAdvisoryCount("chromium", 0)).toBe(true);
    for (const count of [-1, 0.5, 2, NaN, Infinity])
      expect(validFirefoxBounceTrackerAdvisoryCount("firefox", count)).toBe(
        false,
      );
    expect(validFirefoxBounceTrackerAdvisoryCount("webkit", 1)).toBe(false);
  });
  it("классифицирует только exact Firefox availability body abort advisory", () => {
    const source = "https://kodex.test/assets/availability-BwI46JYI.js";
    const message = `[JavaScript Error: "Failed to read data from the ReadableStream: “AbortError: The operation was aborted. ”." {file: "${source}" line: 1}]`;
    const exact = (
      browser = "firefox",
      type = "error",
      selectedSource = source,
      line = 1,
      column = 2700,
      text = message,
    ) =>
      isFirefoxAvailabilityBodyAbortAdvisory(
        browser,
        type,
        selectedSource,
        line,
        column,
        text,
      );
    expect(exact()).toBe(true);
    expect(exact("chromium")).toBe(false);
    expect(exact("webkit")).toBe(false);
    expect(exact("firefox", "warning")).toBe(false);
    expect(exact("firefox", "pageerror")).toBe(false);
    expect(exact("firefox", "error", source, 0)).toBe(false);
    expect(exact("firefox", "error", source, 1, 0)).toBe(false);
    expect(
      exact("firefox", "error", source.replace("kodex.test", "other.test")),
    ).toBe(false);
    expect(
      exact("firefox", "error", "https://kodex.test/api/v1/bootstrap"),
    ).toBe(false);
    expect(
      exact("firefox", "error", source.replace("/assets/", "/scripts/")),
    ).toBe(false);
    expect(exact("firefox", "error", source, 1, 2700, `${message} extra`)).toBe(
      false,
    );
    expect(
      exact(
        "firefox",
        "error",
        source,
        1,
        2700,
        message.replace("AbortError", "TypeError"),
      ),
    ).toBe(false);
    expect(matchesConfirmedFirefoxAvailabilityBodyAborts("firefox", 5, 5)).toBe(
      true,
    );
    expect(matchesConfirmedFirefoxAvailabilityBodyAborts("firefox", 4, 5)).toBe(
      false,
    );
    expect(matchesConfirmedFirefoxAvailabilityBodyAborts("firefox", 6, 5)).toBe(
      false,
    );
    expect(
      matchesConfirmedFirefoxAvailabilityBodyAborts("chromium", 1, 1),
    ).toBe(false);
    expect(
      matchesConfirmedFirefoxAvailabilityBodyAborts("firefox", -1, -1),
    ).toBe(false);
  });

  it("закрыто классифицирует terminal только после полного Chromium ticket body", () => {
    const exact = (
      browserName = "chromium",
      code = "net::ERR_ABORTED",
      method = "POST",
      resourceType = "fetch",
      pathname = "/api/v1/session/ticket",
      bodyCompleted = true,
    ) =>
      isCompletedChromiumTicketTerminal(
        browserName,
        code,
        method,
        resourceType,
        pathname,
        bodyCompleted,
      );

    expect(exact()).toBe(true);
    expect(exact("firefox")).toBe(false);
    expect(exact("webkit")).toBe(false);
    expect(exact("chromium", "net::ERR_FAILED")).toBe(false);
    expect(exact("chromium", "net::ERR_ABORTED", "GET")).toBe(false);
    expect(exact("chromium", "net::ERR_ABORTED", "POST", "xhr")).toBe(false);
    expect(
      exact(
        "chromium",
        "net::ERR_ABORTED",
        "POST",
        "fetch",
        "/api/v1/session/ticket/other",
      ),
    ).toBe(false);
    expect(
      exact(
        "chromium",
        "net::ERR_ABORTED",
        "POST",
        "fetch",
        "/api/v1/session/ticket",
        false,
      ),
    ).toBe(false);
  });

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
  it("классифицирует только точный Firefox bounce tracker advisory", () => {
    const message =
      '[JavaScript Warning: "“identity.invalid” has been classified as a bounce tracker. If it does not receive user activation within the next 3,600 seconds it will have its state purged."]';
    const exact = (
      browser = "firefox",
      type = "warning",
      source = "",
      line = 0,
      column = 0,
      text = message,
    ) =>
      isFirefoxBounceTrackerAdvisory(browser, type, source, line, column, text);
    expect(exact()).toBe(true);
    expect(exact("chromium")).toBe(false);
    expect(exact("firefox", "error")).toBe(false);
    expect(exact("firefox", "pageerror")).toBe(false);
    expect(exact("firefox", "warning", "https://kodex.test/app.js")).toBe(
      false,
    );
    expect(exact("firefox", "warning", "", 1)).toBe(false);
    expect(exact("firefox", "warning", "", 0, 1)).toBe(false);
    expect(exact("firefox", "warning", "", 0, 0, "Application warning")).toBe(
      false,
    );
    expect(
      exact(
        "firefox",
        "warning",
        "",
        0,
        0,
        message.replace("identity.invalid", "other.invalid"),
      ),
    ).toBe(false);
    expect(exact("firefox", "warning", "", 0, 0, `${message} Changed`)).toBe(
      false,
    );
  });
});
