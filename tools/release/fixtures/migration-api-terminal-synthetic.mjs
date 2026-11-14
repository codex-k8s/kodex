import {fingerprint} from '../scoped-release.mjs';
import {canonicalAuthorityReadbackContract} from '../authority-readback-targets.mjs';
import {catalogAuthorityBaselineSchema,catalogAuthorityBaselineSQLSHA256} from '../catalog-authority-baseline.mjs';
import {componentMigrationAPITerminalSchema} from '../component-migration-api-terminal.mjs';

// Только DTO для adversarial boundary tests; этот helper не читает actual DB,
// не создаёт owner receipt и не является fixture живой canonical authority.
export function syntheticMigrationBaseline(target){
 const now=new Date().toISOString(),uid=target.clusterUID;
 const body={schema:catalogAuthorityBaselineSchema,policyRevision:106,operationID:uid,registryUID:uid,publisherUID:uid,
  policySHA256:'a'.repeat(64),registryDataSHA256:'b'.repeat(64),registrySHA256:'c'.repeat(64),publisherSpecSHA256:'d'.repeat(64),
  registryRevision:2,snapshotRevision:4,snapshotSHA256:'e'.repeat(64),rotationPlanSHA256:'f'.repeat(64),rotationEvidenceSHA256:'1'.repeat(64),
  targetSHA256:fingerprint(target),...canonicalAuthorityReadbackContract(),actualReadbackCount:16,sqlSHA256:catalogAuthorityBaselineSQLSHA256,
  observedAt:now,scope:Object.fromEntries(['context','clusterUID','namespaceUID','installationUID','installationSHA256'].map(key=>[key,target[key]])),
  postgres:{pod:'kodex-postgresql-0',uid,ownerUID:uid},postgresBackend:{systemIdentifier:'18446744073709551615',databaseOID:'20000',database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null}};
 return {...body,baselineSHA256:fingerprint(body)};
}

export function syntheticMigrationTerminal({isolation,closed,deployment,sourceSHA}){
 const authority=structuredClone(isolation.migrationRestore.authority);
 const body={schema:componentMigrationAPITerminalSchema,profile:isolation.migrationRestore.profile,migrationVersion:20261114130000,
  migrationPlanSHA256:'1'.repeat(64),migrationEvidenceSHA256:'2'.repeat(64),migrationTargetSHA256:'3'.repeat(64),publicationSHA256:'4'.repeat(64),
  handoffSHA256:'5'.repeat(64),openPlanSHA256:'6'.repeat(64),openEvidenceSHA256:'7'.repeat(64),executionSHA256:'8'.repeat(64),cohortSHA256:'9'.repeat(64),
  apiSpecSHA256:fingerprint(deployment.spec),deploymentUID:deployment.metadata.uid,jobUID:isolation.serviceUID,podUID:isolation.serviceUID,
  jobName:'control-plane-migrate-20261114130000',targetSHA256:fingerprint(isolation.target),trafficPlanSHA256:fingerprint(isolation),
  serviceUID:isolation.serviceUID,closePlanSHA256:fingerprint(closed),maintenanceID:isolation.maintenanceID,
  sourceCompatibility:{profile:isolation.migrationRestore.profile,executorSHA:sourceSHA,migrationSourceSHA:'a'.repeat(40),postconditionSQLSHA256:'a'.repeat(64)},
  postcondition:{profile:isolation.migrationRestore.profile,sqlSHA256:'a'.repeat(64),postgresSHA256:'b'.repeat(64),backendSHA256:'c'.repeat(64),schemaCatalogSHA256:'d'.repeat(64),observedAt:new Date().toISOString()},
  authority,observedAt:authority.observedAt};
 return {...body,terminalSHA256:fingerprint(body)};
}
