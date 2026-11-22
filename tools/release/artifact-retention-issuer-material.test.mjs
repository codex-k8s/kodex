import assert from 'node:assert/strict';
import {createECDH} from 'node:crypto';
import {chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {test} from 'node:test';
import {activateArtifactRetentionIssuerLogin, applyArtifactRetentionIssuerMaterial, artifactRetentionIssuerMaterialStatus, inspectArtifactRetentionIssuerMaterial, planArtifactRetentionIssuerMaterial, readArtifactRetentionIssuerReady} from './artifact-retention-issuer-material.mjs';
import {fingerprint} from './scoped-release.mjs';

const role = 'ira_artifact_retention_issuer_g1';
const uid = suffix => '11111111-2222-4333-8555-' + String(suffix).padStart(12, '0');

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(),'kodex-artifact-issuer-unit-'));
  t.after(() => rmSync(root,{recursive:true,force:true}));
  const directory = join(root,'material');
  mkdirSync(directory,{mode:0o700});
  const generated = createECDH('prime256v1');
  generated.generateKeys();
  const point = generated.getPublicKey();
  const scalar = generated.getPrivateKey();
  const privateD = Buffer.concat([Buffer.alloc(32 - scalar.length), scalar]).toString('base64url');
  const core = {kty:'EC',crv:'P-256',use:'sig',alg:'ES256',kid:'artifact-retention-platform-worker-g1',
    x:point.subarray(1,33).toString('base64url'),y:point.subarray(33).toString('base64url')};
  const password = 'c'.repeat(64);
  const data = {
    'private.jwk':JSON.stringify({...core,key_ops:['sign'],d:privateD}),
    'public.jwk':JSON.stringify({...core,key_ops:['verify']}),
    'postgresql-password':password,'postgresql-username':role,
    'postgresql-dsn':'postgresql://'+role+':'+password+'@internal-rpc-authority-postgresql-rw.kodex-system.svc.cluster.local:5432/internal_rpc_authority?sslmode=verify-full&sslrootcert=/var/run/config/kodex/internal-rpc-authority/postgresql/ca.pem',
  };
  for(const [name,value] of Object.entries(data))writeFileSync(join(directory,name),value,{mode:0o600});
  return {root,directory,data};
}

test('План использует только хеши подготовленного материала и закрепляет прежнее доверие', t => {
  const f = fixture(t), material = inspectArtifactRetentionIssuerMaterial(f.directory);
  const target = {context:'local-fixture',clusterUID:uid(1),namespaceUID:uid(2)};
  const observed = [
    {metadata:{uid:target.clusterUID}},
    {metadata:{uid:target.namespaceUID,labels:{'app.kubernetes.io/part-of':'kodex'}}},
    {metadata:{uid:uid(3),namespace:'kodex-system'},data:{profile:'synthetic'}},
  ];
  let sent = [];
  const kube = args => {
    sent.push(args);
    assert(args.includes('get'));
    if(!args.includes('secret'))return JSON.stringify(observed.shift());
    if(!args.includes('control-plane-application-grants'))return '';
    return [uid(4),'20','kodex','<no value>','', 'no','automation-scheduler.platform-worker.public.jwk','email-bridge.platform-worker.public.jwk',''].join('\n');
  };
  const plan = planArtifactRetentionIssuerMaterial({target,sourceRevision:'b'.repeat(40),material,kube});
  assert.equal(plan.publicTrustAppend.uid,uid(4));
  assert.equal(plan.publicTrustAppend.resourceVersion,'20');
  assert.equal(plan.publicTrustAppend.key,'artifact-retention.platform-worker.public.jwk');
  assert.deepEqual(plan.secretCreates.map(row=>row.name),
    ['artifact-retention-platform-worker-grant-signer','internal-rpc-authority-artifact-retention-issuer-postgresql']);
  assert(sent.filter(query=>query.includes('secret')).every(query=>query.at(-1).startsWith('go-template=')&&!query.at(-1).includes('index .data')));
  for(const value of Object.values(f.data))assert.equal(JSON.stringify(plan).includes(value),false);
});

test('Материал закрыто отклоняет дрейф TLS, alias и несовпадение подписанта', t => {
  const f = fixture(t);
  const dsn = join(f.directory,'postgresql-dsn');
  writeFileSync(dsn,f.data['postgresql-dsn'].replace('verify-full','disable'));
  assert.throws(()=>inspectArtifactRetentionIssuerMaterial(f.directory),/DATABASE_MATERIAL_REJECTED/);
  writeFileSync(dsn,f.data['postgresql-dsn']);
  const alias = join(f.root,'alias');
  symlinkSync(f.directory,alias);
  assert.throws(()=>inspectArtifactRetentionIssuerMaterial(alias),/PATH_REJECTED/);
  chmodSync(dsn,0o644);
  assert.throws(()=>inspectArtifactRetentionIssuerMaterial(f.directory),/PRIVATE_INPUT_REQUIRED/);
  chmodSync(dsn,0o600);
  const privatePath=join(f.directory,'private.jwk');
  const privateKey=JSON.parse(readFileSync(privatePath,'utf8'));
  privateKey.d=Buffer.alloc(32,1).toString('base64url');
  writeFileSync(privatePath,JSON.stringify(privateKey));
  assert.throws(()=>inspectArtifactRetentionIssuerMaterial(f.directory),/WORKER_KEY_MISMATCH/);
});

