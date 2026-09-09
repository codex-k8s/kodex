import { expect, test } from "@playwright/test";
import { authenticateOwner } from "./auth-flow";

const cases = [
  {
    name: "cold delayed initial 401",
    mode: "local",
    initial: 401,
    expected: 200,
  },
  {
    name: "warm delayed initial 401",
    mode: "warm",
    initial: 401,
    expected: 200,
  },
  { name: "initial 403", mode: "local", initial: 403, expected: 403 },
  { name: "initial 503", mode: "local", initial: 503, expected: 503 },
  {
    name: "new session 401",
    mode: "local",
    initial: 401,
    readback: 401,
    expected: 401,
  },
  {
    name: "callback 401",
    mode: "local",
    initial: 401,
    callback: 401,
    expected: 401,
  },
  {
    name: "callback 503",
    mode: "local",
    initial: 401,
    callback: 503,
    expected: 503,
  },
] as const;

for (const scenario of cases) {
  test(`synthetic: OIDC ${scenario.name}`, async ({ page }) => {
    let releaseInitial!: () => void;
    const initialPending = new Promise<void>((resolve) => {
      releaseInitial = resolve;
    });
    let sessionRequests = 0;
    let callbackRequests = 0;
    let identitySubmissions = 0;
    let authorizationRequests = 0;
    const pageErrors: string[] = [];
    page.on("pageerror", (error) => pageErrors.push(error.name));
    await page.route("**/*", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      if (url.origin !== "https://kodex.test") {
        await route.abort("blockedbyclient");
        return;
      }
      if (url.pathname === "/api/v1/session") {
        sessionRequests++;
        if (sessionRequests === 1) await initialPending;
        await route.fulfill({
          status:
            sessionRequests === 1
              ? scenario.initial
              : "readback" in scenario
                ? scenario.readback
                : 200,
          json: {},
        });
        return;
      }
      if (url.pathname === "/api/v1/bootstrap") {
        await route.fulfill({ status: 401, json: {} });
        return;
      }
      if (url.pathname === "/api/v1/session/authorization") {
        authorizationRequests++;
        releaseInitial();
        await new Promise((resolve) => setTimeout(resolve, 600));
        await route.fulfill({ json: {} });
        return;
      }
      if (url.pathname === "/api/v1/session/callback") {
        callbackRequests++;
        await route.fulfill({
          status: "callback" in scenario ? scenario.callback : 200,
          json: {},
        });
        return;
      }
      if (url.pathname === "/identity-submit") identitySubmissions++;
      if (url.pathname === "/" && scenario.mode === "local") {
        await route.fulfill({
          contentType: "text/html",
          body: '<!doctype html><meta charset="utf-8"><form action="/identity-submit" method="post"><input name="username"><input name="password" type="password"><button type="submit">Submit</button></form>',
        });
        return;
      }
      await route.fulfill({
        contentType: "text/html",
        body: `<!doctype html><meta charset="utf-8"><main id="app">Loading</main><script>
        const root = document.querySelector('#app');
        const session = fetch('/api/v1/session').then(response => { if (!response.ok) throw Error('Expected probe rejection'); });
        const bootstrap = fetch('/api/v1/bootstrap').then(response => { if (!response.ok) throw Error('Expected probe rejection'); });
        Promise.all([session, bootstrap]).catch(() => {
          root.innerHTML = '<button>Войти</button>';
          root.querySelector('button').onclick = async () => {
            root.textContent = 'Loading';
            await fetch('/api/v1/session/authorization', {method:'POST'});
            const callback = await fetch('/api/v1/session/callback', {method:'POST'});
            if (!callback.ok) return;
            const readback = await fetch('/api/v1/session');
            if (!readback.ok) return;
            root.innerHTML = '<div class="app-shell">Ready</div>';
          };
        });
      </script>`,
      });
    });
    const result = await authenticateOwner(
      page,
      scenario.mode === "local"
        ? { username: "fixture-user", password: "fixture-password" }
        : undefined,
      { mode: scenario.mode },
    ).then(
      (value) => ({ value }),
      (error: unknown) => ({ error }),
    );
    releaseInitial();
    if (scenario.expected === 200) {
      expect(result).toHaveProperty("value");
      if ("value" in result)
        expect(result.value).toMatchObject({
          identitySubmissions: scenario.mode === "local" ? 1 : 0,
          frontendOIDCAttempts: 1,
          ownerSessionStatuses: [200],
          initialSessionProbe401s: 1,
        });
      expect({ sessionRequests, callbackRequests }).toEqual({
        sessionRequests: 2,
        callbackRequests: 1,
      });
    } else {
      expect(result).toHaveProperty("error");
      if ("error" in result)
        expect(String(result.error)).toContain(
          `HTTP error (${String(scenario.expected)}:`,
        );
    }
    expect(identitySubmissions).toBe(scenario.mode === "local" ? 1 : 0);
    expect(authorizationRequests).toBe(1);
    expect(pageErrors).toEqual([]);
    await page.unrouteAll({ behavior: "wait" });
  });
}
