import {createHash} from 'node:crypto';
import {readdirSync,readFileSync} from 'node:fs';
import {join} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {validPersonalIntegrationsMigrationProof} from './personal-integrations-migration-contract.mjs';
import {personalStagedInputsSchema} from './artifact-retention-issuer-inputs.mjs';
import {workflowFileProvenanceMigration} from './workflow-file-provenance-migration-contract.mjs';

export const artifactGrantSchemaVersion=20261121000100;
export const artifactGrantPreviousSchemaVersion=20261114170000;
export const artifactGrantSchemaCommand='up-artifact-retention-grant-catalog';
export const artifactGrantSchemaJobName='control-plane-migrate-'+artifactGrantSchemaVersion;
export const artifactGrantSchemaReceiptType='PERSONAL_ARTIFACT_GRANT_CATALOG_APPLIED';
export const artifactGrantCatalogRequired=plan=>plan.profile==='personal-integrations-backup'&&
 plan.personalIntegrationsInputs?.schema===personalStagedInputsSchema;
const application='artifact-retention';
const guardTables={
 trusted_workload_generations:['automation-scheduler','control-plane','email-bridge','image-admission','image-admission-controller','image-promotion','integration-gateway','interaction-gateway','role-image-builder','runtime-controller','secret-broker','session-archive','stt-tts-service'],
 worker_grant_high_watermarks:['automation-scheduler','control-plane','email-bridge','image-admission','image-promotion','integration-gateway','interaction-gateway','role-image-builder','runtime-controller','secret-broker','session-archive'],
};
const requireValue=(ok,code='ARTIFACT_GRANT_CATALOG_PROOF_REJECTED')=>{if(!ok)throw Error(code);};
const literal=value=>"'"+value.replaceAll("'","''")+"'";
const sha=bytes=>createHash('sha256').update(bytes).digest('hex');
const shape=value=>typeof value==='string'&&/^[a-f0-9]{64}$/.test(value);
const personalMigrationFiles=JSON.parse(readFileSync(new URL('./personal-integrations-schema-manifest.json',import.meta.url))).migrationPins
 .map(pin=>pin.file).sort();

// Команды Goose ограничены собственными heads. Один опубликованный мигратор
// может содержать закреплённый ряд до 049 и точные 050/051. Неизвестный SQL
// между уже проверенными версиями также не входит в состав перехода.
export function hasPersonalBackupArtifactMigrationChain(directory,personalMigrationFile,{allowOriginalHead=false}={}){
 // Goose читает все *.sql и разбирает числовой префикс без фиксированной ширины.
 const names=readdirSync(directory).filter(name=>name.endsWith('.sql')).sort();
 if(personalMigrationFiles.at(-1)!==personalMigrationFile||!personalMigrationFiles.every((file,index)=>names[index]===file))return false;
 const tail=names.slice(personalMigrationFiles.length),wfp=workflowFileProvenanceMigration;
 if(tail.length===0)return allowOriginalHead;
 const readMigration=name=>readFileSync(join(directory,name));
 return (tail.length===1||tail.length===2)&&tail[0]===wfp.artifactFile&&
  (tail.length===1||tail[1]===wfp.migrationFile)&&sha(readMigration(wfp.artifactFile))===wfp.artifactSHA256&&
  (tail.length===1||sha(readMigration(wfp.migrationFile))===wfp.migrationSHA256);
}
const predecessorChecks=Object.entries(guardTables).map(([table,workloads])=>{
 const definition=ids=>'CHECK ((workload_id = ANY (ARRAY['+ids.map(id=>literal(id)+'::text').join(', ')+'])))';
 return {table,constraint:table+'_workload_id_check',before:definition(workloads),after:definition([...workloads,application])};
});
function replacePoint(text,anchor,value){
 requireValue(text.split(anchor).length===2);
 return text.replace(anchor,()=>value);
}