test('План отклоняет другой stand UID и существующий издатель без принятия владения', t => {
  const material=inspectArtifactRetentionIssuerMaterial(fixture(t).directory);
  const target={context:'local-fixture',clusterUID:uid(1),namespaceUID:uid(2)};
  const base={target,sourceRevision:'a'.repeat(40),material};
  function fakeKube(actual, alreadyExists) {
    return args=>{
      if(args.includes('kube-system'))return JSON.stringify({metadata:{uid:actual}});
      if(args.includes('namespace'))return JSON.stringify({metadata:{uid:uid(2),labels:{'app.kubernetes.io/part-of':'kodex'}}});
      if(args.includes('configmap'))return JSON.stringify({metadata:{uid:uid(3),namespace:'kodex-system'},data:{profile:'synthetic'}});
      if(args.includes('artifact-retention-platform-worker-grant-signer')&&alreadyExists)return [uid(4),'2','kodex','true','', 'no','private.jwk',''].join('\n');
      return '';
    };
  }
  assert.throws(()=>planArtifactRetentionIssuerMaterial({...base,kube:fakeKube(uid(8),false)}),/TARGET_CHANGED/);
  assert.throws(()=>planArtifactRetentionIssuerMaterial({...base,kube:fakeKube(uid(1),true)}),/SECRET_ALREADY_EXISTS/);
});

