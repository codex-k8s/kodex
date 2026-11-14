#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {lstatSync, realpathSync, readSync} from 'node:fs';
import {readComponentServingInventory,componentServingReady} from './portable-component-migration.mjs';
import {activeDeploymentPods} from './stable-archive-transition.mjs';
import {fingerprint} from './scoped-release.mjs';

// Узкий read-only bridge использует общий lineage/parser, не копирует правила join.
try {
  const [kubeconfig, context, uid, mode='stopped', ...rest] = process.argv.slice(2);
  if (rest.length || !['stopped','ready'].includes(mode) || !kubeconfig?.startsWith('/') || !lstatSync(kubeconfig).isFile() ||
      realpathSync(kubeconfig) !== kubeconfig || /[\r\n\0]/.test(kubeconfig) ||
      !/^[A-Za-z0-9_.:@/-]{1,160}$/.test(context ?? '') ||
      !/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/.test(uid ?? '')) throw Error('INVALID_INPUT');
  const kube = args => execFileSync('kubectl', ['--kubeconfig', kubeconfig, '--context', context,
    '--request-timeout=10s', ...args], {encoding:'utf8', timeout:15000, maxBuffer:8<<20, stdio:['ignore','pipe','pipe']});
  const buffer=Buffer.alloc((1<<20)+1);let length=0,chunk;
  while(length<buffer.length&&(chunk=readSync(0,buffer,length,buffer.length-length,null))>0)length+=chunk;
  if(length>1<<20)throw Error('INPUT_BOUND');
  const expected=JSON.parse(buffer.subarray(0,length).toString('utf8'));
  if(Object.keys(expected).join(',')!=='spec'||!expected.spec||typeof expected.spec!=='object')throw Error('EXACT_INPUT_REQUIRED');
  const readDeployment=()=>JSON.parse(kube(['-n','kodex-system','get','deployment','runtime-controller','-o','json']));
  const deployment = readDeployment();
  if(fingerprint(expected.spec)!==fingerprint(deployment.spec))throw Error('SPEC_DRIFT');
  if (deployment.metadata?.uid !== uid || (mode==='stopped' ? deployment.spec?.replicas !== 0 :
      !Number.isSafeInteger(deployment.spec?.replicas)||deployment.spec.replicas<1||deployment.spec.replicas>8)) throw Error('EXACT_DEPLOYMENT_REQUIRED');
  const active = activeDeploymentPods(readComponentServingInventory(kube,'runtime-controller',uid),uid);
  const confirmed=mode==='stopped' ? active.length===0 : componentServingReady(deployment,active,deployment.spec.replicas);
  const after=readDeployment();
  if(after.metadata?.uid!==uid||after.metadata.generation!==deployment.metadata.generation||fingerprint(after.spec)!==fingerprint(expected.spec))throw Error('DEPLOYMENT_DRIFT');
  process.stdout.write(JSON.stringify({deploymentUID:uid, mode, confirmed, activePods:active.length})+'\n');
} catch {
  process.stderr.write('Component serving readback failed\n'); process.exitCode=1;
}
