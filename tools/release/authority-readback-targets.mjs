import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';
import {fingerprint} from './scoped-release.mjs';

// Publisher считает обязательные role/generation bindings, а не все delivery
// targets. Optional build jobs не превращаются в startup readers из-за receipts.
export function authorityReadbackContract(raw) {
  const value=JSON.parse(execFileSync('yq',['-o=json','-I=0','.','-'],{
    input:raw,encoding:'utf8',stdio:['pipe','pipe','pipe'],timeout:30000,maxBuffer:1<<20,
  }));
  const reject=()=>{throw Error('AUTHORITY_READBACK_REGISTRY_REJECTED');};
  if(value?.version!==1||!Array.isArray(value.targets)||!value.targets.length||value.targets.length>384)reject();
  const required=[],seen=new Set();
  for(const target of value.targets) {
    if(!/^[a-z][a-z0-9-]{0,79}$/.test(target.workload_id??'')||
      !['AUTHORIZATION_ISSUER','AUTHORIZATION_VERIFIER','AUTHORITY_PROOF_RESOLVER'].includes(target.role)||
      !Number.isSafeInteger(target.workload_generation)||target.workload_generation<1||
      typeof target.startup_readback_required!=='boolean')reject();
    const binding={workloadID:target.workload_id,role:target.role,generation:target.workload_generation};
    const key=JSON.stringify(binding);if(seen.has(key))reject();seen.add(key);
    if(target.startup_readback_required)required.push(binding);
  }
  if(!required.length)reject();
  required.sort((a,b)=>a.workloadID<b.workloadID?-1:a.workloadID>b.workloadID?1:a.role<b.role?-1:a.role>b.role?1:a.generation-b.generation);
  return Object.freeze({registeredReadbackCount:value.targets.length,expectedReadbackCount:required.length,
    requiredReadbackTargetsSHA256:fingerprint(required)});
}

export function canonicalAuthorityReadbackContract() {
  return authorityReadbackContract(readFileSync(new URL('../../deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml',import.meta.url),'utf8'));
}
