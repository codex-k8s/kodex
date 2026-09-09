#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { existsSync, lstatSync, readFileSync, readdirSync, realpathSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const revisionPattern = /^[a-f0-9]{40}$/;
const pathPattern = /^\/[A-Za-z0-9_.\/-]{1,600}$/;
function requireValue(value, code) { if (!value) throw new Error(code); }

// Доверие ограничено текущим процессом и точными checkout. Пустая первая
// запись сбрасывает унаследованные safe.directory, включая wildcard.
export function gitTrustEnvironment(roots, base = process.env) {
  const env = Object.fromEntries(Object.entries(base).filter(([key]) => !key.startsWith('GIT_')));
  env.GIT_CONFIG_COUNT = String(roots.length + 1);
  env.GIT_CONFIG_KEY_0 = 'safe.directory';
  env.GIT_CONFIG_VALUE_0 = '';
  roots.forEach((root, index) => {
    env[`GIT_CONFIG_KEY_${index + 1}`] = 'safe.directory';
    env[`GIT_CONFIG_VALUE_${index + 1}`] = root;
  });
  env.GIT_OPTIONAL_LOCKS = '0';
  return env;
}

// Проверяется и primary group из passwd, и явное supplementary membership.
// Неизвестный участник или группа с другим UID не расширяет доверие.
export function localNSSProfile(text) {
  return ['passwd', 'group'].every(kind => {
    const lines = text.split('\n').map(line => line.split('#')[0].trim()).filter(line => line.startsWith(`${kind}:`));
    return lines.length === 1 && ['files', 'files systemd'].includes(lines[0].slice(kind.length + 1).trim().replace(/\s+/g, ' '));
  });
}

export function exclusiveOperatorGroup(gid, uid, lookup) {
  let localAccounts;
  if (!lookup) {
    if (!localNSSProfile(readFileSync('/etc/nsswitch.conf', 'utf8'))) return false;
    const getent = (...args) => execFileSync('getent', args, { encoding: 'utf8', timeout: 5000, maxBuffer: 2 << 20, stdio: ['ignore', 'pipe', 'pipe'] });
    const localGroup = readFileSync('/etc/group', 'utf8').trim().split('\n').filter(line => line.split(':')[2] === String(gid));
    const resolvedGroup = getent('group', String(gid)).trim();
    if (localGroup.length !== 1 || localGroup[0] !== resolvedGroup) return false;
    localAccounts = readFileSync('/etc/passwd', 'utf8').trim().split('\n');
    const resolvedAccounts = getent('passwd');
    lookup = kind => kind === 'group' ? resolvedGroup : resolvedAccounts;
  }
  const groups = lookup('group', String(gid)).trim().split('\n');
  if (groups.length !== 1) return false;
  const group = groups[0].split(':');
  if (group.length !== 4 || group[2] !== String(gid)) return false;
  const accountLines = lookup('passwd').trim().split('\n');
  const accounts = accountLines.map(line => line.split(':'));
  if (accounts.some(account => account.length !== 7)) return false;
  const members = new Set(group[3].split(',').filter(Boolean));
  for (const account of accounts) if (account[3] === String(gid)) members.add(account[0]);
  for (const line of localAccounts ?? []) if (line.split(':')[3] === String(gid)) members.add(line.split(':')[0]);
  return members.size > 0 && [...members].every(name => {
    const entries = accounts.filter(item => item[0] === name);
    return entries.length === 1 && [0, uid].includes(Number(entries[0][2])) &&
      (!localAccounts || localAccounts.includes(entries[0].join(':')));
  });
}

