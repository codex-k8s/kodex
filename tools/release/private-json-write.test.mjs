import assert from "node:assert/strict";
import {
  lstatSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import test from "node:test";
import {
  appendPrivateJSONLineSync,
  createPrivateJSON,
  createPrivatePrettyJSONSync,
} from "./private-json-write.mjs";

test("pretty и compact JSON сохраняют прежние байты и эксклюзивное создание 0600", async (t) => {
  const root = mkdtempSync(join(tmpdir(), "kodex-release-json-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const value = { action: "Сохранить", nested: { version: 2 } };
  const pretty = join(root, "pretty.json");
  createPrivatePrettyJSONSync(pretty, value);
  assert.equal(
    readFileSync(pretty, "utf8"),
    JSON.stringify(value, null, 2) + "\n",
  );
  assert.equal(lstatSync(pretty).mode & 0o777, 0o600);
  assert.throws(() => createPrivatePrettyJSONSync(pretty, {}), {
    code: "EEXIST",
  });
  const compact = join(root, "compact.json");
  await createPrivateJSON(compact, value);
  assert.equal(readFileSync(compact, "utf8"), JSON.stringify(value) + "\n");
  assert.equal(lstatSync(compact).mode & 0o777, 0o600);
  await assert.rejects(createPrivateJSON(compact, {}), { code: "EEXIST" });
  assert.equal(
    readFileSync(pretty, "utf8"),
    JSON.stringify(value, null, 2) + "\n",
  );
});

test("журнал не создаётся при resume и append не следует symlink", (t) => {
  const root = mkdtempSync(join(tmpdir(), "kodex-release-journal-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const path = join(root, "journal.jsonl");
  assert.throws(() => appendPrivateJSONLineSync(path, {}), { code: "ENOENT" });
  appendPrivateJSONLineSync(path, { state: "INTENT" }, true);
  appendPrivateJSONLineSync(path, { state: "UNKNOWN" });
  const expected = '{"state":"INTENT"}\n{"state":"UNKNOWN"}\n';
  assert.equal(readFileSync(path, "utf8"), expected);
  assert.equal(lstatSync(path).mode & 0o777, 0o600);
  assert.throws(() => appendPrivateJSONLineSync(path, {}, true), {
    code: "EEXIST",
  });
  const link = join(root, "link");
  symlinkSync(path, link);
  assert.throws(() => appendPrivateJSONLineSync(link, {}), { code: "ELOOP" });
  assert.throws(() => createPrivatePrettyJSONSync(link, {}), {
    code: "EEXIST",
  });
  assert.equal(readFileSync(path, "utf8"), expected);
});

test("ошибка сериализации не превращается в успешный receipt и не перезаписывает файл", async (t) => {
  const root = mkdtempSync(join(tmpdir(), "kodex-release-invalid-json-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const value = {};
  value.circular = value;
  const pretty = join(root, "pretty.json");
  assert.throws(() => createPrivatePrettyJSONSync(pretty, value), TypeError);
  assert.equal(readFileSync(pretty).length, 0);
  assert.throws(() => createPrivatePrettyJSONSync(pretty, {}), {
    code: "EEXIST",
  });
  const compact = join(root, "compact.json");
  await assert.rejects(createPrivateJSON(compact, value), TypeError);
  assert.equal(readFileSync(compact).length, 0);
  await assert.rejects(createPrivateJSON(compact, {}), { code: "EEXIST" });
});
