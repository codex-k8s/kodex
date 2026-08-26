import { type Page } from "@playwright/test";

const authenticationTimeoutMs = 100_000;
const surfacePollIntervalMs = 250;
const maxTransitions = 5;

type AuthSurface =
  | "authenticated-ui"
  | "frontend-sign-in"
  | "identity-provider"
  | "pending";

export interface OwnerCredentials {
  readonly password: string;
  readonly username: string;
}

interface AuthProgress {
  readonly frontendOIDCStarted: boolean;
  readonly identitySubmissions: number;
}

export async function authenticateOwner(
  page: Page,
  credentials: OwnerCredentials,
): Promise<void> {
  const deadline = Date.now() + authenticationTimeoutMs;
  let identitySubmissions = 0;
  let frontendOIDCStarted = false;

  await page.goto("/", {
    timeout: remainingTimeout(deadline),
    waitUntil: "domcontentloaded",
  });

  let surface = await waitForAuthSurface(page, deadline, undefined, {
    identitySubmissions,
    frontendOIDCStarted,
  });
  for (let transition = 0; transition < maxTransitions; transition += 1) {
    if (surface === "authenticated-ui") {
      if (identitySubmissions < 1 || !frontendOIDCStarted) {
        throw authenticationError(
          page,
          "the required proxy and frontend OIDC gates were not both observed",
          identitySubmissions,
          frontendOIDCStarted,
        );
      }
      return;
    }

    if (surface === "frontend-sign-in") {
      if (identitySubmissions < 1) {
        throw authenticationError(
          page,
          "the frontend sign-in gate appeared before proxy authentication",
          identitySubmissions,
          frontendOIDCStarted,
        );
      }
      if (frontendOIDCStarted) {
        throw authenticationError(
          page,
          "the frontend sign-in gate repeated after OIDC had started",
          identitySubmissions,
          frontendOIDCStarted,
        );
      }
      frontendOIDCStarted = true;
      await page.getByRole("button", { name: "Войти", exact: true }).click({
        timeout: remainingTimeout(deadline),
      });
      surface = await waitForAuthSurface(page, deadline, "frontend-sign-in", {
        identitySubmissions,
        frontendOIDCStarted,
      });
      continue;
    }

    if (identitySubmissions > 0 && !frontendOIDCStarted) {
      throw authenticationError(
        page,
        "the identity provider rejected or repeated the proxy login",
        identitySubmissions,
        frontendOIDCStarted,
      );
    }
    if (identitySubmissions >= 2) {
      throw authenticationError(
        page,
        "the identity provider requested credentials too many times",
        identitySubmissions,
        frontendOIDCStarted,
      );
    }

    await submitIdentityProvider(page, credentials, deadline);
    identitySubmissions += 1;
    surface = await waitForAuthSurface(page, deadline, "identity-provider", {
      identitySubmissions,
      frontendOIDCStarted,
    });
  }

  throw authenticationError(
    page,
    "the transition limit was exhausted",
    identitySubmissions,
    frontendOIDCStarted,
  );
}

async function waitForAuthSurface(
  page: Page,
  deadline: number,
  previous?: Exclude<AuthSurface, "pending">,
  progress: AuthProgress = {
    identitySubmissions: 0,
    frontendOIDCStarted: false,
  },
): Promise<Exclude<AuthSurface, "pending">> {
  while (Date.now() < deadline) {
    const surface = await detectAuthSurface(page);
    if (surface !== "pending" && surface !== previous) return surface;
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
    progress.frontendOIDCStarted,
  );
}

async function detectAuthSurface(page: Page): Promise<AuthSurface> {
  try {
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
  frontendOIDCStarted: boolean,
): Error {
  return new Error(
    [
      `Authentication flow failed: ${reason}`,
      `location=${safeLocation(page.url())}`,
      `identity_submissions=${String(identitySubmissions)}`,
      `frontend_oidc_started=${String(frontendOIDCStarted)}`,
    ].join("; "),
  );
}

function safeLocation(raw: string): string {
  try {
    const parsed = new URL(raw);
    return `${parsed.origin}${parsed.pathname}`.slice(0, 512);
  } catch {
    return "invalid-url";
  }
}
