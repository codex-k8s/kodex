import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const directory = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(directory, 'render-local.sh'), 'utf8');
const argsLine = source.split('\n').find(line => line.includes('"args":["elf()'));
const [command] = JSON.parse(argsLine.trim().replace(/^"args":/, '').replace(/,$/, ''));
const packageRoot = '/opt/kodex/npm-toolchain/node_modules/@openai';

function fixture(t, { local = false, arch = 'x86_64', version = '0.160.0', missing = false, script = false, executable = true, corruptCopy = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), 'kodex-native-copy-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const bin = join(root, 'bin'), output = join(root, 'codex'), packages = join(root, 'packages');
  for (const path of [bin, output, packages]) mkdirSync(path);
  const wrapper = join(root, 'wrapper');
  writeFileSync(wrapper, '#!/usr/bin/env node\nthrow new Error("NODE_CLOSURE_MISSING");\n', { mode: 0o755 });
  const native = join(root, 'native'), c = join(root, 'fixture.c');
  writeFileSync(c, '#include <stdio.h>\nint main(void) { puts("codex-cli ' + version + '"); return 0; }\n');
  execFileSync('cc', ['-o', native, c], { stdio: ['ignore', 'pipe', 'pipe'] });
  const selected = local ? wrapper : join(packages, 'codex-linux-' + (arch === 'aarch64' ? 'arm64' : 'x64'), 'vendor', arch === 'aarch64' ? 'aarch64-unknown-linux-musl' : 'x86_64-unknown-linux-musl', 'bin', 'codex');
  if (!missing) {
    mkdirSync(dirname(selected), { recursive: true });
    if (script) copyFileSync(wrapper, selected);
    else copyFileSync(native, selected);
    chmodSync(selected, executable ? 0o755 : 0o644);
  }
  writeFileSync(join(bin, 'uname'), '#!/bin/sh\nprintf "%s\\n" "' + arch + '"\n', { mode: 0o755 });
  if (corruptCopy) writeFileSync(join(bin, 'cp'), '#!/bin/sh\nprintf "#!/bin/sh\\nexit 0\\n" >"$2"\n', { mode: 0o755 });
  const installed = join(output, 'codex');
  writeFileSync(installed, 'PREVIOUS_BINARY');
  const translated = command.replaceAll('/usr/local/bin/codex', wrapper).replaceAll(packageRoot, packages).replaceAll('/codex/', output + '/');
  const result = spawnSync('/bin/sh', ['-ec', translated], { encoding: 'utf8', timeout: 5000, env: { PATH: bin + ':/usr/bin:/bin' } });
  return { result, native, installed, output };
}

test('render keeps init scope, native platform closure and exact catalog version', () => {
  assert.ok(!command.includes("'"), 'Внутренний script не разрывает single-quoted yq expression');
  assert.ok(!command.includes('find '));
  assert.ok(command.includes('codex-linux-x64') && command.includes('codex-linux-arm64'));
  const runtimeSource = readFileSync(join(directory, '../../services/internal/secret-broker/internal/providercredential/model_catalog.go'), 'utf8');
  const version = /catalogCodexVersion\s*=\s*"([^"]+)"/.exec(runtimeSource)[1];
  assert.ok(command.includes('"codex-cli ' + version + '"'));
  assert.ok(command.indexOf('elf "$temporary"') < command.indexOf('mv -f'));
  assert.ok(command.indexOf('"$temporary" --version') < command.indexOf('mv -f'));
});

for (const entry of [{ local: true, arch: 'unsupported' }, { arch: 'x86_64' }, { arch: 'aarch64' }]) {
  test('native binary is copied and executable without npm closure: ' + JSON.stringify(entry), t => {
    const f = fixture(t, entry);
    assert.equal(f.result.status, 0, f.result.stderr);
    assert.deepEqual(readFileSync(f.installed), readFileSync(f.native));
    assert.equal(execFileSync(f.installed, ['--version'], { encoding: 'utf8', env: { PATH: '/nonexistent' } }).trim(), 'codex-cli 0.160.0');
    assert.equal(existsSync(join(f.output, '.codex.tmp')), false);
  });
}

for (const entry of [{ arch: 'unsupported' }, { missing: true }, { script: true }, { executable: false }, { version: '0.159.0' }, { corruptCopy: true }]) {
  test('invalid native input fails closed and preserves previous destination: ' + JSON.stringify(entry), t => {
    const f = fixture(t, entry);
    assert.notEqual(f.result.status, 0);
    assert.equal(readFileSync(f.installed, 'utf8'), 'PREVIOUS_BINARY');
  });
}
