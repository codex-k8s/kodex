import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { resources, proxyImage } from "./proxy-session-store.mjs";
const uid = (i) => `00000000-0000-4000-8000-${String(i).padStart(12, "0")}`;
const fake = `#!/usr/bin/env node
const fs=require('node:fs');const file=process.env.FIXTURE_STATE;const state=JSON.parse(fs.readFileSync(file));const args=process.argv.slice(2);const start=args.findIndex(x=>['get','create','patch'].includes(x));const [verb,kind,name]=args.slice(start);const save=()=>fs.writeFileSync(file,JSON.stringify(state),{mode:0o600});
if(verb==='get'){
 const key=kind.toLowerCase()+':'+name;const obj=state.objects[key];if(!obj){if(args.includes('--ignore-not-found'))process.exit(0);process.exit(1)}
 const output=args[args.indexOf('-o')+1];state.reads.push({kind,name,output});save();if(output==='jsonpath={.metadata}')process.stdout.write(JSON.stringify(obj.metadata));else if(output==='jsonpath={.immutable}')process.stdout.write(String(obj.immutable));else if(output==='json')process.stdout.write(JSON.stringify(obj));else process.exit(2);process.exit(0);
}
const input=JSON.parse(fs.readFileSync(0,'utf8'));
state.calls.push({verb,kind:verb==='create'?input.kind:kind,name:verb==='create'?input.metadata.name:name});
if(state.failName===(input.metadata?.name||name)){save();process.exit(1)}
if(verb==='create'){const key=input.kind.toLowerCase()+':'+input.metadata.name;if(state.objects[key])process.exit(1);input.metadata.uid='00000000-0000-4000-8000-'+String(state.calls.length+20).padStart(12,'0');input.metadata.resourceVersion='1';state.objects[key]=input;save();process.exit(0)}
if(verb==='patch'){const obj=state.objects[kind+':'+name];for(const p of input){const parts=p.path.split('/').slice(1);let target=obj;for(const part of parts.slice(0,-1))target=target[part];const key=parts.at(-1);if(p.op==='test'&&JSON.stringify(target[key])!==JSON.stringify(p.value))process.exit(1);if(p.op==='replace')target[key]=p.value}obj.metadata.resourceVersion=String(Number(obj.metadata.resourceVersion)+1);save();process.exit(0)}process.exit(1);
`;
function fixture(ready = false) {
  const directory = mkdtempSync(join(tmpdir(), "kodex-proxy-cli-"));
  const stateFile = join(directory, "state.json");
  const objects = {
    "namespace:kube-system": { metadata: { uid: uid(1) } },
    "namespace:kodex-system": {
      metadata: { uid: uid(2), labels: { "kodex.dev/environment": "staging" } },
    },
  };
  if (ready) {
    for (const [i, r] of resources().entries()) {
      r.metadata = {
        ...r.metadata,
        uid: uid(i + 3),
        resourceVersion: "1",
        generation: 1,
      };
      if (r.kind === "Certificate")
        r.status = { conditions: [{ type: "Ready", status: "True" }] };
      if (r.kind === "StatefulSet")
        r.status = { readyReplicas: 1, observedGeneration: 1 };
      if (
        r.kind === "NetworkPolicy" &&
        Array.isArray(r.spec.egress) &&
        r.spec.egress.length === 0
      )
        delete r.spec.egress;
      objects[r.kind.toLowerCase() + ":" + r.metadata.name] = r;
    }
    objects["secret:proxy-session-store-auth-v1"] = {
      kind: "Secret",
      immutable: true,
      metadata: {
        name: "proxy-session-store-auth-v1",
        namespace: "kodex-system",
        uid: uid(20),
        labels: { "kodex.io/owner": "management-surfaces" },
      },
    };
    objects["deployment:oauth2-control-center"] = {
      kind: "Deployment",
      metadata: {
        name: "oauth2-control-center",
        namespace: "kodex-system",
        uid: uid(21),
        resourceVersion: "7",
      },
      spec: {
        replicas: 2,
        template: {
          spec: {
            containers: [
              {
                name: "oauth2-proxy",
                image: proxyImage,
                args: [
                  "--cookie-expire=8h",
                  "--cookie-refresh=1h",
                  "--provider=keycloak-oidc",
                  "--cookie-secure=true",
                  "--cookie-httponly=true",
                ],
              },
            ],
          },
        },
      },
    };
  }
  writeFileSync(stateFile, JSON.stringify({ objects, calls: [], reads: [] }), {
    mode: 0o600,
  });
  writeFileSync(join(directory, "kubectl"), fake, { mode: 0o700 });
  const load = () => JSON.parse(readFileSync(stateFile));
  const change = (fn) => {
    const state = load();
    fn(state);
    writeFileSync(stateFile, JSON.stringify(state), { mode: 0o600 });
  };
  const run = (...args) =>
    execFileSync(
      process.execPath,
      [
        new URL("./proxy-session-store.mjs", import.meta.url).pathname,
        ...args,
        "--context",
        "fixture-staging",
      ],
      {
        env: {
          PATH: directory + ":" + process.env.PATH,
          FIXTURE_STATE: stateFile,
        },
        encoding: "utf8",
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
  return {
    directory,
    load,
    change,
    run,
    dispose: () => rmSync(directory, { recursive: true, force: true }),
  };
}
test("install сначала plan, затем11 owned creates; повтор не генерирует credentials", () => {
  const f = fixture();
  try {
    const plan = join(f.directory, "plan.json"),
      journal = join(f.directory, "journal.jsonl");
    f.run("plan-install", "--output", plan);
    assert.equal(f.load().calls.length, 0);
    f.run(
      "install",
      "--plan",
      plan,
      "--evidence",
      journal,
      "--confirm",
      "INSTALL-STAGING-PROXY-SESSION-STORE",
    );
    assert.equal(f.load().calls.length, resources().length + 1);
    assert.equal(
      JSON.parse(readFileSync(journal, "utf8").trim().split("\n").at(-1))
        .status,
      "INSTALLED",
    );
    assert.throws(() =>
      f.run("plan-install", "--output", join(f.directory, "second.json")),
    );
    assert.equal(f.load().calls.length, resources().length + 1);
    const text = readFileSync(plan, "utf8") + readFileSync(journal, "utf8");
    const secret = f.load().objects["secret:proxy-session-store-auth-v1"];
    for (const key of ["proxy-password", "probe-password", "backup-password"])
      assert.ok(!text.includes(secret.stringData[key]));
  } finally {
    f.dispose();
  }
});
test("unknown partial install не повторяется и сохраняет journal", () => {
  const f = fixture();
  try {
    f.change((s) => (s.failName = "proxy-session-selfsigned"));
    const plan = join(f.directory, "plan.json"),
      journal = join(f.directory, "journal.jsonl");
    f.run("plan-install", "--output", plan);
    assert.throws(() =>
      f.run(
        "install",
        "--plan",
        plan,
        "--evidence",
        journal,
        "--confirm",
        "INSTALL-STAGING-PROXY-SESSION-STORE",
      ),
    );
    assert.equal(
      JSON.parse(readFileSync(journal, "utf8").trim().split("\n").at(-1))
        .status,
      "UNKNOWN",
    );
    assert.throws(() =>
      f.run("plan-install", "--output", join(f.directory, "second.json")),
    );
    assert.equal(f.load().calls.length, 2);
  } finally {
    f.dispose();
  }
});
test("cutover точного плана изменяет только proxy через проверяемый CAS", () => {
  const f = fixture(true);
  try {
    const plan = join(f.directory, "plan.json"),
      journal = join(f.directory, "journal.jsonl");
    f.run("plan-cutover", "--output", plan);
    f.run(
      "cutover",
      "--plan",
      plan,
      "--evidence",
      journal,
      "--confirm",
      "CUTOVER-STAGING-PROXY-SESSIONS-REAUTH",
    );
    assert.equal(f.load().calls.length, 1);
    assert.equal(f.load().calls[0].verb, "patch");
    const secretReads = f.load().reads.filter(
      (read) =>
        read.kind === "secret" && read.name === "proxy-session-store-auth-v1",
    );
    assert.deepEqual(
      [...new Set(secretReads.map((read) => read.output))].sort(),
      ["jsonpath={.immutable}", "jsonpath={.metadata}"],
    );
    const terminal = JSON.parse(
      readFileSync(journal, "utf8").trim().split("\n").at(-1),
    );
    assert.equal(terminal.status, "APPLIED");
    assert.equal(terminal.rollout, "NOT_RUN");
    assert.equal(terminal.reauthentication, "REQUIRED");
  } finally {
    f.dispose();
  }
});
for (const [label, change] of [
  [
    "Deployment resourceVersion",
    (s) =>
      (s.objects["deployment:oauth2-control-center"].metadata.resourceVersion =
        "8"),
  ],
  [
    "Deployment UID",
    (s) =>
      (s.objects["deployment:oauth2-control-center"].metadata.uid = uid(99)),
  ],
  [
    "namespace UID",
    (s) => (s.objects["namespace:kodex-system"].metadata.uid = uid(99)),
  ],
  [
    "dependency config",
    (s) =>
      (s.objects["configmap:proxy-session-store"].data["valkey.conf"] +=
        "port 1\n"),
  ],
])
  test(`CAS drift ${label} не отправляет mutation`, () => {
    const f = fixture(true);
    try {
      const plan = join(f.directory, "plan.json");
      f.run("plan-cutover", "--output", plan);
      f.change(change);
      assert.throws(() =>
        f.run(
          "cutover",
          "--plan",
          plan,
          "--evidence",
          join(f.directory, "journal.jsonl"),
          "--confirm",
          "CUTOVER-STAGING-PROXY-SESSIONS-REAUTH",
        ),
      );
      assert.equal(f.load().calls.length, 0);
    } finally {
      f.dispose();
    }
  });
test("namespace без staging label не получает даже install plan", () => {
  const f = fixture();
  try {
    f.change((s) => (s.objects["namespace:kodex-system"].metadata.labels = {}));
    assert.throws(() =>
      f.run("plan-install", "--output", join(f.directory, "plan.json")),
    );
    assert.equal(f.load().calls.length, 0);
  } finally {
    f.dispose();
  }
});
