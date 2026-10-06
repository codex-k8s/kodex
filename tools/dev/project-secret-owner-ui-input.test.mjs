import test from "node:test";
import assert from "node:assert/strict";
import {
  mkdtempSync,
  writeFileSync,
  chmodSync,
  symlinkSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import vm from "node:vm";
import {
  validateInputProfile,
  readInputProfile,
  assertInputEnvironment,
  assertChromeEndpoint,
  assertOwnerPins,
  readOwnerPins,
  assertMaskedForm,
  fillProjectSecret,
  main,
  secretInputIntent,
  secretInputKey,
} from "./project-secret-owner-ui-input.mjs";

const profile = {
  cdpEndpoint: "http://127.0.0.1:9222",
  chromePID: 1234,
  chromeStartTicks: "4321",
  pageURL: "https://kodex.127.0.0.2.nip.io/projects/prj_synthetic/secrets",
  organizationRef: "org_synthetic",
  ownerRef: "usr_synthetic",
  projectRef: "prj_synthetic",
  environmentRef: "renv_synthetic",
  environmentVersion: 2,
  versionRef: "renvv_synthetic",
  environmentDigest: "a".repeat(64),
  agentRef: "agt_synthetic",
  agentVersion: 5,
  bindingRef: "aenv_synthetic",
  bindingVersion: 3,
  bindingDigest: "b".repeat(64),
};
function proof() {
  return {
    ownerRef: profile.ownerRef,
    organizationRef: profile.organizationRef,
    platformRole: "OWNER",
    environment: {
      ref: profile.environmentRef,
      organizationRef: profile.organizationRef,
      projectRef: profile.projectRef,
      scopeKind: "PROJECT",
      name: "selfdev-write",
      state: "ACTIVE",
      version: profile.environmentVersion,
      ready: true,
      revisionRef: profile.versionRef,
      digest: profile.environmentDigest,
    },
    agentVersion: profile.agentVersion,
    binding: {
      ref: profile.bindingRef,
      version: profile.bindingVersion,
      agentRef: profile.agentRef,
      environmentRef: profile.environmentRef,
      versionRef: profile.versionRef,
      digest: profile.bindingDigest,
    },
    impact: {
      environmentRef: profile.environmentRef,
      environmentVersion: profile.environmentVersion,
      targetVersionRef: profile.versionRef,
      targetDigest: profile.environmentDigest,
      total: 1,
      nextPageToken: "",
      consumers: [
        {
          agentRef: profile.agentRef,
          agentVersion: profile.agentVersion,
          bindingRef: profile.bindingRef,
          bindingVersion: profile.bindingVersion,
          versionRef: profile.versionRef,
          projectRef: profile.projectRef,
          scopeKind: "PROJECT",
          organizationRef: profile.organizationRef,
        },
      ],
    },
    secretAbsent: true,
  };
}
const sentinel = "synthetic-private-Git-token-never-log";
function fixture(options = {}) {
  const calls = [];
  const selected = { [secretInputKey]: sentinel };
  const field = {
    count: async () => options.count ?? 1,
    isVisible: async () => true,
    isEnabled: async () => true,
    fill: async (value) => {
      assert.equal(value, sentinel);
      calls.push("fill");
      if (options.fillFailure) throw new Error(sentinel);
    },
  };
  field.elementHandle = async () => (options.missingHandle ? null : field);
  const page = {
    url: () => options.url ?? profile.pageURL,
    evaluate: async () => options.masked ?? true,
    locator: () => field,
  };
  const browser = {
    contexts: () => [
      { pages: () => (options.duplicate ? [page, page] : [page]) },
    ],
    close: async () => calls.push("disconnect"),
  };
  let reads = 0;
  const dependencies = {
    environment: {},
    argumentsList: [],
    connect: async () => {
      calls.push("connect");
      return browser;
    },
    assertEndpoint: () => calls.push("endpoint"),
    readPins: async () => {
      calls.push("read");
      return options.readPins ? options.readPins(reads++) : proof();
    },
    readSecrets: (keys) => {
      assert.deepEqual(keys, [secretInputKey]);
      calls.push("secret");
      return selected;
    },
  };
  return { calls, selected, dependencies, page };
}

test("Profile/CLI: exact local origin и pins, без произвольного secret/key/URL", async () => {
  assert.equal(validateInputProfile(profile), profile);
  for (const patch of [
    { ownerToken: "synthetic" },
    { cdpEndpoint: "http://0.0.0.0:9222" },
    { cdpEndpoint: "http://user:pass@127.0.0.1:9222" },
    { pageURL: profile.pageURL + "?other=1" },
    { environmentDigest: "bad" },
    { agentVersion: 0 },
    { chromeStartTicks: "0" },
  ])
    assert.throws(() => validateInputProfile({ ...profile, ...patch }));
  const output = [];
  const code = await main(
    ["--profile", "/private/profile.json", "--confirm", "wrong"],
    { output: (line) => output.push(line), environment: {}, argumentsList: [] },
  );
  assert.equal(code, 1);
  assert(!output.join("").includes(sentinel));
});

test("Profile private0600/0700: regular file и canonical JSON, отказ duplicate/symlink", () => {
  const directory = mkdtempSync(join(tmpdir(), "kodex-input-profile-"));
  const file = join(directory, "profile.json");
  try {
    writeFileSync(file, JSON.stringify(profile, null, 2) + "\n", {
      mode: 0o600,
    });
    assert.deepEqual(readInputProfile(file), profile);
    chmodSync(file, 0o640);
    assert.throws(() => readInputProfile(file));
    chmodSync(file, 0o600);
    symlinkSync(file, join(directory, "alias"));
    assert.throws(() => readInputProfile(join(directory, "alias")));
    writeFileSync(file, '{"ownerRef":"first","ownerRef":"second"}\n');
    assert.throws(() => readInputProfile(file));
    chmodSync(directory, 0o755);
    assert.throws(() => readInputProfile(file));
  } finally {
    rmSync(directory, { recursive: true });
  }
});

test("Same-UID Chrome PID/start/socket: отказ wildcard и чужому endpoint", () => {
  const values = {
    uid: 1000,
    comm: "chrome\n",
    start: "4321",
    address: "0100007F",
    socketUID: 1000,
    inode: "12345",
    owns: true,
  };
  function io() {
    return {
      uid: 1000,
      stat: () => ({ uid: values.uid }),
      readdir: () => ["4"],
      readlink: () => (values.owns ? "socket:[12345]" : "socket:[other]"),
      readFile: (path) =>
        path.endsWith("/comm")
          ? values.comm
          : path.endsWith("/stat")
            ? `1234 (chrome) ${[...Array(19).fill("0"), values.start].join(" ")}`
            : path.endsWith("tcp6")
              ? "header\n"
              : `header\n 0: ${values.address}:2406 00000000:0000 0A 0 0 0 ${values.socketUID} 0 ${values.inode}\n`,
    };
  }
  assert.doesNotThrow(() => assertChromeEndpoint(profile, io()));
  for (const [key, value] of [
    ["uid", 999],
    ["comm", "foreign"],
    ["start", "4322"],
    ["address", "00000000"],
    ["socketUID", 999],
    ["owns", false],
  ]) {
    const old = values[key];
    values[key] = value;
    assert.throws(() => assertChromeEndpoint(profile, io()));
    values[key] = old;
  }
});

test("Каждый owner/scope/OCC/sole-consumer pin проверяется до чтения секрета", async () => {
  const changes = [
    (v) => (v.platformRole = "ADMINISTRATOR"),
    (v) => (v.ownerRef = "usr_foreign"),
    (v) => (v.organizationRef = "org_foreign"),
    (v) => (v.environment.projectRef = "prj_foreign"),
    (v) => (v.environment.scopeKind = "ORGANIZATION"),
    (v) => (v.environment.name = "selfdev-review"),
    (v) => (v.environment.ready = false),
    (v) => (v.environment.state = "DISABLED"),
    (v) => v.environment.version++,
    (v) => (v.environment.revisionRef = "renvv_foreign"),
    (v) => (v.environment.digest = "c".repeat(64)),
    (v) => v.agentVersion++,
    (v) => (v.binding.ref = "aenv_foreign"),
    (v) => v.binding.version++,
    (v) => (v.binding.agentRef = "agt_foreign"),
    (v) => (v.binding.versionRef = "renvv_foreign"),
    (v) => (v.binding.digest = "c".repeat(64)),
    (v) => (v.impact.total = 2),
    (v) => (v.impact.nextPageToken = "next"),
    (v) => v.impact.consumers.push(v.impact.consumers[0]),
    (v) => (v.impact.consumers[0].agentRef = "agt_foreign"),
    (v) => (v.impact.consumers[0].organizationRef = "org_foreign"),
    (v) => (v.impact.consumers[0].versionRef = "renvv_foreign"),
    (v) => (v.secretAbsent = false),
  ];
  for (const change of changes) {
    const value = proof();
    change(value);
    assert.throws(() => assertOwnerPins(value, profile));
    const f = fixture({ readPins: () => value });
    assert.equal(
      (await fillProjectSecret(profile, f.dependencies)).code,
      "PROJECT_SECRET_INPUT_REJECTED",
    );
    assert(!f.calls.includes("secret"));
    assert(!f.calls.includes("fill"));
  }
});

test("Fill только после двух owner proof: без submit/reveal/navigation или закрытия Chrome", async () => {
  const f = fixture();
  const result = await fillProjectSecret(profile, f.dependencies);
  assert.deepEqual(result, {
    status: "FILLED",
    code: "PROJECT_DEVELOPER_SECRET_INPUT_FILLED",
  });
  assert.deepEqual(f.calls, [
    "endpoint",
    "connect",
    "read",
    "read",
    "endpoint",
    "secret",
    "fill",
    "disconnect",
  ]);
  assert.equal(f.selected[secretInputKey], "");
  assert(!JSON.stringify(result).includes(sentinel));
});

test("Чужая/duplicate page, немаскированная форма, drift и debug не читают секрет", async () => {
  for (const options of [
    { url: profile.pageURL + "?other=1" },
    { duplicate: true },
    { masked: false },
    { count: 2 },
    { missingHandle: true },
    {
      readPins: (index) => {
        const v = proof();
        if (index) v.environment.version++;
        return v;
      },
    },
  ]) {
    const f = fixture(options);
    assert.equal(
      (await fillProjectSecret(profile, f.dependencies)).status,
      "FAIL",
    );
    assert(!f.calls.includes("secret"));
  }
  for (const key of [
    "DEBUG",
    "PWDEBUG",
    "PW_TEST_HTML_REPORT",
    "PLAYWRIGHT_JSON_OUTPUT_NAME",
    "NODE_OPTIONS",
    "NODE_DEBUG",
    "SSLKEYLOGFILE",
  ])
    assert.throws(() => assertInputEnvironment({ [key]: "enabled" }));
  assert.throws(() => assertInputEnvironment({}, ["--inspect"]));
});

test("Ошибка fill/reader возвращает только closed code, без token/cause", async () => {
  for (const mode of ["fill", "secret"]) {
    const f = fixture({ fillFailure: mode === "fill" });
    if (mode === "secret")
      f.dependencies.readSecrets = () => {
        throw new Error(sentinel);
      };
    const output = [];
    const code = await main(
      ["--profile", "/private/profile.json", "--confirm", secretInputIntent],
      {
        ...f.dependencies,
        readProfile: () => profile,
        output: (line) => output.push(line),
      },
    );
    assert.equal(code, 1);
    assert(!output.join("").includes(sentinel));
    assert.equal(
      JSON.parse(output[0]).code,
      mode === "fill"
        ? "PROJECT_SECRET_INPUT_UNKNOWN"
        : "PROJECT_SECRET_INPUT_REJECTED",
    );
  }
});

test("Navigation после private read не переадресует token; detached DOM handle не повторяется", async () => {
  const f = fixture();
  f.dependencies.readSecrets = (keys) => {
    assert.deepEqual(keys, [secretInputKey]);
    f.page.url = () => "https://foreign.invalid";
    return f.selected;
  };
  const result = await fillProjectSecret(profile, f.dependencies);
  assert.equal(result.code, "PROJECT_SECRET_INPUT_REJECTED");
  assert(!f.calls.includes("fill"));
  assert.equal(f.selected[secretInputKey], "");
  const detached = fixture();
  detached.page.locator = () => ({
    count: async () => 1,
    isVisible: async () => true,
    isEnabled: async () => true,
    elementHandle: async () => ({
      fill: async () => {
        detached.calls.push("detached");
        throw new Error(sentinel);
      },
    }),
    fill: async () => assert.fail("Locator retarget запрещён"),
  });
  assert.equal(
    (await fillProjectSecret(profile, detached.dependencies)).code,
    "PROJECT_SECRET_INPUT_UNKNOWN",
  );
  assert.equal(
    detached.calls.filter((value) => value === "detached").length,
    1,
  );
});

test("Production GET projection отбрасывает private body, без mutation/redirect/reveal", async () => {
  const v = proof(),
    paths = [];
  const env = {
    ...v.environment,
    currentVersion: {
      ref: profile.versionRef,
      digest: profile.environmentDigest,
    },
    unexpected: sentinel,
  };
  const responses = [
    {
      currentUser: { ref: profile.ownerRef },
      organizationRef: profile.organizationRef,
      platformRole: "OWNER",
      unknown: sentinel,
    },
    env,
    {
      agentVersion: profile.agentVersion,
      environmentBinding: { ...v.binding, unknown: sentinel },
    },
    {
      ...v.impact,
      consumers: v.impact.consumers.map((item) => ({
        ...item,
        unknown: sentinel,
      })),
    },
    { items: [], nextPageToken: "", unknown: sentinel },
  ];
  const context = {
    location: new URL(profile.pageURL),
    AbortSignal,
    TextDecoder,
    Uint8Array,
    fetch: async (path, options) => {
      paths.push(path);
      assert.equal(options.method, "GET");
      assert.equal(options.redirect, "error");
      assert.equal(options.credentials, "same-origin");
      assert(!Object.hasOwn(options, "body"));
      const payload = responses[paths.length - 1];
      const bytes = new TextEncoder().encode(JSON.stringify(payload));
      let read = false;
      return {
        status: 200,
        url: new URL(path, profile.pageURL).href,
        body: {
          getReader: () => ({
            read: async () =>
              read
                ? { done: true }
                : ((read = true), { value: bytes, done: false }),
            cancel: async () => {},
          }),
        },
      };
    },
  };
  context.location.href = profile.pageURL;
  const page = {
    evaluate: async (fn, argument) =>
      vm.runInNewContext(`(${fn})`, context)(argument),
  };
  const result = await readOwnerPins(page, profile);
  assertOwnerPins(result, profile);
  assert(!JSON.stringify(result).includes(sentinel));
  assert.equal(paths.length, 5);
  assert(paths.every((path) => !path.includes("reveal")));
});

test("Actual DOM требует единственную пустую masked GH_TOKEN/STRING форму", async () => {
  const props = {
    name: "GH_TOKEN",
    type: "STRING",
    empty: true,
    masked: true,
    disabled: false,
    mask: "disc",
  };
  const value = {
    get value() {
      return props.empty ? "" : sentinel;
    },
    get disabled() {
      return props.disabled;
    },
    readOnly: false,
    classList: { contains: () => props.masked },
    closest: () => ({
      querySelectorAll: (selector) => [
        {
          value: selector.startsWith("input") ? props.name : props.type,
          disabled: false,
        },
      ],
    }),
  };
  const context = {
    document: { querySelectorAll: () => [value] },
    getComputedStyle: () => ({ webkitTextSecurity: props.mask }),
  };
  const f = fixture();
  f.page.evaluate = async (fn) => vm.runInNewContext(`(${fn})`, context)();
  await assertMaskedForm(f.page, profile);
  for (const [key, changed] of [
    ["name", "OTHER"],
    ["type", "JSON"],
    ["empty", false],
    ["masked", false],
    ["disabled", true],
    ["mask", "none"],
  ]) {
    const old = props[key];
    props[key] = changed;
    await assert.rejects(() => assertMaskedForm(f.page, profile));
    props[key] = old;
  }
});
