import { expect, test } from "@playwright/test";
import {
  projectForm,
  projectCollection,
  searchAssistantHistory,
  ReadonlyFixtureMissing,
  waitForProjectCollection,
} from "./ui-readonly-forms";
import { UIConditionError, permittedRequest } from "./ui-acceptance-proof";

test("synthetic: exact permission query читается, соседняя mutation блокируется", async ({
  page,
}) => {
  let allowed = 0,
    blocked = 0;
  await page.route("https://kodex.test/**", async (route) => {
    const request = route.request(),
      url = new URL(request.url());
    if (request.method() === "POST") {
      if (
        !permittedRequest(
          request.method(),
          url.pathname,
          false,
          request.postDataJSON(),
        )
      ) {
        blocked++;
        await route.abort("blockedbyclient");
      } else {
        allowed++;
        await route.fulfill({ json: { items: [] } });
      }
      return;
    }
    await route.fulfill({
      contentType: "text/html",
      body: "<!doctype html><title>Fixture</title>",
    });
  });
  await page.goto("https://kodex.test");
  const read = await page.evaluate(async () => {
    const body = JSON.stringify({
      target: { kind: "ORGANIZATION" },
      permissionKeys: [
        "image.build",
        "image.source.view",
        "image.source.manage",
      ],
    });
    const response = await fetch(
      "/api/v1/administration/access/effective-access/query",
      { method: "POST", headers: { "Content-Type": "application/json" }, body },
    );
    const rejected = await fetch("/api/v1/administration/access/bindings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
    }).then(
      () => false,
      () => true,
    );
    return { status: response.status, rejected };
  });
  expect(read).toEqual({ status: 200, rejected: true });
  expect({ allowed, blocked }).toEqual({ allowed: 1, blocked: 1 });
});

for (const outcome of ["populated", "empty", "error"] as const) {
  test(`synthetic: collection ждёт delayed read и различает ${outcome}`, async ({
    page,
  }) => {
    let release!: () => void;
    const delayed = new Promise<void>((resolve) => {
      release = resolve;
    });
    let requested = false,
      finished = false;
    await page.route("https://kodex.test/**", async (route) => {
      if (new URL(route.request().url()).pathname === "/api/v1/projects") {
        requested = true;
        await delayed;
        await route.fulfill({
          status: outcome === "error" ? 503 : 200,
          json: { items: outcome === "populated" ? [{}] : [] },
        });
        return;
      }
      await route.fulfill({
        contentType: "text/html",
        body: `<!doctype html><meta charset="UTF-8"><style>body{margin:0}.topbar{height:58px}</style><div class="app-shell"><div class="topbar"></div><main class="page-frame"><div class="page-header"><h1>Fixture</h1></div><div class="projects-toolbar"><button class="icon-button">Expand</button></div><p role="status">Loading</p><div id="rows"></div></main></div><script>
      fetch('/api/v1/projects?pageSize=30').then(async response => {const data=await response.json();document.querySelector('[role=status]').remove();if(response.ok)document.querySelector('#rows').innerHTML=data.items.length?'<div class="project-list__item">Fixture</div>':'<p>Create your first Project</p>';});
      document.querySelector('button').onclick=()=>{document.querySelector('#rows').innerHTML='<div role="dialog" aria-label="Projects"><div class="project-list__item">Fixture</div><button aria-label="Close" onclick="this.parentElement.remove()">Close</button></div>'};
      </script>`,
      });
    });
    const result = projectCollection(page, "en").then(
      (value) => {
        finished = true;
        return { value };
      },
      (error: unknown) => {
        finished = true;
        return { error };
      },
    );
    await expect.poll(() => requested).toBe(true);
    await expect(page.getByRole("status")).toBeVisible();
    expect(finished).toBe(false);
    release();
    const actual = await result;
    if (outcome === "populated")
      expect(actual).toHaveProperty("value.collapsedRows", 1);
    else if (outcome === "empty") {
      expect(actual).toHaveProperty("error");
      if ("error" in actual)
        expect(actual.error).toBeInstanceOf(ReadonlyFixtureMissing);
    } else {
      expect(actual).toHaveProperty("error");
      if ("error" in actual)
        expect(actual.error).not.toBeInstanceOf(ReadonlyFixtureMissing);
    }
    await page.unrouteAll({ behavior: "wait" });
  });
}

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

