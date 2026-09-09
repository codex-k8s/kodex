import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync, spawnSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { exclusiveOperatorGroup, localNSSProfile, gitTrustEnvironment, trustedSourceRoots } from './source-git-trust.mjs';

function fixture(t) {
  const previousMask = process.umask(0o022);
  t.after(() => process.umask(previousMask));
  const directory = mkdtempSync(join(tmpdir(), 'kodex-trust-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = join(directory, 'repo'); mkdirSync(root, { mode: 0o755 });
  const git = (...args) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
  git('init'); git('config', 'user.name', 'Fixture'); git('config', 'user.email', 'fixture@example.invalid');
  git('remote', 'add', 'origin', 'https://github.com/codex-k8s/kodex.git');
  mkdirSync(join(root, 'nested'), { mode: 0o755 });
  writeFileSync(join(root, 'nested/file.txt'), 'fixture\n', { mode: 0o644 });
  writeFileSync(join(root, '.gitignore'), '.env\n', { mode: 0o644 });
  git('add', '.'); git('commit', '-m', 'fixture');
  const revision = git('rev-parse', 'HEAD');
  const source = join(directory, 'source'); git('worktree', 'add', '--detach', source, revision);
  const manifest = join(directory, 'manifest.json');
  const value = { version: 1, profile: 'component-revisions', components: [{ sources: [{ path: root, revision, mountedPath: join(root, 'nested/file.txt') }] }] };
  const save = () => writeFileSync(manifest, JSON.stringify(value), { mode: 0o600 }); save();
  return { directory, root, source, revision, git, manifest, value, save };
}

test('linked source and exact manifest roots work through shell, nested Git and inspectSource descendants', t => {
  const f = fixture(t), roots = trustedSourceRoots(f.source, f.revision, f.manifest);
  assert.deepEqual(roots, [f.source, f.root]);
  const env = gitTrustEnvironment(roots);
  env.GIT_TEST_ASSUME_DIFFERENT_OWNER = '1';
  const inspect = fileURLToPath(new URL('../release/application-source.mjs', import.meta.url));
  assert.equal(execFileSync('bash', ['-c', 'git -C "$1/nested" rev-parse --show-toplevel', 'fixture', f.root], { env, encoding: 'utf8' }).trim(), f.root);
  assert.equal(JSON.parse(execFileSync(process.execPath, ['--input-type=module', '-e', 'const {inspectSource}=await import(process.argv[1]);console.log(JSON.stringify(inspectSource(process.argv[2])))', inspect, f.source], { env, encoding: 'utf8' })).revision, f.revision);
});

test('process trust clears inherited wildcard and Git overrides without trusting another checkout', t => {
  const f = fixture(t), home = join(f.directory, 'home'); mkdirSync(home);
  const config = join(home, '.gitconfig'); writeFileSync(config, '[safe]\n directory = *\n');
  const env = gitTrustEnvironment([f.source], { ...process.env, HOME: home, GIT_DIR: '/foreign', GIT_WORK_TREE: '/foreign', GIT_CONFIG_PARAMETERS: "'safe.directory=*'" });
  assert.equal(env.GIT_DIR, undefined); assert.equal(env.GIT_CONFIG_PARAMETERS, undefined);
  env.GIT_TEST_ASSUME_DIFFERENT_OWNER = '1';
  assert.equal(spawnSync('git', ['-C', f.source, 'rev-parse', 'HEAD'], { env }).status, 0);
  assert.notEqual(spawnSync('git', ['-C', f.root, 'rev-parse', 'HEAD'], { env }).status, 0);
  assert.equal(readFileSync(config, 'utf8'), '[safe]\n directory = *\n');
});

test('wrong revision, origin, dirty tracked content and private material fail before trust export', t => {
  const f = fixture(t);
  assert.throws(() => trustedSourceRoots(f.source, 'a'.repeat(40)), /CHECKOUT_MISMATCH/);
  f.git('remote', 'set-url', 'origin', 'https://example.invalid/repo.git');
  assert.throws(() => trustedSourceRoots(f.source, f.revision), /ORIGIN_INVALID/);
  f.git('remote', 'set-url', 'origin', 'https://github.com/codex-k8s/kodex.git');
  writeFileSync(join(f.source, 'nested/file.txt'), 'changed');
  assert.throws(() => trustedSourceRoots(f.source, f.revision), /CHECKOUT_DIRTY/);
  writeFileSync(join(f.source, 'nested/file.txt'), 'fixture\n');
  writeFileSync(join(f.source, '.env'), 'FIXTURE=not-a-secret\n');
  assert.throws(() => trustedSourceRoots(f.source, f.revision), /PRIVATE_MATERIAL/);
});

test('world-writable source, ancestor and Git metadata are rejected', t => {
  const f = fixture(t);
  for (const path of [f.source, f.directory, join(f.root, '.git/config')]) {
    chmodSync(path, 0o777);
    assert.throws(() => trustedSourceRoots(f.source, f.revision), /SOURCE_TRUST_WRITABLE/);
    chmodSync(path, path.endsWith('config') ? 0o644 : 0o755);
  }
});

test('symlink checkout and metadata and invalid linked backref are rejected', t => {
  const f = fixture(t), link = join(f.directory, 'link'); symlinkSync(f.source, link);
  assert.throws(() => trustedSourceRoots(link, f.revision), /PATH_INVALID/);
  const config = join(f.root, '.git/config'), original = readFileSync(config);
  rmSync(config); symlinkSync(join(f.root, '.git/HEAD'), config);
  assert.throws(() => trustedSourceRoots(f.source, f.revision), /PATH_INVALID/);
  rmSync(config); writeFileSync(config, original);
  const gitdir = readFileSync(join(f.source, '.git'), 'utf8').trim().slice(8);
  writeFileSync(join(gitdir, 'gitdir'), `${f.root}/.git\n`);
  assert.throws(() => trustedSourceRoots(f.source, f.revision), /BACKREF_INVALID/);
});

test('manifest cannot extend trust with mismatched revision, traversal or outside mount and must be private', t => {
  const f = fixture(t);
  chmodSync(f.manifest, 0o644);
  assert.throws(() => trustedSourceRoots(f.source, f.revision, f.manifest), /MANIFEST_PRIVATE_REQUIRED/);
  chmodSync(f.manifest, 0o600);
  const entry = f.value.components[0].sources[0];
  entry.mountedPath = f.source; f.save();
  assert.throws(() => trustedSourceRoots(f.source, f.revision, f.manifest), /MOUNT_OUTSIDE_ROOT/);
  entry.mountedPath = join(f.root, 'nested'); entry.path = `${f.root}/../repo`; f.save();
  assert.throws(() => trustedSourceRoots(f.source, f.revision, f.manifest), /PATH_INVALID/);
  entry.path = f.source; entry.mountedPath = f.source; entry.revision = 'a'.repeat(40); f.save();
  assert.throws(() => trustedSourceRoots(f.source, f.revision, f.manifest), /REVISION_CONFLICT/);
});

test('group write is allowed only for fully resolved operator/root membership', () => {
  const uid = process.getuid();
  const passwd = `root:x:0:0::/:/bin/sh\noperator:x:${uid}:123::/home/operator:/bin/sh\nforeign:x:43210:456::/:/bin/sh\n`;
  const lookup = group => (kind => kind === 'group' ? group : passwd);
  assert.equal(exclusiveOperatorGroup(123, uid, lookup('operator:x:123:root')), true);
  assert.equal(exclusiveOperatorGroup(123, uid, lookup('operator:x:123:foreign')), false);
  assert.equal(exclusiveOperatorGroup(123, uid, lookup('operator:x:123:missing')), false);
  assert.equal(exclusiveOperatorGroup(456, uid, lookup('foreign:x:456:')), false);
});

test('only explicit local files/systemd NSS profile admits group write', () => {
  assert.equal(localNSSProfile('passwd: files systemd\ngroup: files systemd\n'), true);
  assert.equal(localNSSProfile('passwd: files\ngroup: files\n'), true);
  for (const value of ['files sss', 'files ldap', 'compat', 'files [SUCCESS=return] systemd'])
    assert.equal(localNSSProfile(`passwd: ${value}\ngroup: files\n`), false);
  assert.equal(localNSSProfile('passwd: files\ngroup: files\ngroup: files\n'), false);
});
