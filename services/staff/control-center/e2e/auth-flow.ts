import { type Frame, type Page, type Response } from "@playwright/test";

import { gotoWithRetry } from "./helpers";

const authenticationTimeoutMs = 100_000;
const blankCallbackRecoveryMs = 5_000;
const frontendOIDCRetryTimeoutMs = 7_500;
const maxFrontendOIDCAttempts = 2;
const surfacePollIntervalMs = 250;
const maxTransitions = 5;

type AuthSurface =
  | "authenticated-ui"
  | "application-failure"
  | "frontend-sign-in"
  | "identity-provider"
  | "pending";

export interface OwnerCredentials {
  readonly password: string;
  readonly username: string;
}

export interface AuthenticationOptions {
  readonly mode: "cold" | "local" | "warm";
}

export interface AuthenticationResult {
  readonly frontendOIDCAttempts: number;
  readonly identitySubmissions: number;
  readonly ownerSessionStatuses: readonly number[];
}

export interface AuthenticationDocumentSnapshot {
  readonly appChildElementCount: number | undefined;
  readonly bodyText: string;
  readonly origin: string;
  readonly pathname: string;
}

interface AuthProgress {
  readonly frontendOIDCAttempts: number;
  readonly identitySubmissions: number;
}

export async function authenticateOwner(
  page: Page,
  credentials: OwnerCredentials | undefined,
  options: AuthenticationOptions,
): Promise<AuthenticationResult> {
  const deadline = Date.now() + authenticationTimeoutMs;
  let identitySubmissions = 0;
  let frontendOIDCAttempts = 0;
  const ownerSessionStatuses: number[] = [];

  await gotoWithRetry(
    page,
    "/",
    {
      timeout: remainingTimeout(deadline),
      waitUntil: "domcontentloaded",
    },
    { appShell: false },
  );

  let surface = await waitForAuthSurface(page, deadline, undefined, {
    identitySubmissions,
    frontendOIDCAttempts,
  });
  for (let transition = 0; transition < maxTransitions; transition += 1) {
    if (surface === "application-failure") {
      throw authenticationError(
        page,
        "the Control Center bootstrap failed",
        identitySubmissions,
        frontendOIDCAttempts,
      );
    }
    if (surface === "authenticated-ui") {
      if (
        frontendOIDCAttempts < 1 ||
        (options.mode === "cold" && identitySubmissions < 1) ||
        (options.mode === "local" && identitySubmissions < 1) ||
        (options.mode === "warm" && identitySubmissions !== 0)
      ) {
        throw authenticationError(
          page,
          `the required ${options.mode} authentication gates were not observed`,
          identitySubmissions,
          frontendOIDCAttempts,
        );
      }
      await waitForVisibleImages(page, deadline);
      await waitForAuthenticationNavigation(page, deadline);
      return {
        frontendOIDCAttempts,
        identitySubmissions,
        ownerSessionStatuses,
      };
    }

    if (surface === "frontend-sign-in") {
      if (options.mode === "cold" && identitySubmissions < 1) {
        throw authenticationError(
          page,
          "the frontend sign-in gate appeared before proxy authentication",
          identitySubmissions,
          frontendOIDCAttempts,
        );
      }
      if (frontendOIDCAttempts >= maxFrontendOIDCAttempts) {
        throw authenticationError(
          page,
          "the frontend sign-in gate remained after the bounded OIDC retries",
          identitySubmissions,
          frontendOIDCAttempts,
        );
      }
      frontendOIDCAttempts += 1;
      surface = await startFrontendOIDCTransition(
        page,
        deadline,
        {
          identitySubmissions,
          frontendOIDCAttempts,
        },
        ownerSessionStatuses,
      );
      if (
        surface === "frontend-sign-in" &&
        frontendOIDCAttempts < maxFrontendOIDCAttempts
      ) {
        reportFrontendOIDCRetry(page, frontendOIDCAttempts);
      }
      continue;
    }

    if (options.mode === "warm") {
      throw authenticationError(
        page,
        "the identity provider requested credentials during warm SSO",
        identitySubmissions,
        frontendOIDCAttempts,
      );
    }
    if (!credentials) {
      throw authenticationError(
        page,
        "owner credentials are unavailable for cold authentication",
        identitySubmissions,
        frontendOIDCAttempts,
      );
    }

    if (identitySubmissions > 0 && frontendOIDCAttempts < 1) {
      throw authenticationError(
        page,
        "the identity provider rejected or repeated the proxy login",
        identitySubmissions,
        frontendOIDCAttempts,
      );
    }
    if (identitySubmissions >= 2) {
      throw authenticationError(
        page,
        "the identity provider requested credentials too many times",
        identitySubmissions,
        frontendOIDCAttempts,
      );
    }

    await submitIdentityProvider(page, credentials, deadline);
    identitySubmissions += 1;
    surface = await waitForAuthSurface(page, deadline, "identity-provider", {
      identitySubmissions,
      frontendOIDCAttempts,
    });
  }

  throw authenticationError(
    page,
    "the transition limit was exhausted",
    identitySubmissions,
    frontendOIDCAttempts,
  );
}

