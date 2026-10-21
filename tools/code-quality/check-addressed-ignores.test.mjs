import assert from "node:assert/strict";
import crypto from "node:crypto";
import test from "node:test";
import { checkAddressedIgnores } from "./check-addressed-ignores.mjs";

const file = "libs/go/unit/source.go";
const body = "// Отдельная причина здесь.\nfunc typedBoundary() {}";
const source = `// jscpd:ignore-start\n${body}\n// jscpd:ignore-end\n`;
const region = {
  path: file,
  maxLines: 2,
  sha256: crypto.createHash("sha256").update(body).digest("hex"),
  reason:
    "Разные типизированные сигнатуры не представляют общий алгоритм и сохраняют точную границу.",
};
const registry = { version: 1, regions: [region] };

test("requires every registered region to match its reviewed bytes and bound", () => {
  assert.deepEqual(checkAddressedIgnores(new Map([[file, source]]), registry), {
    checked: 1,
  });
  assert.throws(() =>
    checkAddressedIgnores(
      new Map([[file, source.replace("typedBoundary", "otherBoundary")]]),
      registry,
    ),
  );
  assert.throws(() =>
    checkAddressedIgnores(new Map([[file, source]]), {
      version: 1,
      regions: [{ ...region, maxLines: 1 }],
    }),
  );
});

test("rejects unregistered, absent, nested and unpaired ignore markers", () => {
  assert.throws(() =>
    checkAddressedIgnores(new Map([[file, source]]), {
      version: 1,
      regions: [],
    }),
  );
  assert.throws(() =>
    checkAddressedIgnores(new Map([[file, "func plain() {}"]]), registry),
  );
  assert.throws(() =>
    checkAddressedIgnores(
      new Map([[file, source.replace(body, `// jscpd:ignore-start\n${body}`)]]),
      registry,
    ),
  );
  assert.throws(() =>
    checkAddressedIgnores(
      new Map([[file, source.replace("// jscpd:ignore-end", "")]]),
      registry,
    ),
  );
});
