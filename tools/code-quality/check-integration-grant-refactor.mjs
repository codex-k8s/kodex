// Одноразовая воспроизводимая differential-проверка refactor из #1823.
// Исторические исходники читаются из Git, не копируются в product/tests.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import vm from "node:vm";

const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const require = createRequire(
  path.join(root, "services/staff/control-center/package.json"),
);
const ts = require("typescript");
const baseline = "dc881e2e8447f1f75a29f161574db7889e285c25";
const relativeBase = "services/staff/control-center/src/features/assistant/";
const base = path.join(root, relativeBase);
const originals = Object.fromEntries(
  ["system", "project"].map((scope) => [
    scope,
    execFileSync(
      "git",
      [
        "show",
        baseline + ":" + relativeBase + scope + "-integration-grant-plan.ts",
      ],
      { cwd: root, encoding: "utf8", timeout: 10_000 },
    ),
  ]),
);
function moduleOf(source, shared) {
  const context = {
    structuredClone,
    exports: {},
    require: (name) => {
      if (name !== "./integration-grant-plan-fields")
        throw Error("Unexpected runtime import");
      return shared;
    },
  };
  const result = ts.transpileModule(source, {
    reportDiagnostics: true,
    compilerOptions: {
      target: ts.ScriptTarget.ES2022,
      module: ts.ModuleKind.CommonJS,
    },
  });
  if (
    result.diagnostics?.some(
      (item) =>
        item.category === ts.DiagnosticCategory.Error ||
        item.category === ts.DiagnosticCategory.Warning,
    )
  )
    throw Error("Comparison source has compiler diagnostics");
  vm.runInNewContext(result.outputText, context, { timeout: 2000 });
  return context.exports;
}
const shared = moduleOf(
  fs.readFileSync(base + "integration-grant-plan-fields.ts", "utf8"),
);
const values = [
  null,
  undefined,
  false,
  true,
  0,
  1,
  -1,
  Number.MAX_SAFE_INTEGER,
  Number.MAX_SAFE_INTEGER + 1,
  "",
  "foreign",
  "a".repeat(64),
  [],
  ["NONE"],
  ["NONE", "NONE"],
  {},
  ["x".repeat(161)],
];
let count = 0;
function outcome(fn, args) {
  try {
    return { value: fn(...args) };
  } catch (e) {
    return { error: e.name };
  }
}
function compare(a, b, args) {
  const x = outcome(a, args),
    y = outcome(b, args);
  count++;
  if (x.error !== y.error || !Object.is(x.value, y.value))
    throw Error("Differential mismatch at " + count);
}
for (const scope of ["system", "project"]) {
  const a = moduleOf(originals[scope]),
    b = moduleOf(
      fs.readFileSync(base + scope + "-integration-grant-plan.ts", "utf8"),
      shared,
    );
  const test = fs.readFileSync(
    base + scope + "-integration-grant-plan.test.ts",
    "utf8",
  );
  const fixture = moduleOf(
    test.slice(test.indexOf("const pins ="), test.indexOf("describe(")) +
      "\nexports.fixture={pins,before,operation,candidate,page};",
  );
  const f = fixture.fixture;
  const owner = scope + "IntegrationGrantPlanOwner",
    fresh = scope + "IntegrationGrantPlanCandidate",
    applied = scope + "IntegrationGrantAppliedCandidate",
    receipt = scope + "IntegrationGrantReceiptRef";
  for (const name of [owner, fresh, applied, receipt]) {
    if (typeof a[name] !== "function" || typeof b[name] !== "function")
      throw Error("Comparison function is missing");
  }
  const appliedCandidate = {
    ...structuredClone(f.candidate),
    currentGrantRef: "grant_applied",
    currentGrantVersion: 1,
    currentGrantEnabled: true,
    currentApprovalPolicy: f.operation().parameters.approvalPolicy,
  };
  for (const source of [a, b]) {
    if (
      source[owner](f.operation(), f.pins.organizationRef) !== true ||
      source[fresh](f.operation(), f.page, f.candidate) !== true ||
      source[applied](
        f.operation(),
        f.page,
        appliedCandidate,
        "grant_applied",
      ) !== true
    )
      throw Error("Positive comparison fixture was rejected");
  }
  const check = (op) => {
    compare(a[owner], b[owner], [op, f.pins.organizationRef]);
    compare(a[fresh], b[fresh], [op, f.page, f.candidate]);
    compare(a[applied], b[applied], [
      op,
      f.page,
      appliedCandidate,
      "grant_applied",
    ]);
  };
  check(f.operation());
  for (const area of ["parameters", "before", "after", "target"]) {
    for (const key of Object.keys(f.operation()[area])) {
      for (const value of values) {
        const op = f.operation();
        op[area][key] = value;
        check(op);
      }
    }
    for (const value of values) {
      const op = f.operation();
      op[area].unexpected = value;
      check(op);
    }
  }
  for (const field of ["type", "action", "expectedVersion", "selected"]) {
    for (const value of values) {
      const op = f.operation();
      op[field] = value;
      check(op);
    }
  }
  for (const area of ["page", "candidate", "capability"]) {
    const original = area === "capability" ? f.candidate.capability : f[area];
    for (const key of Object.keys(original)) {
      for (const value of values) {
        const page = structuredClone(f.page),
          candidate = structuredClone(f.candidate),
          appliedValue = structuredClone(appliedCandidate);
        (area === "page"
          ? page
          : area === "candidate"
            ? candidate
            : candidate.capability)[key] = value;
        compare(a[fresh], b[fresh], [f.operation(), page, candidate]);
        if (area !== "page")
          (area === "candidate" ? appliedValue : appliedValue.capability)[key] =
            value;
        compare(a[applied], b[applied], [
          f.operation(),
          page,
          appliedValue,
          "grant_applied",
        ]);
      }
    }
  }
  const op = f.operation();
  const r = {
    ref: "receipt_test",
    planRef: "plan_test",
    planRevision: 2,
    outcome: "APPLIED",
    operationReceipts: [
      {
        operationRef: op.ref,
        resourceRef: "grant_applied",
        outcome: "APPLIED",
      },
    ],
  };
  const p = {
    ref: "plan_test",
    revision: 2,
    state: "APPLIED",
    operations: [op],
    receipt: r,
  };
  for (const source of [a, b]) {
    if (source[receipt](p, op.ref) !== "grant_applied")
      throw Error("Positive receipt fixture was rejected");
  }
  compare(a[receipt], b[receipt], [p, op.ref]);
  for (const area of ["plan", "receipt", "operation"]) {
    const original = area === "plan" ? p : area === "receipt" ? r : op;
    for (const key of Object.keys(original)) {
      for (const value of values) {
        const plan = structuredClone(p);
        (area === "plan"
          ? plan
          : area === "receipt"
            ? plan.receipt
            : plan.operations[0])[key] = value;
        compare(a[receipt], b[receipt], [plan, op.ref]);
      }
    }
  }
}
console.log(
  JSON.stringify({
    status: "PASS",
    comparisons: count,
    scope:
      "SYSTEM/PROJECT owner,candidate,applied,receipt; malformed fields, unknown keys, types and bounds",
    baseline,
  }),
);