async function startFrontendOIDCTransition(
  page: Page,
  deadline: number,
  progress: AuthProgress,
  ownerSessionStatuses: number[],
): Promise<Exclude<AuthSurface, "pending">> {
  const frontendOrigin = new URL(page.url()).origin;
  const blankCallbackDeadline = Date.now() + blankCallbackRecoveryMs;
  let authenticationProgressObserved = false;
  let failedResponse: string | undefined;
  const recordNavigation = (frame: Frame): void => {
    if (frame.parentFrame() !== null) return;
    authenticationProgressObserved ||= isOIDCProgressLocation(
      frame.url(),
      frontendOrigin,
    );
  };
  const recordFailedResponse = (response: Response): void => {
    if (isOwnerSessionCreationResponse(response, frontendOrigin)) {
      ownerSessionStatuses.push(response.status());
    }
    if (response.status() < 400 || !isAuthenticationResponse(response)) return;
    failedResponse = `${String(response.status())}:${response.request().method()}:${safeLocation(response.url())}`;
  };
  page.on("framenavigated", recordNavigation);
  page.on("response", recordFailedResponse);
  try {
    await page
      .getByRole("button", { name: /^(Войти|Повторить)$/ })
      .click({ timeout: remainingTimeout(deadline) });
    const retryDeadline = Math.min(
      deadline,
      Date.now() + frontendOIDCRetryTimeoutMs,
    );
    let surface: AuthSurface = "pending";
    while (Date.now() < deadline) {
      if (failedResponse) {
        throw authenticationError(
          page,
          `the frontend OIDC transition received an HTTP error (${failedResponse})`,
          progress.identitySubmissions,
          progress.frontendOIDCAttempts,
        );
      }
      surface = await detectAuthSurface(page);
      if (surface !== "pending" && surface !== "frontend-sign-in") {
        return surface;
      }
      if (
        Date.now() >= blankCallbackDeadline &&
        new URL(page.url()).pathname === "/auth/callback" &&
        (await hasBlankApplicationDocument(page, frontendOrigin))
      ) {
        await gotoWithRetry(
          page,
          "/",
          {
            timeout: remainingTimeout(deadline),
            waitUntil: "domcontentloaded",
          },
          { appShell: false },
        );
        return await waitForAuthSurface(page, deadline, undefined, progress);
      }
      authenticationProgressObserved ||= isOIDCProgressLocation(
        page.url(),
        frontendOrigin,
      );
      const activeDeadline = authenticationProgressObserved
        ? deadline
        : retryDeadline;
      if (Date.now() >= activeDeadline) break;
      await page.waitForTimeout(
        Math.min(surfacePollIntervalMs, remainingTimeout(activeDeadline)),
      );
    }
    if (failedResponse) {
      throw authenticationError(
        page,
        `the frontend OIDC transition received an HTTP error (${failedResponse})`,
        progress.identitySubmissions,
        progress.frontendOIDCAttempts,
      );
    }
    if (surface === "frontend-sign-in") return surface;
    throw authenticationError(
      page,
      authenticationProgressObserved
        ? "the frontend OIDC transition remained pending before the authentication deadline"
        : "the frontend OIDC transition remained pending before the retry deadline",
      progress.identitySubmissions,
      progress.frontendOIDCAttempts,
    );
  } finally {
    page.off("framenavigated", recordNavigation);
    page.off("response", recordFailedResponse);
  }
}