// Сохраняет полный original049 catalog digest, включая ACL/functions/graph.
// Только два точно проверенных новых CHECK проецируются к прежней форме;
// фактический head и полная ordered Goose история включают одну новую миграцию.
export function artifactGrantCatalogPostconditionSQL(original,originalSHA256){
 requireValue(sha(original)===originalSHA256);
 let source=original.toString('utf8');
 const history=[...source.matchAll(/\$history\$(\[[\s\S]*?\])\$history\$/g)];
 requireValue(history.length===1);
 const versions=JSON.parse(history[0][1]);
 requireValue(Array.isArray(versions)&&versions.every(row=>Array.isArray(row)&&row.length===2&&Number.isSafeInteger(row[0])&&row[1]===true)&&
  versions.at(-1)?.[0]===artifactGrantPreviousSchemaVersion&&!versions.some(row=>row[0]===artifactGrantSchemaVersion));
 versions.push([artifactGrantSchemaVersion,true]);
 source=replacePoint(source,history[0][0],'$history$'+JSON.stringify(versions)+'$history$');
 const predicate=({table,constraint,after})=>
  'c.conrelid = '+literal('control_plane.'+table)+'::regclass AND c.conname = '+literal(constraint)+' AND pg_get_constraintdef(c.oid) = '+literal(after);
 const cases=predecessorChecks.map(check=>'WHEN '+predicate(check)+' THEN '+literal(check.before)).join('\n      ');
 source=replacePoint(source,'c.coninhcount,c.connoinherit,pg_get_constraintdef(c.oid)',
  'c.coninhcount,c.connoinherit,CASE '+cases+' ELSE pg_get_constraintdef(c.oid) END');
 const checks=predecessorChecks.map(check=>
  '(SELECT count(*)=1 FROM pg_constraint c WHERE '+predicate(check)+' AND c.convalidated AND c.conenforced AND c.contype=\'c\')');
 const trust="(SELECT count(*)=1 FROM control_plane.trusted_workload_generations WHERE workload_id='artifact-retention' AND credential_generation=1 AND enabled)";
 return replacePoint(source," 'schemaCatalog',value,"," 'artifactGrantCatalogMatches',"+[...checks,trust].join(' AND ')+",\n 'schemaCatalog',value,");
}

export function verifyArtifactGrantCatalogProof(proof,backup){
 const {artifactGrantCatalogMatches,gooseVersion,...body}=proof??{};
 requireValue(artifactGrantCatalogMatches===true&&gooseVersion===artifactGrantSchemaVersion&&
  validPersonalIntegrationsMigrationProof({...body,gooseVersion:artifactGrantPreviousSchemaVersion},backup,false));
 return {schemaVersion:gooseVersion,schemaCatalogSHA256:fingerprint(proof.schemaCatalog)};
}

export function artifactGrantCatalogReceipt(migration,events){
 const receipts=events.filter(row=>row.type===artifactGrantSchemaReceiptType);
 if(!receipts.length)return undefined;
 requireValue(receipts.length===1);
 const [receipt]=receipts;
 const intent=events.filter(row=>row.type==='PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_INTENT');
 const ack=events.filter(row=>row.type==='PERSONAL_ARTIFACT_GRANT_CATALOG_CREATE_ACK');
 requireValue(intent.length===1&&ack.length===1&&events.indexOf(intent[0])<events.indexOf(ack[0])&&events.indexOf(ack[0])<events.indexOf(receipt)&&
  (!artifactGrantCatalogRequired(migration)||intent[0].admission==='STAGED_COHORT'&&intent[0].sourceSHA===migration.source.revision)&&
  receipt.planSHA256===intent[0].planSHA256&&receipt.planSHA256===ack[0].planSHA256&&receipt.jobUID===ack[0].jobUID&&
  receipt.jobSpecSHA256===ack[0].jobSpecSHA256&&receipt.migrationPlanSHA256===fingerprint(migration)&&
  receipt.schemaVersion===artifactGrantSchemaVersion&&receipt.jobName===artifactGrantSchemaJobName&&
  /^[a-f0-9-]{36}$/.test(receipt.jobUID??'')&&
  [receipt.planSHA256,receipt.schemaCatalogSHA256,receipt.sqlSHA256,receipt.jobSpecSHA256].every(shape));
 return receipt;
}
