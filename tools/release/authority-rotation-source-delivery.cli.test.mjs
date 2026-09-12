import assert from 'node:assert/strict';
import test from 'node:test';
import {chmodSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {main} from './authority-rotation-source-delivery.mjs';
import {authorityGoImage,createPublisherSourceCAS,createSourceMigrationJob} from './authority-rotation-source-delivery-model.mjs';
import {fingerprint} from './scoped-release.mjs';

const context='staging-exact',uidA='14390000-0000-4000-8000-00000000000a',uidB='14390000-0000-4000-8000-00000000000b';
const env=Object.entries({GOMODCACHE:'/go/pkg/mod',GOCACHE:'/go/build-cache/cache',GOWORK:'off',GOTOOLCHAIN:'local',GOTMPDIR:'/go/build-cache/tmp',HOME:'/go/build-cache/home'}).map(([name,value])=>({name,value}));
const cache=(name,path=`/srv/kodex-dev/cache/${name}`)=>({name,hostPath:{path,type:'Directory'}}),mount=(name,mountPath,readOnly)=>({name,mountPath,readOnly});
const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};

test('CLI plan связывает проверенный --source без несуществующего inspectSource.path',async t=>{
 const directory=mkdtempSync(join(tmpdir(),'asp-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const source='/srv/kodex-dev/next',revision='a'.repeat(40),before=publisher(),rendered=migration(),{sa,egress,ingress}=prerequisites();
 const output=join(directory,'plan.json'),capabilityPath=join(directory,'capability.json');
 writeFileSync(capabilityPath,JSON.stringify({version:1,protocol:1,revision,go:'go1.26.6',publisherSHA256:'b'.repeat(64),migrations:[{name:'fixture.sql',sha256:'c'.repeat(64)}]}),{mode:0o600});
 const registry={metadata:{uid:uidB},data:{registry:'18'}};let dryRuns=0;
 const io={inspectSource:path=>({revision:path===source?revision:'c'.repeat(40)}),sourcePublisherExecutable:()=> 'd'.repeat(64),run:(command,args,input)=>{
  if(command==='yq')return JSON.stringify([rendered,sa,egress,ingress]);
  const argv=args.slice(args.indexOf('--context')+2);
  if(argv[0]==='config')return context;
  if(argv[0]==='get'){
   const [kind,name]=argv.slice(1);
   if(kind==='namespace')return JSON.stringify({metadata:{uid:uidA,labels:{'kodex.dev/environment':'staging'}}});
   if(kind==='serviceaccount')return JSON.stringify(sa);
   if(kind==='networkpolicy')return JSON.stringify(name==='internal-rpc-authority-migrator'?egress:ingress);
   if(kind==='deployment')return JSON.stringify(before);
   if(kind==='configmap')return JSON.stringify(registry);
   if(kind==='deployments'||kind==='replicasets,pods')return JSON.stringify({items:[]});
  }
  if(argv[0]==='create'){
   assert.ok(argv.includes('--dry-run=server'));dryRuns++;
   return JSON.stringify({...JSON.parse(input),metadata:{...JSON.parse(input).metadata,uid:uidB}});
  }
  throw new Error('Unexpected mutation or read');
 }};
 const args=expected=>['plan','--context',context,'--source',source,'--revision',expected,'--render',join(directory,'render.yaml'),'--capability',capabilityPath,'--output',output];
 await main(args(revision),io);
 const plan=JSON.parse(readFileSync(output,'utf8'));
 assert.equal(plan.source,source);assert.equal(plan.revision,revision);assert.equal(dryRuns,1);
 assert.equal(createPublisherSourceCAS(before,plan).spec.template.spec.volumes.find(v=>v.name==='dev-source').hostPath.path,source);
 await assert.rejects(main(args('e'.repeat(40)),io),/EXACT_CLEAN_SOURCE_REQUIRED/);assert.equal(dryRuns,1);
});

function publisher(){return {kind:'Deployment',metadata:{name:'internal-rpc-authority-publisher',namespace:'kodex-system',uid:'14390000-0000-4000-8000-000000000001',resourceVersion:'7'},spec:{replicas:1,template:{metadata:{},spec:{containers:[{name:'publisher',image:authorityGoImage,workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-hot-reload.sh'],args:['services/internal/internal-rpc-authority','./cmd/internal-rpc-authority-publisher','publisher'],env,volumeMounts:[mount('dev-source','/workspace',true),mount('dev-go-mod','/go/pkg/mod',true),mount('dev-go-sumdb','/go/pkg/sumdb',true),mount('dev-go-tools','/go/tools',true),mount('dev-build-publisher','/go/build-cache',false),mount('tmp','/tmp',false)]}],volumes:[cache('dev-source','/srv/kodex-dev/workspace-release1226'),cache('dev-go-mod'),cache('dev-go-sumdb'),cache('dev-go-tools'),cache('dev-build-publisher'),{name:'tmp',emptyDir:{}}]}}}};}
function migration(){return {kind:'Job',metadata:{name:'internal-rpc-authority-migrate',namespace:'kodex-system'},spec:{template:{metadata:{labels},spec:{serviceAccountName:'internal-rpc-authority-migrator',automountServiceAccountToken:false,restartPolicy:'Never',containers:[{name:'migrate',image:authorityGoImage,workingDir:'/workspace/services/internal/internal-rpc-authority',command:['/workspace/tools/dev/run-go-command.sh'],args:['services/internal/internal-rpc-authority','./cmd/cli','up'],env:[...env,{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_DSN_FILE',value:'/var/run/secrets/kodex/internal-rpc-authority/postgres/dsn'},{name:'INTERNAL_RPC_AUTHORITY_POSTGRES_TLS_SERVER_NAME',value:'internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local'}],volumeMounts:[mount('dev-source','/workspace',true),mount('dev-go-mod','/go/pkg/mod',true),mount('dev-go-sumdb','/go/pkg/sumdb',true),mount('dev-go-tools','/go/tools',true),mount('dev-build-migrate','/go/build-cache',false),mount('tmp','/tmp',false),mount('postgresql-credentials','/var/run/secrets/kodex/internal-rpc-authority/postgres',true),mount('postgresql-ca','/var/run/config/kodex/internal-rpc-authority/postgresql',true)]}],volumes:[cache('dev-source','/srv/kodex-dev/old'),cache('dev-go-mod'),cache('dev-go-sumdb'),cache('dev-go-tools'),cache('dev-build-migrate'),{name:'tmp',emptyDir:{}},{name:'postgresql-credentials',secret:{secretName:'internal-rpc-authority-postgres-migration'}},{name:'postgresql-ca',configMap:{name:'internal-rpc-authority-postgresql-ca'}}]}}}};}
function prerequisites(){
 const sa={kind:'ServiceAccount',metadata:{name:'internal-rpc-authority-migrator',labels},automountServiceAccountToken:false};
 const egress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-migrator'},spec:{podSelector:{matchLabels:labels},policyTypes:['Ingress','Egress'],egress:[{to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}}}],ports:[{port:5432,protocol:'TCP'}]},{to:[{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kube-system'}},podSelector:{matchLabels:{'k8s-app':'kube-dns'}}}],ports:[{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}]}]}};
 const ingress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-postgresql-from-migrator'},spec:{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}},policyTypes:['Ingress'],ingress:[{from:[{podSelector:{matchLabels:labels}}],ports:[{port:5432,protocol:'TCP'}]}]}};return {sa,egress,ingress};
}

test('public source-delivery CLI keeps one CREATE across UNKNOWN, observe and publisher',async t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-source-cli-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const before=publisher(),rendered=migration(),registry={kind:'ConfigMap',metadata:{uid:'14390000-0000-4000-8000-000000000002'},data:{registry:'18'}},plan={version:1,kind:'AUTHORITY_SOURCE_DELIVERY',intentID:'14390000-0000-4000-8000-000000000003',context,namespaceUID:'14390000-0000-4000-8000-000000000004',source:'/srv/kodex-dev/next',revision:'a'.repeat(40),capability:{publisherSHA256:'b'.repeat(64)},publisher:{uid:before.metadata.uid,resourceVersion:before.metadata.resourceVersion,beforeSpecSHA256:fingerprint(before.spec),beforeSource:'/srv/kodex-dev/workspace-release1226',beforeRevision:'c'.repeat(40),beforeExecutableSHA256:'d'.repeat(64),beforeAnnotations:{},beforeAnnotationsPresent:false},migration:{rendered,renderedSpecSHA256:fingerprint(rendered.spec)},boundary:{registryUID:registry.metadata.uid,registryDataSHA256:fingerprint(registry.data),neighborsSHA256:fingerprint([])}};
 plan.publisher.desiredSpecSHA256=fingerprint(createPublisherSourceCAS(before,plan).spec);const expected=createSourceMigrationJob(rendered,plan),jobA={...structuredClone(expected),metadata:{...expected.metadata,uid:uidA}},jobB={...structuredClone(expected),metadata:{...expected.metadata,uid:uidB}};
 const planPath=join(directory,'plan.json'),receipt=join(directory,'receipt.json');writeFileSync(planPath,JSON.stringify(plan),{mode:0o600});let job=null,currentPublisher=before,createCount=0,replaceCount=0;const rolloutCalls=[];
 const {sa,egress,ingress}=prerequisites();let liveEgress=egress;const run=(_command,args,input)=>{const argv=args.slice(args.indexOf('--context')+2);if(argv[0]==='config')return context;if(argv[0]==='get'){
  const [kind,name]=argv.slice(1);if(kind==='namespace')return JSON.stringify({metadata:{uid:plan.namespaceUID,labels:{'kodex.dev/environment':'staging'}}});if(kind==='serviceaccount')return JSON.stringify(sa);if(kind==='networkpolicy')return JSON.stringify(name==='internal-rpc-authority-migrator'?liveEgress:ingress);if(kind==='deployment')return JSON.stringify(currentPublisher);if(kind==='configmap')return JSON.stringify(registry);if(kind==='deployments')return JSON.stringify({items:[]});if(kind==='job')return job?JSON.stringify(job):'';if(kind==='replicasets,pods')return JSON.stringify({items:[]});
  }if(argv[0]==='create'){createCount++;job=structuredClone(jobA);throw new Error('lost ACK');}if(argv[0]==='replace'){replaceCount++;currentPublisher=JSON.parse(input);return JSON.stringify(currentPublisher);}if(argv[0]==='rollout'){rolloutCalls.push({_command,args});return '';}throw new Error(`unexpected mock call: ${argv.join(' ')}`);};
 const executable=()=>currentPublisher.spec.template.spec.volumes.find(value=>value.name==='dev-source').hostPath.path===plan.source?plan.capability.publisherSHA256:plan.publisher.beforeExecutableSHA256;
 const io={run,inspectSource:()=>({revision:plan.revision}),sourcePublisherExecutable:executable},args=(command,...extra)=>[command,'--context',context,'--plan',planPath,'--migration-receipt',receipt,...extra];
 liveEgress=structuredClone(egress);liveEgress.spec.egress[0].to[0].podSelector.matchLabels['app.kubernetes.io/name']='foreign';
 await assert.rejects(main(args('apply-migration','--evidence',join(directory,'rejected.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-SOURCE-MIGRATION'),io),/ROTATION_JOB_PREREQUISITES_REJECTED/);assert.equal(createCount,0);liveEgress=egress;
 await assert.rejects(main(args('apply-migration','--evidence',join(directory,'first.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-SOURCE-MIGRATION'),io),/SOURCE_MIGRATION_RECEIPT_REQUIRED/);assert.equal(createCount,1);
 job=null;await assert.rejects(main(args('apply-migration','--evidence',join(directory,'second.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-SOURCE-MIGRATION'),io),/EXISTING_INTENT_REQUIRES_OBSERVE/);assert.equal(createCount,1);
 job=structuredClone(jobA);await main(args('observe-migration'),io);job=structuredClone(jobB);job.status={succeeded:1};await assert.rejects(main(args('apply-publisher','--evidence',join(directory,'publisher-b.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-PUBLISHER-SOURCE'),io),/SOURCE_MIGRATION_REPLACED/);assert.equal(replaceCount,0);
 job=structuredClone(jobA);job.status={succeeded:1};await main(args('apply-publisher','--evidence',join(directory,'publisher-a.jsonl'),'--confirm','APPLY-STAGING-AUTHORITY-PUBLISHER-SOURCE'),io);assert.equal(replaceCount,1);
 job=structuredClone(jobB);job.status={succeeded:1};await assert.rejects(main(args('observe-publisher'),io),/SOURCE_MIGRATION_REPLACED/);assert.equal(replaceCount,1);
 job=structuredClone(jobA);job.status={succeeded:1};await main(args('observe-publisher'),io);
 await main(args('rollback','--k3s-sudo','--evidence',join(directory,'rollback.jsonl'),'--confirm','ROLLBACK-STAGING-AUTHORITY-PUBLISHER-SOURCE'),io);assert.equal(replaceCount,2);
 assert.deepEqual(rolloutCalls,[
  {_command:'kubectl',args:['--context',context,'rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n','kodex-system']},
  {_command:'kubectl',args:['--context',context,'rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n','kodex-system']},
  {_command:'sudo',args:['-n','k3s','kubectl','--context',context,'rollout','status','deployment/internal-rpc-authority-publisher','--timeout=300s','-n','kodex-system']},
 ]);
});