async function waitForVisibleImages(
  page: Page,
  deadline: number,
): Promise<void> {
  await page.waitForFunction(
    () =>
      Array.from(document.querySelectorAll<HTMLImageElement>("img"))
        .filter((image) => image.getClientRects().length > 0)
        .every((image) => image.complete && image.naturalWidth > 0),
    undefined,
    { timeout: remainingTimeout(deadline) },
  );
}

async function waitForAuthenticationNavigation(
  page: Page,
  deadline: number,
): Promise<void> {
  await page.waitForFunction(
    () => window.location.pathname !== "/auth/callback",
    undefined,
    { timeout: remainingTimeout(deadline) },
  );
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        window.requestAnimationFrame(() =>
          window.requestAnimationFrame(() => resolve()),
        );
      }),
  );
}

async function waitForAuthSurface(
  page: Page,
  deadline: number,
  previous?: Exclude<AuthSurface, "pending">,
  progress: AuthProgress = {
    identitySubmissions: 0,
    frontendOIDCAttempts: 0,
  },
): Promise<Exclude<AuthSurface, "pending">> {
  const initialDeadline = Date.now() + 5_000;
  let retriedBlankInitialDocument = false;
  while (Date.now() < deadline) {
    const surface = await detectAuthSurface(page);
    if (surface !== "pending" && surface !== previous) return surface;
    if (
      previous === undefined &&
      !retriedBlankInitialDocument &&
      Date.now() >= initialDeadline &&
      (await hasBlankApplicationDocument(page, new URL(page.url()).origin))
    ) {
      retriedBlankInitialDocument = true;
      await page.reload({
        timeout: remainingTimeout(deadline),
        waitUntil: "domcontentloaded",
      });
      continue;
    }
    await page.waitForTimeout(
      Math.min(surfacePollIntervalMs, remainingTimeout(deadline)),
    );
  }
  throw authenticationError(
    page,
    previous === undefined
      ? "no actionable authentication surface appeared before the deadline"
      : `the ${previous} surface did not transition before the deadline`,
    progress.identitySubmissions,
    progress.frontendOIDCAttempts,
  );
}

async function hasBlankApplicationDocument(
  page: Page,
  frontendOrigin: string,
): Promise<boolean> {
  try {
    const snapshot = await page.evaluate(() => {
      const application = document.querySelector("#app");
      return {
        appChildElementCount:
          application instanceof HTMLElement
            ? application.childElementCount
            : undefined,
        bodyText: document.body.textContent,
        origin: window.location.origin,
        pathname: window.location.pathname,
      } satisfies AuthenticationDocumentSnapshot;
    });
    return isRecoverableBlankFrontendDocument(snapshot, frontendOrigin);
  } catch {
    return false;
  }
}

export function isRecoverableBlankFrontendDocument(
  snapshot: AuthenticationDocumentSnapshot,
  frontendOrigin: string,
): boolean {
  return (
    snapshot.origin === frontendOrigin &&
    (snapshot.pathname === "/" || snapshot.pathname === "/auth/callback") &&
    snapshot.bodyText.trim() === "" &&
    (snapshot.appChildElementCount === undefined ||
      snapshot.appChildElementCount === 0)
  );
}

