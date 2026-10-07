import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { withFrontendCodegen } from "./frontend-codegen-barrier.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "kodex-codegen-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  return root;
}
test("barrier охватывает весь output и исчезает только после успешной generation", async (t) => {
  const root = await fixture(t);
  await withFrontendCodegen(
    "asyncapi",
    async () => {
      const status = JSON.parse(
        await readFile(
          join(root, ".kodex-codegen/asyncapi/status.json"),
          "utf8",
        ),
      );
      assert.equal(status.state, "RUNNING");
      assert.ok(Number.isSafeInteger(status.startedAt));
      assert.deepEqual(await readdir(join(root, ".kodex-codegen/asyncapi")), [
        "active",
        "status.json",
      ]);
    },
    root,
  );
  assert.deepEqual(await readdir(join(root, ".kodex-codegen")), []);
});
test("failed output остаётся FAILED; явный успешный rerun восстанавливает READY", async (t) => {
  const root = await fixture(t);
  await assert.rejects(
    withFrontendCodegen(
      "openapi",
      async () => {
        throw new Error("fixture");
      },
      root,
    ),
    /fixture/,
  );
  assert.equal(
    JSON.parse(
      await readFile(join(root, ".kodex-codegen/openapi/status.json"), "utf8"),
    ).state,
    "FAILED",
  );
  await withFrontendCodegen("openapi", async () => {}, root);
  assert.deepEqual(await readdir(join(root, ".kodex-codegen")), []);
});
test("parallel generation того же output закрыта; разные generators независимы", async (t) => {
  const root = await fixture(t);
  await withFrontendCodegen(
    "asyncapi",
    async () => {
      let called = false;
      await assert.rejects(
        withFrontendCodegen(
          "asyncapi",
          async () => {
            called = true;
          },
          root,
        ),
        /already active/,
      );
      assert.equal(called, false);
      await withFrontendCodegen("integration-schema", async () => {}, root);
      assert.equal(
        JSON.parse(
          await readFile(
            join(root, ".kodex-codegen/asyncapi/status.json"),
            "utf8",
          ),
        ).state,
        "RUNNING",
      );
    },
    root,
  );
});
test("неизвестный generator отклоняется до создания marker или вызова effect", async (t) => {
  const root = await fixture(t);
  let called = false;
  await assert.rejects(
    withFrontendCodegen(
      "../unknown",
      () => {
        called = true;
      },
      root,
    ),
    /Invalid/,
  );
  assert.equal(called, false);
  assert.deepEqual(await readdir(root), []);
});
