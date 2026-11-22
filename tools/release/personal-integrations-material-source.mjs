import {createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {inspectSource,sourceGitArguments} from './application-source.mjs';

const fail=(ok,code)=>{if(!ok)throw Error(code);};
const revision=value=>typeof value==='string'&&/^[a-f0-9]{40}$/.test(value);
const paths=Object.freeze(['tools/release/artifact-retention-issuer-material.mjs',
 'tools/release/artifact-retention-issuer-login-precondition.sql','tools/release/artifact-retention-issuer-login-readback.sql',
 'services/internal/internal-rpc-authority/cmd/cli/migrations/20261115000200_artifact_retention_issuer.sql']);

// Material LOGIN имеет собственный immutable source. Более новый cohort049
// допускает только ancestor с прежними точными helper/SQL bytes. Source hash
// material не переписывается текущим source и не заменяет его readiness receipt.
export function verifyPersonalIntegrationsMaterialSource({source,sourceSHA,materialSourceSHA}){
 fail(revision(sourceSHA)&&revision(materialSourceSHA)&&inspectSource(source).revision===sourceSHA,
  'PERSONAL_INTEGRATIONS_MATERIAL_SOURCE_REJECTED');
 const git=(...args)=>execFileSync('git',sourceGitArguments(source,...args),{timeout:10000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
 try{git('merge-base','--is-ancestor',materialSourceSHA,sourceSHA);}catch{throw Error('PERSONAL_INTEGRATIONS_MATERIAL_SOURCE_UNRELATED');}
 const files=paths.map(path=>{
  let before,current;
  try{before=git('show',`${materialSourceSHA}:${path}`);current=git('show',`${sourceSHA}:${path}`);}catch{throw Error('PERSONAL_INTEGRATIONS_MATERIAL_SOURCE_MISSING');}
  fail(before.equals(current),'PERSONAL_INTEGRATIONS_MATERIAL_SOURCE_CHANGED');
  return {path,sha256:createHash('sha256').update(current).digest('hex')};
 });
 fail(inspectSource(source).revision===sourceSHA,'PERSONAL_INTEGRATIONS_MATERIAL_SOURCE_REJECTED');
 return {schema:'kodex.personal-integrations-material-source/v1',materialSourceSHA,sourceSHA,files};
}
