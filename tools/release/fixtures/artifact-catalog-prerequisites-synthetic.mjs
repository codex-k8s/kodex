import assert from 'node:assert/strict';
import {createECDH} from 'node:crypto';
import {mkdirSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {ownerFixture,publication as syntheticPublication,uid,before,after} from './issuer-migration-synthetic.mjs';
import {planIssuerMigration,executeIssuerMigration,issuerMigrationIO,issuerMigrationJournalHeader,readArtifactRetentionIssuerMigrationReady} from '../artifact-retention-issuer-migration.mjs';
import {inspectArtifactRetentionIssuerMaterial,planArtifactRetentionIssuerMaterial,applyArtifactRetentionIssuerMaterial,activateArtifactRetentionIssuerLogin,readArtifactRetentionIssuerReady} from '../artifact-retention-issuer-material.mjs';
import {authorityOwnerDatabaseArgs} from '../migration-owner-database.mjs';
import {privateJournal} from '../../dev/role-image-acceptance.mjs';
import {fingerprint} from '../scoped-release.mjs';

const role='ira_artifact_retention_issuer_g1';
const signer='artifact-retention-platform-worker-grant-signer';
const databaseSecret='internal-rpc-authority-artifact-retention-issuer-postgresql';
const trust='control-plane-application-grants';
const trustKey='artifact-retention.platform-worker.public.jwk';
const marker='kodex.dev/artifact-retention-issuer-material-plan';
const namespace='kodex-system';
const roleJobName='authority-artifact-retention-issuer-20261115000200';

// Общее состояние состоит только из JSON-значений: тот же transport пригоден
// для owner factory в процессе теста и для отдельного настоящего CLI subprocess.
// Неизвестная команда передаётся следующему transport, а не получает ложный PASS.
export function artifactCatalogPrerequisiteTransport(state,rawArgs,input='') {
  const args=[...rawArgs];
  for(let index=0;index<args.length;){
    if(/^--request-timeout=[0-9]{1,3}s$/.test(args[index])){
      args.splice(index,1);
    }else if(['--kubeconfig','--context','--request-timeout'].includes(args[index])){
      if(args[index]==='--context')assert.equal(args[index+1],state.target.context);
      assert.ok(args[index+1]);args.splice(index,2);
    }else index++;
  }
  const selectedNamespace=args.indexOf('-n');
  if(selectedNamespace!==-1&&args[selectedNamespace+1]!==namespace)return undefined;
  const read=(kind,name)=>args.includes('get')&&args[args.indexOf('get')+1]===kind&&args[args.indexOf('get')+2]===name;
  if(args.includes('exec')&&args.includes('--dbname=internal_rpc_authority')){
    if(args.includes('\\password '+role)){
      assert.deepEqual(args.slice(0,authorityOwnerDatabaseArgs.length),authorityOwnerDatabaseArgs);
      assert.ok(args.some(value=>value.includes('FOR UPDATE')));
      assert.ok(args.some(value=>value.includes("SET LOCAL password_encryption='scram-sha-256';")));
      assert.ok(state.materialPassword&&!state.verifierSHA256);
      assert.equal(input,state.materialPassword+'\n'+state.materialPassword+'\n');
      state.verifierSHA256='7'.repeat(64);state.writes.login++;
      return 'ISSUER_LOGIN_INITIALIZED\n';
    }
    if(args.length!==authorityOwnerDatabaseArgs.length)return undefined;
    assert.deepEqual(args,authorityOwnerDatabaseArgs);
    if(input.includes('pg_control_system()'))return JSON.stringify(state.backend);
    if(input.includes('pg_catalog.pg_authid'))return JSON.stringify({configured:state.verifierSHA256!==null,scram:state.verifierSHA256!==null,verifierSHA256:state.verifierSHA256});
    if(input.includes("'validRole'"))return JSON.stringify({schema:state.roleJob?20261115000200:20261115000100,validRole:!!state.roleJob});
    if(input.includes("'roleExists'"))return JSON.stringify(state.roleJob?after():before());
    return undefined;
  }
  if(args.includes('create')&&input.trimStart().startsWith('{')){
    const object=JSON.parse(input);
    if(object.kind==='Job'&&object.metadata?.name===roleJobName){
      assert.equal(state.roleJob,null);state.writes.roleJobs++;
      state.roleJob=structuredClone(object);state.roleJob.metadata.uid=uid(806);
      state.roleJob.status={succeeded:1,conditions:[{type:'Complete',status:'True'}]};
      const spec=structuredClone(object.spec.template.spec);
      const terminal=container=>({name:container.name,imageID:'containerd://'+container.image.split('@')[1],restartCount:0,state:{terminated:{exitCode:0}}});
      state.rolePod={kind:'Pod',metadata:{name:'synthetic-artifact-issuer-migrator',namespace,uid:uid(807),ownerReferences:[{kind:'Job',name:object.metadata.name,uid:state.roleJob.metadata.uid,controller:true}]},spec,
        status:{phase:'Succeeded',containerStatuses:spec.containers.map(terminal),initContainerStatuses:(spec.initContainers??[]).map(terminal)}};
      return JSON.stringify(state.roleJob);
    }
    if(object.kind==='Secret'&&[signer,databaseSecret].includes(object.metadata?.name)){
      assert.equal(object.immutable,true);assert.equal(object.metadata.namespace,namespace);
      assert.equal(state.secrets[object.metadata.name],undefined);
      state.writes.secretCreates++;
      state.secrets[object.metadata.name]={uid:uid(820+state.writes.secretCreates),resourceVersion:'21',owner:object.metadata.labels['app.kubernetes.io/part-of'],immutable:true,annotations:object.metadata.annotations,data:object.data};
      return 'secret/'+object.metadata.name;
    }
    return undefined;
  }
  if(args.includes('patch')&&args.includes('secret')&&args.includes(trust)){
    const patch=JSON.parse(readFileSync(args[args.indexOf('--patch-file')+1],'utf8')),value=state.secrets[trust];
    assert.deepEqual(patch.slice(0,2),[{op:'test',path:'/metadata/uid',value:value.uid},{op:'test',path:'/metadata/resourceVersion',value:value.resourceVersion}]);
    assert.equal(patch.length,4);assert.equal(patch[2].op,'add');assert.equal(patch[2].path,'/data/'+trustKey);
    assert.equal(patch[3].op,'add');assert.equal(patch[3].path,'/metadata/annotations/'+marker.replace('/','~1'));
    assert.equal(value.data[trustKey],undefined);
    value.data[trustKey]=patch[2].value;value.annotations[marker]=patch[3].value;value.resourceVersion='22';state.writes.trustPatches++;
    return 'secret/'+trust;
  }
  if(args.includes('get')&&args[args.indexOf('get')+1]==='secret'){
    const name=args[args.indexOf('get')+2];
    if(name==='internal-rpc-authority-postgres-migration'&&args.at(-1).startsWith('jsonpath='))return uid(810)+'|1|kodex';
    if(![signer,databaseSecret,trust].includes(name))return undefined;
    const value=state.secrets[name];if(!value)return '';
    const keyed=args.at(-1).match(/{{index \.data "([^"]+)"}}/);
    if(keyed){assert.ok(Object.hasOwn(value.data,keyed[1]));return value.uid+'\n'+value.data[keyed[1]];}
    assert.ok(args.at(-1).startsWith('go-template='));
    return [value.uid,value.resourceVersion,value.owner,String(value.immutable),value.annotations[marker]??'','yes',...Object.keys(value.data),''].join('\n');
  }
  if(read('namespace','kube-system'))return JSON.stringify({metadata:{uid:state.target.clusterUID}});
  if(read('namespace',namespace))return JSON.stringify({metadata:{uid:state.target.namespaceUID,labels:{'app.kubernetes.io/part-of':'kodex'}}});
  if(read('configmap','kodex-installation-profile'))return JSON.stringify(state.installation);
  if(read('job',roleJobName))return state.roleJob?JSON.stringify(state.roleJob):'';
  if(read('pod','synthetic-artifact-issuer-migrator'))return JSON.stringify(state.rolePod);
  if(args.includes('get')&&args[args.indexOf('get')+1]==='endpointslices'&&args.includes('kubernetes.io/service-name=internal-rpc-authority-postgresql-rw'))return JSON.stringify({items:[state.endpoint]});
  if(args.includes('get')&&args[args.indexOf('get')+1]==='pods'&&args.includes('-l')&&args.includes('batch.kubernetes.io/controller-uid='+state.roleJob?.metadata.uid))return JSON.stringify({items:state.rolePod?[state.rolePod]:[]});
  if(args.includes('get')&&['jobs','pods'].includes(args[args.indexOf('get')+1])&&args.includes('--chunk-size=0')){
    const items=args[args.indexOf('get')+1]==='jobs'?(state.roleJob?[state.roleJob]:[]):[state.resources['pod/kodex-postgresql-0'],...(state.rolePod?[state.rolePod]:[])];
    return '\n'+items.map(item=>{
      const spec=item.spec.template?.spec??item.spec;
      return [item.metadata.name,item.metadata.uid,spec.serviceAccountName??'',spec.serviceAccount??'',(spec.volumes??[]).map(volume=>volume.secret?.secretName).filter(Boolean).join(' '),'','','','',''].join('\t')+'\n';
    }).join('');
  }
  if(args.includes('get')){
    const kind=args[args.indexOf('get')+1],name=args[args.indexOf('get')+2],object=state.resources[kind+'/'+name];
    if(object)return JSON.stringify(object);
  }
  return undefined;
}

// Материал создаётся только в собственном0700 каталоге и никогда не берётся
// из env, kubeconfig или действующего stand. Production owner APIs сами пишут
// планы/journals и затем повторно читают их через настоящий Ready boundary.
export async function artifactCatalogPrerequisitesFixture(t,{source,target,installation,postgres,backend,publication}={}) {
  assert.ok(source?.path&&source.revision&&target&&installation&&postgres&&backend);
  assert.equal(postgres.pod,'kodex-postgresql-0');
  assert.equal(backend.database,'internal_rpc_authority');
  assert.equal(installation.metadata.uid,target.installationUID);
  assert.equal(fingerprint(installation.data),target.installationSHA256);
  const directory=mkdtempSync(join(tmpdir(),'kodex-artifact-prerequisites-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const seed=ownerFixture(),resources={};
  const replacements={[uid(2)]:target.clusterUID,[uid(3)]:target.namespaceUID,[uid(4)]:postgres.uid,[uid(5)]:postgres.ownerUID};
  const remap=value=>JSON.parse(JSON.stringify(value),(_,part)=>typeof part==='string'&&Object.hasOwn(replacements,part)?replacements[part]:part);
  for(const [kind,name] of [['statefulset','kodex-postgresql'],['pod','kodex-postgresql-0'],['service','internal-rpc-authority-postgresql-rw'],['serviceaccount','internal-rpc-authority-migrator'],['networkpolicy','internal-rpc-authority-migrator'],['networkpolicy','internal-rpc-authority-postgresql-from-migrator'],['bundles.trust.cert-manager.io','internal-rpc-authority-postgresql-ca'],['configmap','internal-rpc-authority-postgresql-ca']]){
    resources[kind+'/'+name]=remap(JSON.parse(seed.kube(['-n',namespace,'get',kind,name,'-o','json'])));
  }
  const state={target:structuredClone(target),installation:structuredClone(installation),backend:structuredClone(backend),resources,endpoint:remap(seed.state.endpoint),roleJob:null,rolePod:null,
    secrets:{[trust]:{uid:uid(824),resourceVersion:'20',owner:'kodex',immutable:false,annotations:{'fixture.example.invalid/retained':'yes'},data:{'email-bridge.platform-worker.public.jwk':'Zml4dHVyZQ=='}}},
    verifierSHA256:null,writes:{roleJobs:0,secretCreates:0,trustPatches:0,login:0}};
  const kube=(args,input)=>{const result=artifactCatalogPrerequisiteTransport(state,args,input);assert.notEqual(result,undefined,'unhandled prerequisite fixture command '+JSON.stringify(args));return result;};
  if(!publication){
    publication=syntheticPublication();const plan=publication.publicationPlan;
    plan.context=target.context;plan.clusterUID=target.clusterUID;plan.namespaceUID=target.namespaceUID;
    plan.receipt.sourceSHA=source.revision;plan.image.sourceSHA=source.revision;plan.image.tag='stable-'+source.revision+'-'+plan.image.digest.slice(-12);
    publication.publicationEvidence[0].planSHA256=fingerprint(plan);
  }
  const issuerMigration={planPath:join(directory,'issuer-plan.json'),evidencePath:join(directory,'issuer-evidence.jsonl'),sourceSHA:source.revision};
  const issuerPlan=planIssuerMigration({source,target,publication,evidencePath:issuerMigration.evidencePath},kube);
  writeFileSync(issuerMigration.planPath,JSON.stringify(issuerPlan)+'\n',{mode:0o600});
  const issuerJournal=privateJournal(issuerMigration.evidencePath,issuerMigrationJournalHeader(issuerPlan));
  try{assert.equal((await executeIssuerMigration('apply',issuerPlan,issuerJournal,issuerMigrationIO(issuerPlan,kube))).status,'PASS');}finally{issuerJournal.close();}
  const issuerReady=await readArtifactRetentionIssuerMigrationReady({...issuerMigration,target},kube);
  const materialDirectory=join(directory,'material');mkdirSync(materialDirectory,{mode:0o700});
  const generated=createECDH('prime256v1');generated.generateKeys();
  const point=generated.getPublicKey(),scalar=generated.getPrivateKey();
  const core={kty:'EC',crv:'P-256',use:'sig',alg:'ES256',kid:'artifact-retention-platform-worker-g1',x:point.subarray(1,33).toString('base64url'),y:point.subarray(33).toString('base64url')};
  state.materialPassword='c'.repeat(64);
  const files={'private.jwk':JSON.stringify({...core,key_ops:['sign'],d:Buffer.concat([Buffer.alloc(32-scalar.length),scalar]).toString('base64url')}),'public.jwk':JSON.stringify({...core,key_ops:['verify']}),
    'postgresql-password':state.materialPassword,'postgresql-username':role,'postgresql-dsn':`postgresql://${role}:${state.materialPassword}@internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local:5432/internal_rpc_authority?sslmode=verify-full&sslrootcert=/var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem`};
  for(const [name,value] of Object.entries(files))writeFileSync(join(materialDirectory,name),value,{mode:0o600});
  const materialPlan=planArtifactRetentionIssuerMaterial({target,sourceRevision:source.revision,material:inspectArtifactRetentionIssuerMaterial(materialDirectory),kube});
  const material={planPath:join(directory,'material-plan.json'),evidencePath:join(directory,'material-evidence.jsonl'),sourceSHA:source.revision};
  writeFileSync(material.planPath,JSON.stringify(materialPlan)+'\n',{mode:0o600});
  const materialJournal=privateJournal(material.evidencePath,{planSHA256:fingerprint(materialPlan),schema:materialPlan.schema});
  try{
    const request={plan:materialPlan,materialDirectory,kube,journal:materialJournal};
    assert.equal(applyArtifactRetentionIssuerMaterial(request).status,'MATERIAL_STAGED');
    assert.equal(activateArtifactRetentionIssuerLogin(request).status,'LOGIN_INITIALIZED');
  }finally{materialJournal.close();}
  delete state.materialPassword;
  const materialReady=readArtifactRetentionIssuerReady({...material,target},kube);
  assert.deepEqual(state.writes,{roleJobs:1,secretCreates:2,trustPatches:1,login:1});
  return {directory,state,kube,issuerMigration,material,issuerPlan,materialPlan,issuerReady,materialReady,publication};
}
