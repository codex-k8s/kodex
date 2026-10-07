import assert from "node:assert/strict";
import { constants } from "node:fs";
import { test } from "node:test";
import {
  agentSecretPath,
  parseSelectedAgentSecrets,
  readPrivateAgentSecrets,
} from "./private-agent-secret-reader.mjs";

const key = "CONTEXT7_API_KEY";
const sentinel = "SYNTHETIC_SECRET_SENTINEL";

function fixture(source, changes = {}) {
  const input = Buffer.from(source);
  const info = {
    isFile: () => true,
    uid: 1001,
    mode: 0o100600,
    nlink: 1,
    size: input.length,
    ino: 77,
    dev: 9,
    mtimeMs: 1,
    ctimeMs: 1,
    ...changes,
  };
  let position = 0;
  let closed = 0;
  let buffer;
  return {
    io: {
      uid: 1001,
      open(path, flags) {
        assert.equal(path, agentSecretPath);
        assert.ok(flags & constants.O_NOFOLLOW);
        assert.ok(flags & constants.O_NONBLOCK);
        return 7;
      },
      stat: () => info,
      read(_descriptor, target, offset, length) {
        buffer = target;
        const count = Math.min(length, input.length - position);
        input.copy(target, offset, position, position + count);
        position += count;
        return count;
      },
      close: () => {
        closed++;
      },
    },
    closed: () => closed,
    buffer: () => buffer,
  };
}

test("reader выбирает только закрытые ключи и очищает буфер", () => {
  const f = fixture(`OTHER_TOKEN=not-exported\n${key}='${sentinel}'\n`);
  const selected = readPrivateAgentSecrets([key], f.io);
  assert.deepEqual(Object.keys(selected), [key]);
  assert.equal(selected[key], sentinel);
  assert.equal(f.closed(), 1);
  assert.ok(f.buffer().every((byte) => byte === 0));
});

test("dotenv не исполняет shell и не расширяет переменные", () => {
  const selected = parseSelectedAgentSecrets(
    `export ${key}='$(printf forbidden) $HOME \`command\`' # comment\n`,
    [key],
  );
  assert.equal(selected[key], "$(printf forbidden) $HOME `command`");
  const multiline = parseSelectedAgentSecrets(
    `OTHER="foreign\n${key}=not-an-assignment"\n${key}="one\ntwo"\n`,
    [key],
  );
  assert.equal(multiline[key], "one\ntwo");
});

test("неизвестные, отсутствующие и повторяющиеся ключи закрыто отклоняются", () => {
  for (const [source, keys] of [
    [`${key}=value\n`, ["GIT_OWNER_PAT"]],
    [`${key}=value\n`, []],
    [`${key}=value\n`, [key, key]],
    ["OTHER=foreign\n", [key]],
    [`${key}=first\n${key}=second\n`, [key]],
    [`${key}=\n`, [key]],
    [`${key}='unterminated\n`, [key]],
    [`${key}='value' trailing\n`, [key]],
    [`${key}=\0${sentinel}`, [key]],
    ["not dotenv", [key]],
  ])
    assert.throws(
      () => parseSelectedAgentSecrets(source, keys),
      /^Error: SECRET_INPUT_INVALID$/,
    );
  assert.throws(
    () =>
      readPrivateAgentSecrets(["GIT_OWNER_PAT"], {
        open: () => assert.fail("unknown key must not open file"),
      }),
    /^Error: SECRET_INPUT_INVALID$/,
  );
});

test("все метаданные файла проверяются до чтения", () => {
  for (const changes of [
    { mode: 0o100644 },
    { mode: 0o100400 },
    { uid: 1002 },
    { nlink: 2 },
    { isFile: () => false },
    { size: 0 },
    { size: (1 << 20) + 1 },
  ]) {
    const f = fixture(`${key}=${sentinel}\n`, changes);
    f.io.read = () => assert.fail("invalid metadata must not read");
    assert.throws(
      () => readPrivateAgentSecrets([key], f.io),
      /^Error: SECRET_INPUT_INVALID$/,
    );
    assert.equal(f.closed(), 1);
  }
});

test("ошибки открытия/symlink и изменение файла не раскрывают сырой текст", () => {
  assert.throws(
    () =>
      readPrivateAgentSecrets([key], {
        open: () => {
          throw new Error(sentinel);
        },
      }),
    /^Error: SECRET_INPUT_INVALID$/,
  );
  const f = fixture(`${key}=${sentinel}\n`);
  let calls = 0;
  const stat = f.io.stat;
  f.io.stat = () => ({ ...stat(), mtimeMs: ++calls });
  assert.throws(
    () => readPrivateAgentSecrets([key], f.io),
    /^Error: SECRET_INPUT_INVALID$/,
  );
  assert.ok(f.buffer().every((byte) => byte === 0));
});
