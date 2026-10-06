import {
  constants,
  openSync,
  fstatSync,
  readFileSync,
  closeSync,
  lstatSync,
  readdirSync,
  readlinkSync,
} from "node:fs";
import { dirname } from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";
import { readPrivateAgentSecrets } from "./private-agent-secret-reader.mjs";

export const secretInputIntent = "FILL_PROJECT_DEVELOPER_GH_TOKEN";
export const secretInputKey = "CODEX_GITHUB_AGENT_GIT_TOKEN";
const origin = "https://kodex.127.0.0.2.nip.io";
const maskedField =
  'textarea[name^="runtime-secret-"][name$="-value"].secret-form__masked';
const profileFields = [
  "cdpEndpoint",
  "chromePID",
  "chromeStartTicks",
  "pageURL",
  "organizationRef",
  "ownerRef",
  "projectRef",
  "environmentRef",
  "environmentVersion",
  "versionRef",
  "environmentDigest",
  "agentRef",
  "agentVersion",
  "bindingRef",
  "bindingVersion",
  "bindingDigest",
];
const ref = (value) =>
  typeof value === "string" && /^[A-Za-z][A-Za-z0-9_-]{7,127}$/.test(value);
const version = (value) => Number.isSafeInteger(value) && value > 0;
const digest = (value) =>
  typeof value === "string" && /^[a-f0-9]{64}$/.test(value);
function requireInput(valid, code = "PROJECT_SECRET_INPUT_REJECTED") {
  if (!valid) throw new Error(code);
}

export function validateInputProfile(value) {
  requireInput(
    value &&
      Object.keys(value).length === profileFields.length &&
      profileFields.every((key) => Object.hasOwn(value, key)),
  );
  for (const key of [
    "organizationRef",
    "ownerRef",
    "projectRef",
    "environmentRef",
    "versionRef",
    "agentRef",
    "bindingRef",
  ])
    requireInput(ref(value[key]));
  for (const key of [
    "chromePID",
    "environmentVersion",
    "agentVersion",
    "bindingVersion",
  ])
    requireInput(version(value[key]));
  requireInput(
    typeof value.chromeStartTicks === "string" &&
      /^[1-9][0-9]{0,19}$/.test(value.chromeStartTicks),
  );
  requireInput(digest(value.environmentDigest) && digest(value.bindingDigest));
  requireInput(
    value.pageURL === `${origin}/projects/${value.projectRef}/secrets`,
  );
  const endpoint = new URL(value.cdpEndpoint);
  requireInput(
    endpoint.protocol === "http:" &&
      endpoint.hostname === "127.0.0.1" &&
      endpoint.port &&
      endpoint.pathname === "/" &&
      !endpoint.username &&
      !endpoint.password &&
      !endpoint.search &&
      !endpoint.hash,
  );
  requireInput(value.cdpEndpoint === endpoint.origin);
  return value;
}

export function readInputProfile(path) {
  requireInput(
    typeof path === "string" && path.startsWith("/") && !path.includes("\0"),
  );
  const directory = lstatSync(dirname(path));
  requireInput(
    directory.isDirectory() &&
      !directory.isSymbolicLink() &&
      directory.uid === process.getuid() &&
      (directory.mode & 0o777) === 0o700,
  );
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const info = fstatSync(fd);
    requireInput(
      info.isFile() &&
        info.uid === process.getuid() &&
        (info.mode & 0o777) === 0o600 &&
        info.size > 0 &&
        info.size <= 16384,
    );
    const bytes = readFileSync(fd);
    requireInput(bytes.length === info.size);
    const text = bytes.toString("utf8");
    requireInput(Buffer.from(text).equals(bytes));
    const value = JSON.parse(text);
    // Каноническое представление исключает duplicate keys без второго parser.
    requireInput(text === `${JSON.stringify(value, null, 2)}\n`);
    return validateInputProfile(value);
  } finally {
    closeSync(fd);
  }
}

export function assertInputEnvironment(environment, argumentsList = []) {
  requireInput(argumentsList.length === 0);
  requireInput(
    !Object.entries(environment).some(
      ([key, value]) =>
        value &&
        (/^(DEBUG|NODE_OPTIONS|NODE_DEBUG|NODE_DEBUG_NATIVE|SSLKEYLOGFILE)$/.test(
          key,
        ) ||
          /^(PW|PLAYWRIGHT_)/.test(key)),
    ),
  );
}