async function detectAuthSurface(page: Page): Promise<AuthSurface> {
  try {
    if (
      (await page.locator('html[data-kodex-bootstrap="failed"]').count()) > 0
    ) {
      return "application-failure";
    }
    if (await page.locator(".app-shell").isVisible()) {
      return "authenticated-ui";
    }
    if (
      await page.getByRole("button", { name: "Выйти", exact: true }).isVisible()
    ) {
      return "authenticated-ui";
    }
    if (
      (await page.locator('input[name="username"]').isVisible()) &&
      (await page.locator('input[name="password"]').isVisible()) &&
      (await page
        .locator('button[type="submit"], input[type="submit"]')
        .first()
        .isVisible())
    ) {
      return "identity-provider";
    }
    if (
      await page.getByRole("button", { name: "Войти", exact: true }).isVisible()
    ) {
      return "frontend-sign-in";
    }
    if (
      await page
        .locator(".auth-gate")
        .getByRole("button", { name: "Повторить", exact: true })
        .isVisible()
    ) {
      return "frontend-sign-in";
    }
  } catch {
    // Navigation may replace the execution context between locator probes.
  }
  return "pending";
}

async function submitIdentityProvider(
  page: Page,
  credentials: OwnerCredentials,
  deadline: number,
): Promise<void> {
  await page
    .locator('input[name="username"]')
    .fill(credentials.username, { timeout: remainingTimeout(deadline) });
  await page
    .locator('input[name="password"]')
    .fill(credentials.password, { timeout: remainingTimeout(deadline) });
  await page
    .locator('button[type="submit"], input[type="submit"]')
    .first()
    .click({ timeout: remainingTimeout(deadline) });
}

function remainingTimeout(deadline: number): number {
  return Math.max(1, deadline - Date.now());
}

function authenticationError(
  page: Page,
  reason: string,
  identitySubmissions: number,
  frontendOIDCAttempts: number,
): Error {
  return new Error(
    [
      `Authentication flow failed: ${reason}`,
      `location=${safeLocation(page.url())}`,
      `identity_submissions=${String(identitySubmissions)}`,
      `frontend_oidc_attempts=${String(frontendOIDCAttempts)}`,
    ].join("; "),
  );
}

function reportFrontendOIDCRetry(page: Page, completedAttempts: number): void {
  process.stderr.write(
    `[e2e-auth] frontend OIDC did not leave the sign-in surface; retrying attempt=${String(completedAttempts + 1)}/${String(maxFrontendOIDCAttempts)} location=${safeLocation(page.url())}\n`,
  );
}

function isAuthenticationResponse(response: Response): boolean {
  if (response.request().resourceType() === "document") return true;
  try {
    const path = new URL(response.url()).pathname;
    return (
      path === "/api/v1/session" ||
      path.includes("/.well-known/") ||
      path.includes("/protocol/openid-connect/") ||
      path.endsWith("/authorize") ||
      path.endsWith("/token")
    );
  } catch {
    return false;
  }
}

function isOwnerSessionCreationResponse(
  response: Response,
  frontendOrigin: string,
): boolean {
  try {
    const location = new URL(response.url());
    return (
      response.request().method() === "POST" &&
      location.origin === frontendOrigin &&
      location.pathname === "/api/v1/session"
    );
  } catch {
    return false;
  }
}

function isOIDCProgressLocation(raw: string, frontendOrigin: string): boolean {
  try {
    const location = new URL(raw);
    return (
      location.origin !== frontendOrigin ||
      location.pathname === "/auth/callback"
    );
  } catch {
    return false;
  }
}

function safeLocation(raw: string): string {
  try {
    const parsed = new URL(raw);
    return `${parsed.origin}${parsed.pathname}`.slice(0, 512);
  } catch {
    return "invalid-url";
  }
}
