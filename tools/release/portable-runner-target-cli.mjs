#!/usr/bin/env node
import { constants, openSync, closeSync, fstatSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { portableRunnerTarget } from './portable-runner-target.mjs';

// Входы содержат только operator scope и Kubernetes metadata/config, не credentials.
function readPrivate(path) {
  const fd=openSync(path,constants.O_RDONLY|constants.O_NOFOLLOW);
  try { const s=fstatSync(fd); if(!s.isFile()||s.nlink!==1||s.uid!==process.getuid()||(s.mode&0o077)!==0||s.size<=0||s.size>1<<20)throw Error('PRIVATE_INPUT_REQUIRED');return JSON.parse(readFileSync(fd,'utf8')); }
  finally {closeSync(fd);}
}
try {
  const [profilePath,directory,context,...extra]=process.argv.slice(2);
  if(!profilePath||!directory||!context||extra.length)throw Error('INVALID_ARGUMENTS');
  const target=portableRunnerTarget(readPrivate(profilePath),{context,
    clusterUID:readPrivate(join(directory,'cluster.json')).metadata.uid,
    namespaceUID:readPrivate(join(directory,'namespace.json')).metadata.uid,
    installation:readPrivate(join(directory,'installation.json')),
  });
  if(!target.allows(readPrivate(join(directory,'resource.json'))))throw Error('PORTABLE_RUNNER_RESOURCE_REJECTED');
  process.stdout.write(JSON.stringify({status:'PASS',portableTargetSHA256:target.digest})+'\n');
} catch {process.stderr.write('Portable runner target verification failed\n');process.exitCode=1;}