export function assertChromeEndpoint(profile, io = {}) {
  const read = io.readFile ?? ((path) => readFileSync(path, "utf8"));
  const stat = io.stat ?? lstatSync;
  const link = io.readlink ?? readlinkSync;
  const names = io.readdir ?? readdirSync;
  const uid = io.uid ?? process.getuid();
  const root = `/proc/${profile.chromePID}`;
  requireInput(stat(root).uid === uid);
  requireInput(["chrome", "chromium"].includes(read(`${root}/comm`).trim()));
  const record = read(`${root}/stat`);
  requireInput(
    record.startsWith(`${profile.chromePID} (`) &&
      record.slice(record.lastIndexOf(")") + 2).split(" ")[19] ===
        profile.chromeStartTicks,
  );
  const port = Number(new URL(profile.cdpEndpoint).port)
    .toString(16)
    .padStart(4, "0")
    .toUpperCase();
  const sockets = ["tcp", "tcp6"]
    .flatMap((kind) =>
      read(`${root}/net/${kind}`)
        .trim()
        .split("\n")
        .slice(1)
        .map((line) => line.trim().split(/\s+/)),
    )
    .filter((fields) => fields[3] === "0A" && fields[1]?.endsWith(`:${port}`));
  requireInput(
    sockets.length === 1 &&
      sockets[0][1] === `0100007F:${port}` &&
      Number(sockets[0][7]) === uid &&
      /^[1-9][0-9]*$/.test(sockets[0][9]),
  );
  const expected = `socket:[${sockets[0][9]}]`;
  requireInput(
    names(`${root}/fd`).some((name) => {
      try {
        return link(`${root}/fd/${name}`) === expected;
      } catch {
        return false;
      }
    }),
  );
}

// Только canonical GET; наружу выходит закрытая metadata projection, не body.
export async function readOwnerPins(page, profile) {
  return page.evaluate(async (pins) => {
    if (location.href !== pins.pageURL)
      throw new Error("PROJECT_SECRET_INPUT_REJECTED");
    async function get(path) {
      const response = await fetch(path, {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        redirect: "error",
        headers: { "X-Kodex-Project-ID": pins.projectRef },
        signal: AbortSignal.timeout(5000),
      });
      if (
        response.status !== 200 ||
        response.url !== `${location.origin}${path}`
      )
        throw new Error("PROJECT_SECRET_INPUT_REJECTED");
      const reader = response.body.getReader();
      const chunks = [];
      let size = 0;
      try {
        for (;;) {
          const item = await reader.read();
          if (item.done) break;
          size += item.value.length;
          if (size > 1048576) throw new Error("PROJECT_SECRET_INPUT_REJECTED");
          chunks.push(item.value);
        }
      } finally {
        await reader.cancel();
      }
      const bytes = new Uint8Array(size);
      let offset = 0;
      for (const chunk of chunks) {
        bytes.set(chunk, offset);
        offset += chunk.length;
      }
      return JSON.parse(
        new TextDecoder("utf-8", { fatal: true }).decode(bytes),
      );
    }
    const [bootstrap, environment, runtime, impact, secrets, agents] =
      await Promise.all([
        get("/api/v1/bootstrap"),
        get(`/api/v1/runtime-environments/${pins.environmentRef}`),
        get(`/api/v1/agents/${pins.agentRef}/runtime-configuration`),
        get(
          `/api/v1/runtime-environments/${pins.environmentRef}/versions/${pins.versionRef}/impact?pageSize=100`,
        ),
        get(
          `/api/v1/projects/${pins.projectRef}/runtime-secrets?pageSize=100&query=GH_TOKEN`,
        ),
        get(
          `/api/v1/runtime-environments/${pins.environmentRef}/agents?pageSize=100`,
        ),
      ]);
    return {
      ownerRef: bootstrap.currentUser?.ref,
      organizationRef: bootstrap.organizationRef,
      platformRole: bootstrap.platformRole,
      environment: {
        ref: environment.ref,
        organizationRef: environment.organizationRef,
        projectRef: environment.projectRef,
        scopeKind: environment.scopeKind,
        name: environment.name,
        state: environment.state,
        version: environment.version,
        ready: environment.ready,
        revisionRef: environment.currentVersion?.ref,
        digest: environment.currentVersion?.digest,
      },
      agentVersion: runtime.agentVersion,
      assignedAgents: {
        items: Array.isArray(agents.items)
          ? agents.items.map((item) => ({
              ref: item.ref,
              version: item.version,
              projectRef: item.projectRef,
              system: item.system,
            }))
          : null,
        nextPageToken: agents.nextPageToken,
      },
      binding: runtime.environmentBinding && {
        ref: runtime.environmentBinding.ref,
        version: runtime.environmentBinding.version,
        agentRef: runtime.environmentBinding.agentRef,
        environmentRef: runtime.environmentBinding.environmentRef,
        versionRef: runtime.environmentBinding.versionRef,
        digest: runtime.environmentBinding.digest,
      },
      impact: {
        environmentRef: impact.environmentRef,
        environmentVersion: impact.environmentVersion,
        targetVersionRef: impact.targetVersionRef,
        targetDigest: impact.targetDigest,
        total: impact.total,
        nextPageToken: impact.nextPageToken,
        consumers: Array.isArray(impact.consumers)
          ? impact.consumers.map((item) => ({
              agentRef: item.agentRef,
              agentVersion: item.agentVersion,
              bindingRef: item.bindingRef,
              bindingVersion: item.bindingVersion,
              versionRef: item.versionRef,
              projectRef: item.projectRef,
              scopeKind: item.scopeKind,
              organizationRef: item.organizationRef,
            }))
          : null,
      },
      secretAbsent:
        Array.isArray(secrets.items) &&
        secrets.items.every(
          (item) =>
            item.scopeKind === "PROJECT" &&
            item.organizationRef === pins.organizationRef &&
            item.projectRef === pins.projectRef &&
            item.name !== "GH_TOKEN",
        ) &&
        !secrets.nextPageToken,
    };
  }, profile);
}