export function trustedSourceRoots(source, revision, manifest) {
  const roots = new Map(), verified = new Set(), metadata = new Set();
  const uid = process.getuid(), groups = new Map();
  const exclusiveGroup = gid => {
    if (!groups.has(gid)) groups.set(gid, exclusiveOperatorGroup(gid, uid));
    return groups.get(gid);
  };
  const safePath = path => requireValue(typeof path === 'string' && (path === '/' || pathPattern.test(path)) && resolve(path) === path && realpathSync(path) === path, 'SOURCE_TRUST_PATH_INVALID');
  const check = (path, ancestor = false) => {
    safePath(path);
    const stat = lstatSync(path);
    requireValue(!stat.isSymbolicLink() && (stat.isFile() || stat.isDirectory()) && [0, uid].includes(stat.uid), 'SOURCE_TRUST_OWNER_INVALID');
    // Единственное исключение — системный sticky ancestor вроде /tmp;
    // checkout и Git metadata никогда не получают это исключение.
    const stickyAncestor = ancestor && stat.isDirectory() && stat.uid === 0 && (stat.mode & 0o1000) !== 0;
    requireValue(stickyAncestor || ((stat.mode & 0o002) === 0 && ((stat.mode & 0o020) === 0 || exclusiveGroup(stat.gid))), 'SOURCE_TRUST_WRITABLE');
    return stat;
  };
  const ancestry = path => {
    for (let parent = dirname(path); !verified.has(parent); parent = dirname(parent)) {
      check(parent, true); verified.add(parent);
      if (parent === '/') break;
    }
  };
  const tree = path => {
    if (metadata.has(path)) return;
    requireValue(metadata.size < 250000, 'SOURCE_TRUST_METADATA_LIMIT');
    const stat = check(path); metadata.add(path);
    if (stat.isDirectory()) for (const name of readdirSync(path)) tree(join(path, name));
  };
  const add = (path, sha) => {
    requireValue(revisionPattern.test(sha ?? ''), 'SOURCE_TRUST_REVISION_INVALID');
    safePath(path);
    requireValue(lstatSync(path).isDirectory(), 'SOURCE_TRUST_ROOT_INVALID');
    requireValue(!roots.has(path) || roots.get(path) === sha, 'SOURCE_TRUST_REVISION_CONFLICT');
    roots.set(path, sha);
  };
  add(source, revision);
  if (manifest) {
    safePath(manifest);
    const stat = lstatSync(manifest);
    requireValue(stat.isFile() && stat.uid === uid && (stat.mode & 0o077) === 0 && stat.size <= (8 << 20), 'SOURCE_TRUST_MANIFEST_PRIVATE_REQUIRED');
    const value = JSON.parse(readFileSync(manifest, 'utf8'));
    requireValue(value.version === 1 && value.profile === 'component-revisions' && Array.isArray(value.components) && value.components.length > 0 && value.components.length <= 128, 'SOURCE_TRUST_MANIFEST_INVALID');
    for (const component of value.components) {
      requireValue(Array.isArray(component.sources) && component.sources.length <= 128, 'SOURCE_TRUST_MANIFEST_INVALID');
      for (const entry of component.sources) {
        add(entry.path, entry.revision);
        safePath(entry.mountedPath);
        requireValue(entry.mountedPath === entry.path || entry.mountedPath.startsWith(`${entry.path}/`), 'SOURCE_TRUST_MOUNT_OUTSIDE_ROOT');
      }
    }
  }
  requireValue(roots.size <= 128, 'SOURCE_TRUST_ROOT_LIMIT');
  for (const root of roots.keys()) {
    requireValue(!manifest || (manifest !== root && !manifest.startsWith(`${root}/`)), 'SOURCE_TRUST_MANIFEST_INSIDE_SOURCE');
    check(root); ancestry(root);
    const dotgit = join(root, '.git');
    const stat = check(dotgit);
    let gitdir = dotgit;
    if (stat.isFile()) {
      requireValue(stat.size < 4096, 'SOURCE_TRUST_GITDIR_INVALID');
      const match = /^gitdir: (.+)\n?$/.exec(readFileSync(dotgit, 'utf8'));
      requireValue(match, 'SOURCE_TRUST_GITDIR_INVALID');
      gitdir = resolve(root, match[1]);
      check(gitdir); ancestry(gitdir);
      const backref = join(gitdir, 'gitdir');
      check(backref);
      requireValue(readFileSync(backref, 'utf8').trim() === dotgit, 'SOURCE_TRUST_GITDIR_BACKREF_INVALID');
    }
    tree(gitdir);
    const commonfile = join(gitdir, 'commondir');
    if (existsSync(commonfile)) {
      const common = resolve(gitdir, readFileSync(commonfile, 'utf8').trim());
      check(common); ancestry(common); tree(common);
    }
  }
  const env = gitTrustEnvironment([...roots.keys()]);
  for (const [root, sha] of roots) {
    const git = (...args) => execFileSync('git', ['-C', root, ...args], { env, encoding: 'utf8', timeout: 10000, maxBuffer: 8 << 20, stdio: ['ignore', 'pipe', 'pipe'] }).trim();
    requireValue(git('rev-parse', '--show-toplevel') === root && git('rev-parse', 'HEAD') === sha, 'SOURCE_TRUST_CHECKOUT_MISMATCH');
    requireValue(['https://github.com/codex-k8s/kodex.git', 'git@github.com:codex-k8s/kodex.git'].includes(git('remote', 'get-url', 'origin')), 'SOURCE_TRUST_ORIGIN_INVALID');
    requireValue(git('status', '--porcelain', '--untracked-files=all') === '', 'SOURCE_TRUST_CHECKOUT_DIRTY');
    requireValue(['.env', '.kodex-env', '.kodex-remote-env'].every(name => !existsSync(join(root, name))), 'SOURCE_TRUST_PRIVATE_MATERIAL');
  }
  return [...roots.keys()];
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args = process.argv.slice(2);
    requireValue((args.length === 4 || args.length === 6) && args[0] === '--source' && args[2] === '--revision' && (args.length === 4 || args[4] === '--manifest'), 'SOURCE_TRUST_ARGUMENTS_INVALID');
    process.stdout.write(`${trustedSourceRoots(args[1], args[3], args[5]).join('\n')}\n`);
  } catch (error) {
    const code = /^SOURCE_TRUST_[A-Z_]+$/.test(error.message) ? error.message : 'SOURCE_TRUST_FAILED';
    process.stderr.write(`Source Git trust failed: ${code}\n`);
    process.exitCode = 1;
  }
}
