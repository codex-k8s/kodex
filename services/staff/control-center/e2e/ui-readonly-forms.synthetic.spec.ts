import { expect, test } from "@playwright/test";
import { projectForm, searchAssistantHistory } from "./ui-readonly-forms";
import { permittedRequest } from "./ui-acceptance-proof";

test("synthetic: поздний initial response и пустой список не заменяют exact query", async ({
  page,
}) => {
  let release!: () => void;
  const delayed = new Promise<void>((resolve) => {
    release = resolve;
  });
  let exactRequested = false;
  let finished = false;
  await page.route("https://kodex.test/**", async (route) => {
    const url = new URL(route.request().url());
    if (url.pathname === "/api/v1/assistant-conversations") {
      if (url.searchParams.get("query") === "synthetic-absent") {
        exactRequested = true;
        await delayed;
      }
      await route.fulfill({ json: { items: [] } });
      return;
    }
    await route.fulfill({
      contentType: "text/html",
      body: `<!doctype html><meta charset="UTF-8"><input type="search"><div id="entries"><button>Fixture</button></div><script>
      document.querySelector('input').oninput = async (event) => {
        await fetch('/api/v1/assistant-conversations').then(r => r.json());
        document.querySelector('#entries').innerHTML = '';
        await fetch('/api/v1/assistant-conversations?query=' + encodeURIComponent(event.target.value)).then(r => r.json());
      };
    </script>`,
    });
  });
  await page.goto("https://kodex.test");
  const proof = searchAssistantHistory(
    page,
    page.getByRole("searchbox"),
    page.locator("#entries button"),
    "synthetic-absent",
  ).then(() => {
    finished = true;
  });
  try {
    await expect.poll(() => exactRequested).toBe(true);
    await expect(page.locator("#entries button")).toHaveCount(0);
    await new Promise((resolve) => setTimeout(resolve, 100));
    expect(finished).toBe(false);
  } finally {
    release();
    await proof;
    await page.unrouteAll({ behavior: "wait" });
  }
  expect(finished).toBe(true);
});

for (const locale of ["ru", "en"] as const) {
  test(`synthetic: форма ${locale} проверяет native input и закрывается без submit`, async ({
    page,
  }) => {
    let writes = 0;
    await page.route("https://kodex.test/**", async (route) => {
      const request = route.request();
      if (
        !permittedRequest(
          request.method(),
          new URL(request.url()).pathname,
          false,
        )
      ) {
        writes++;
        await route.abort();
        return;
      }
      await route.fulfill({
        contentType: "text/html",
        body: `<!doctype html><meta charset="UTF-8"><style>body{margin:0}.topbar{height:58px}[hidden]{display:none}</style><div class="app-shell"><div class="topbar"></div><div class="page-header"><h1>Fixture</h1></div><button id="open">${locale === "ru" ? "Новый Проект" : "New Project"}</button><div id="holder"></div></div><script>
      document.querySelector('#open').onclick = () => {
        document.querySelector('#holder').innerHTML = '<form id="project-form"><input required maxlength="120"><textarea required></textarea><button type="button" id="cancel">${locale === "ru" ? "Отмена" : "Cancel"}</button></form>';
        document.querySelector('input').focus();
        document.querySelector('#cancel').onclick = () => {document.querySelector('#holder').innerHTML = ''};
        document.querySelector('form').onsubmit = (event) => {event.preventDefault();fetch('/api/v1/projects',{method:'POST'})};
      };
      </script>`,
      });
    });
    const result = await projectForm(page, locale, "synthetic-readonly");
    expect(result).toMatchObject({
      requiredValidation: true,
      focusPreserved: true,
    });
    expect(writes).toBe(0);
    await expect(page.locator("#project-form")).toHaveCount(0);
  });
}