for (const locale of ["ru", "en"] as const) {
  for (const outcome of ["populated", "empty", "error"] as const) {
    test(`synthetic: расширение ${locale}/${outcome} ждёт terminal UI без повторного действия`, async ({
      page,
    }) => {
      let reads = 0;
      await page.route("https://kodex.test/**", async (route) => {
        if (new URL(route.request().url()).pathname === "/api/v1/projects") {
          reads++;
          await route.fulfill({ json: { items: [{}] } });
          return;
        }
        await route.fulfill({
          contentType: "text/html",
          body: `<!doctype html><meta charset="UTF-8"><style>.topbar{height:58px}</style>
          <div class="app-shell"><div class="topbar"></div><main class="page-frame">
          <div class="page-header"><h1>Fixture</h1></div>
          <div class="projects-toolbar"><button class="icon-button">Expand</button></div>
          <p role="status">Loading</p><div id="rows"></div></main></div><script>
          fetch('/api/v1/projects?pageSize=30').then(r=>r.json()).then(()=>{
            document.querySelector('[role=status]').remove();
            document.querySelector('#rows').innerHTML='<article class="project-list__item">Fixture</article>';
          });
          document.querySelector('button').onclick=()=>{
            document.querySelector('#rows').innerHTML='<div role="dialog" aria-label="${locale === "ru" ? "Проекты" : "Projects"}"><div id="result"><p role="status">Loading</p></div><button aria-label="${locale === "ru" ? "Закрыть" : "Close"}" onclick="this.parentElement.remove()">Close</button></div>';
            document.querySelector('.projects-toolbar button').disabled=true;
          };
          </script>`,
        });
      });
      let finished = false;
      const result = projectCollection(page, locale)
        .then(
          (value) => ({ value }),
          (error: unknown) => ({ error }),
        )
        .finally(() => {
          finished = true;
        });
      await expect(page.getByRole("dialog").getByRole("status")).toBeVisible();
      // Старый count сразу давал PROJECT_DIALOG_EMPTY и закрывал диалог.
      await page.waitForTimeout(150);
      expect(finished).toBe(false);
      await page.locator("#result").evaluate(
        (element, input) => {
          const node = document.createElement(
            input.outcome === "populated" ? "article" : "p",
          );
          if (input.outcome === "populated")
            node.className = "project-list__item";
          if (input.outcome === "error") node.setAttribute("role", "alert");
          node.textContent =
            input.outcome === "empty"
              ? input.locale === "ru"
                ? "Создайте первый Проект"
                : "Create your first Project"
              : "Fixture";
          element.replaceChildren(node);
        },
        { outcome, locale },
      );
      const actual = await result;
      if (outcome === "populated") {
        expect(actual).toHaveProperty("value.collapsedRows", 1);
        expect(actual).toHaveProperty("value.expandedRows", 1);
      } else {
        expect(actual).toHaveProperty("error");
        if ("error" in actual) {
          if (outcome === "empty") {
            expect(actual.error).toBeInstanceOf(ReadonlyFixtureMissing);
            expect(actual.error).toHaveProperty(
              "stage",
              "PROJECT_DIALOG_EMPTY",
            );
          } else {
            expect(actual.error).toBeInstanceOf(UIConditionError);
            expect(actual.error).toHaveProperty("condition", "VISIBLE_ALERT");
          }
        }
      }
      expect(reads).toBe(1);
      await expect(page.getByRole("dialog")).toHaveCount(0);
    });
  }
}

for (const state of [
  "loading",
  "unmarked-empty",
  "hidden-rows",
  "loading-stale-rows",
  "alert-stale-rows",
] as const) {
  test(`synthetic: collection ${state} не становится fixture NOT RUN или ложным PASS`, async ({
    page,
  }) => {
    await page.setContent(`<main>
      ${state.includes("loading") ? '<p role="status">Loading</p>' : ""}
      ${state === "alert-stale-rows" ? '<p role="alert">Fixture error</p>' : ""}
      ${state.includes("rows") ? `<article class="project-list__item" ${state === "hidden-rows" ? "hidden" : ""}>Fixture</article>` : ""}
      ${state === "loading" ? "<p>Create your first Project</p>" : ""}
    </main>`);
    const started = Date.now();
    const error = await waitForProjectCollection(
      page.locator("main"),
      "en",
      "PROJECT_DIALOG_EMPTY",
      300,
    ).then(
      () => undefined,
      (value: unknown) => value,
    );
    expect(error).toBeInstanceOf(UIConditionError);
    expect(error).not.toBeInstanceOf(ReadonlyFixtureMissing);
    expect(error).toHaveProperty(
      "condition",
      state === "alert-stale-rows" ? "VISIBLE_ALERT" : "UI_ACTION",
    );
    expect(Date.now() - started).toBeLessThan(3000);
  });
}