export function assertOwnerPins(value, profile) {
  requireInput(
    value.ownerRef === profile.ownerRef &&
      value.organizationRef === profile.organizationRef &&
      value.platformRole === "OWNER",
  );
  const env = value.environment;
  requireInput(
    env?.ref === profile.environmentRef &&
      env.scopeKind === "PROJECT" &&
      env.organizationRef === profile.organizationRef &&
      env.projectRef === profile.projectRef &&
      env.name === "selfdev-write" &&
      env.state === "ACTIVE" &&
      env.ready === true &&
      env.version === profile.environmentVersion &&
      env.revisionRef === profile.versionRef &&
      env.digest === profile.environmentDigest,
  );
  const binding = value.binding;
  requireInput(
    value.agentVersion === profile.agentVersion &&
      binding?.agentRef === profile.agentRef &&
      binding.ref === profile.bindingRef &&
      binding.version === profile.bindingVersion &&
      binding.environmentRef === profile.environmentRef &&
      binding.versionRef === profile.versionRef &&
      binding.digest === profile.bindingDigest,
  );
  const impact = value.impact;
  requireInput(
    impact?.environmentRef === profile.environmentRef &&
      impact.environmentVersion === profile.environmentVersion &&
      impact.targetVersionRef === profile.versionRef &&
      impact.targetDigest === profile.environmentDigest &&
      impact.total === 0 &&
      impact.nextPageToken === "" &&
      Array.isArray(impact.consumers) &&
      impact.consumers.length === 0,
  );
  // Impact перечисляет только обновляемые binding; already-current туда не входит.
  // Полный unfiltered assigned list доказывает sole Developer независимо от target.
  const assigned = value.assignedAgents;
  requireInput(
    assigned &&
      (assigned.nextPageToken === undefined || assigned.nextPageToken === "") &&
      Array.isArray(assigned.items) &&
      assigned.items.length === 1 &&
      assigned.items[0]?.ref === profile.agentRef &&
      assigned.items[0].version === profile.agentVersion &&
      assigned.items[0].projectRef === profile.projectRef &&
      assigned.items[0].system === false &&
      value.secretAbsent === true,
  );
}