test('Применение восстанавливает потерянное подтверждение и сохраняет прежние ключи доверия', t => {
  const f = fixture(t);
  const target={context:'local-fixture',clusterUID:uid(1),namespaceUID:uid(2)};
  const actualTrust = {
    uid:uid(4),resourceVersion:'20',owner:'kodex',immutable:false,annotations:{'fixture.example.invalid/retained':'yes'},
    data:{'email-bridge.platform-worker.public.jwk':'Zml4dHVyZQ=='},
  };
  const store=new Map([['control-plane-application-grants',actualTrust]]);
  const creates=new Map();
  let patches=0;
  let loseAcknowledgement=true;
  let issuerReady=false;
  let loginMutations=0;
  let verifierSHA256=null;
  const kube=(args,input)=>{
    if(args.includes('exec')){
      if(args.includes('\\password ' + role)){
        loginMutations++;
        assert(args.some(value=>value.includes('FOR UPDATE')));
        assert(args.some(value=>value.includes("SET LOCAL password_encryption='scram-sha-256';")));
        assert(args.some(value=>value.includes("pg_catalog.pg_control_system()) IS DISTINCT FROM '103'")));
        assert(args.some(value=>value.includes("pg_catalog.pg_database WHERE datname=current_database()) IS DISTINCT FROM '4321'")));
        assert.equal(input,f.data['postgresql-password']+'\n'+f.data['postgresql-password']+'\n');
        assert(!args.join(' ').includes(f.data['postgresql-password']));
        verifierSHA256='7'.repeat(64);
        return 'ISSUER_LOGIN_INITIALIZED\n';
      }
      if(input.includes('pg_catalog.pg_authid'))return JSON.stringify({configured:verifierSHA256!==null,scram:verifierSHA256!==null,verifierSHA256});
      if(input.includes('systemIdentifier'))return JSON.stringify({systemIdentifier:'103',databaseOID:'4321',
        database:'internal_rpc_authority',port:'5432',serverAddress:null,serverPort:null});
      return JSON.stringify({schema:issuerReady?20261115000200:20261115000100,validRole:issuerReady});
    }
    if(args.includes('pod'))return JSON.stringify({metadata:{uid:uid(20),
      ownerReferences:[{kind:'StatefulSet',name:'kodex-postgresql',uid:uid(21),controller:true}]},status:{phase:'Running'}});
    if(args.includes('create')){
      const object=JSON.parse(input);
      assert.equal(object.kind,'Secret');
      assert.equal(object.immutable,true);
      const name=object.metadata.name;
      assert(!store.has(name));
      creates.set(name,(creates.get(name)??0)+1);
      store.set(name,{uid:uid(4+creates.size),resourceVersion:'21',owner:object.metadata.labels['app.kubernetes.io/part-of'],
        immutable:object.immutable,annotations:object.metadata.annotations,data:object.data});
      if(loseAcknowledgement){loseAcknowledgement=false;throw Error('synthetic lost ACK');}
      return 'secret/'+name;
    }
    if(args.includes('patch')){
      patches++;
      const patch=JSON.parse(readFileSync(args[args.indexOf('--patch-file')+1],'utf8'));
      assert.deepEqual(patch.slice(0,2),[
        {op:'test',path:'/metadata/uid',value:uid(4)},
        {op:'test',path:'/metadata/resourceVersion',value:'20'},
      ]);
      assert.equal(patch[2].path,'/data/artifact-retention.platform-worker.public.jwk');
      actualTrust.data['artifact-retention.platform-worker.public.jwk']=patch[2].value;
      actualTrust.annotations['kodex.dev/artifact-retention-issuer-material-plan']=patch[3].value;
      actualTrust.resourceVersion='22';
      return 'secret/control-plane-application-grants';
    }
    if(args.includes('kube-system'))return JSON.stringify({metadata:{uid:uid(1)}});
    if(args.includes('namespace'))return JSON.stringify({metadata:{uid:uid(2),labels:{'app.kubernetes.io/part-of':'kodex'}}});
    if(args.includes('configmap'))return JSON.stringify({metadata:{uid:uid(3),namespace:'kodex-system'},data:{profile:'synthetic'}});
    const name=args[args.indexOf('secret')+1], value=store.get(name);
    if(!value)return '';
    const keyed=args.at(-1).match(/{{index \.data "([^"]+)"}}/);
    if(keyed)return value.uid+'\n'+value.data[keyed[1]];
    return [value.uid,value.resourceVersion,value.owner,String(value.immutable),
      value.annotations['kodex.dev/artifact-retention-issuer-material-plan']??'', 'yes',...Object.keys(value.data), ''].join('\n');
  };
  const plan=planArtifactRetentionIssuerMaterial({target,sourceRevision:'d'.repeat(40),material:inspectArtifactRetentionIssuerMaterial(f.directory),kube});
  const journal={events:[],append(event){this.events.push(event);}};
  const request={plan,materialDirectory:f.directory,kube,journal};
  assert.throws(()=>applyArtifactRetentionIssuerMaterial(request),/ISSUER_ROLE_NOT_READY/);
  assert.equal(creates.size,0);
  assert.equal(journal.events.length,0);
  issuerReady=true;
  assert.throws(()=>applyArtifactRetentionIssuerMaterial(request),/RECONCILIATION_REQUIRED/);
  assert.equal(creates.size,1);
  assert.equal(applyArtifactRetentionIssuerMaterial(request).status,'MATERIAL_STAGED');
  assert.equal(applyArtifactRetentionIssuerMaterial(request).status,'MATERIAL_STAGED');
  assert.deepEqual([...creates.values()],[1,1]);
  assert.equal(patches,1);
  assert.equal(actualTrust.data['email-bridge.platform-worker.public.jwk'],'Zml4dHVyZQ==');
  assert.equal(actualTrust.annotations['fixture.example.invalid/retained'],'yes');
  assert.equal(artifactRetentionIssuerMaterialStatus(request).status,'MATERIAL_STAGED');
  assert.equal(activateArtifactRetentionIssuerLogin(request).status,'LOGIN_INITIALIZED');
  assert.equal(activateArtifactRetentionIssuerLogin(request).status,'LOGIN_INITIALIZED');
  assert.equal(artifactRetentionIssuerMaterialStatus(request).status,'LOGIN_INITIALIZED');
  const planPath=join(f.root,'plan.json'), evidencePath=join(f.root,'journal.jsonl');
  const planSHA256=fingerprint(plan);
  writeFileSync(planPath,JSON.stringify(plan),{mode:0o600});
  writeFileSync(evidencePath,[{type:'HEADER',planSHA256,schema:plan.schema},...journal.events]
    .map(event=>JSON.stringify(event)).join('\n')+'\n',{mode:0o600});
  const ready={planPath,evidencePath,sourceSHA:plan.sourceRevision,target:plan.target};
  const proof=readArtifactRetentionIssuerReady(ready,kube);
  assert.equal(proof.planSHA256,planSHA256);
  assert.equal(proof.sourceSHA,plan.sourceRevision);
  assert.deepEqual(proof.target,plan.target);
  assert.throws(()=>readArtifactRetentionIssuerReady({...ready,target:{...plan.target,namespaceUID:uid(99)}},kube),/TARGET_CHANGED/);
  const unknown={...request,journal:{...journal,events:journal.events.slice(0,-1)}};
  assert.equal(artifactRetentionIssuerMaterialStatus(unknown).status,'LOGIN_RECONCILIATION_REQUIRED');
  assert.throws(()=>activateArtifactRetentionIssuerLogin(unknown),/LOGIN_RECONCILIATION_REQUIRED/);
  verifierSHA256='8'.repeat(64);
  assert.throws(()=>activateArtifactRetentionIssuerLogin(request),/LOGIN_READBACK_REJECTED/);
  assert.throws(()=>artifactRetentionIssuerMaterialStatus(request),/LOGIN_READBACK_REJECTED/);
  assert.equal(loginMutations,1);
  assert(!JSON.stringify(journal.events).includes(f.data['postgresql-password']));
});
