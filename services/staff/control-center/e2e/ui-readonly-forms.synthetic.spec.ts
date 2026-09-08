import { expect, test } from "@playwright/test";
import { projectForm } from "./ui-readonly-forms";
import { permittedRequest } from "./ui-acceptance-proof";

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