export async function assertMaskedForm(page, profile) {
  requireInput(page.url() === profile.pageURL);
  requireInput(
    await page.evaluate(() => {
      const fields = [
        ...document.querySelectorAll(
          'textarea[name^="runtime-secret-"][name$="-value"]',
        ),
      ];
      if (fields.length !== 1) return false;
      const value = fields[0];
      const dialog = value.closest('[role="dialog"]');
      const names = dialog?.querySelectorAll(
        'input[name^="runtime-secret-"][name$="-name"]',
      );
      const types = dialog?.querySelectorAll(
        'select[name^="runtime-secret-"][name$="-value-type"]',
      );
      return Boolean(
        dialog &&
        names?.length === 1 &&
        types?.length === 1 &&
        names[0].value === "GH_TOKEN" &&
        !names[0].disabled &&
        types[0].value === "STRING" &&
        !types[0].disabled &&
        value.value === "" &&
        !value.disabled &&
        !value.readOnly &&
        value.classList.contains("secret-form__masked") &&
        getComputedStyle(value).webkitTextSecurity === "disc",
      );
    }),
  );
  const field = page.locator(maskedField);
  requireInput(
    (await field.count()) === 1 &&
      (await field.isVisible()) &&
      (await field.isEnabled()),
  );
  return field;
}

export async function fillProjectSecret(profile, dependencies = {}) {
  let browser,
    selected,
    attempted = false;
  try {
    validateInputProfile(profile);
    assertInputEnvironment(
      dependencies.environment ?? process.env,
      dependencies.argumentsList ?? process.execArgv,
    );
    const fence = dependencies.assertEndpoint ?? assertChromeEndpoint;
    fence(profile);
    let connect = dependencies.connect;
    if (!connect) {
      const require = createRequire(
        new URL(
          "../../services/staff/control-center/package.json",
          import.meta.url,
        ),
      );
      const chromium = require("playwright").chromium;
      connect = (endpoint) =>
        chromium.connectOverCDP(endpoint, { timeout: 5000 });
    }
    browser = await connect(profile.cdpEndpoint);
    const pages = browser
      .contexts()
      .flatMap((context) => context.pages())
      .filter((page) => page.url() === profile.pageURL);
    requireInput(pages.length === 1);
    const page = pages[0];
    const read = dependencies.readPins ?? readOwnerPins;
    const before = await read(page, profile);
    assertOwnerPins(before, profile);
    await assertMaskedForm(page, profile);
    const after = await read(page, profile);
    assertOwnerPins(after, profile);
    requireInput(JSON.stringify(after) === JSON.stringify(before));
    fence(profile);
    const field = await assertMaskedForm(page, profile);
    // Закреплённый DOM handle не переадресует fill в новую страницу при navigation.
    const element = await field.elementHandle({ timeout: 5000 });
    requireInput(element && page.url() === profile.pageURL);
    selected = (dependencies.readSecrets ?? readPrivateAgentSecrets)([
      secretInputKey,
    ]);
    requireInput(
      typeof selected[secretInputKey] === "string" &&
        selected[secretInputKey].length > 0 &&
        selected[secretInputKey].length <= 8192 &&
        !/[\x00-\x1f\x7f]/.test(selected[secretInputKey]),
    );
    requireInput(page.url() === profile.pageURL);
    attempted = true;
    await element.fill(selected[secretInputKey], { timeout: 5000 });
    // Не читаем value: сохранение и OCC/fresh-auth/publication остаются UI owner.
    return { status: "FILLED", code: "PROJECT_DEVELOPER_SECRET_INPUT_FILLED" };
  } catch {
    return {
      status: "FAIL",
      code: attempted
        ? "PROJECT_SECRET_INPUT_UNKNOWN"
        : "PROJECT_SECRET_INPUT_REJECTED",
    };
  } finally {
    if (selected) selected[secretInputKey] = "";
    // connectOverCDP close отключает client, не закрывает owner Chrome/pages.
    if (browser) await browser.close().catch(() => {});
  }
}

export async function main(args, dependencies = {}) {
  try {
    requireInput(
      args.length === 4 &&
        args[0] === "--profile" &&
        args[2] === "--confirm" &&
        args[3] === secretInputIntent,
    );
    assertInputEnvironment(
      dependencies.environment ?? process.env,
      dependencies.argumentsList ?? process.execArgv,
    );
    const profile = (dependencies.readProfile ?? readInputProfile)(args[1]);
    const result = await fillProjectSecret(profile, dependencies);
    (dependencies.output ?? console.log)(JSON.stringify(result));
    return result.status === "FILLED" ? 0 : 1;
  } catch {
    (dependencies.output ?? console.log)(
      JSON.stringify({ status: "FAIL", code: "PROJECT_SECRET_INPUT_REJECTED" }),
    );
    return 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href)
  process.exitCode = await main(process.argv.slice(2));
