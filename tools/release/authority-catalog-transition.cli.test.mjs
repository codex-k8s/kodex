import assert from 'node:assert/strict';
import test from 'node:test';
import {mkdtempSync,writeFileSync,readFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath} from 'node:url';
import {main as rotationMain} from './authority-rotation-transition.mjs';
import {catalogPolicyTransition} from './authority-catalog-transition.mjs';
import {fixture} from './fixtures/authority-catalog-synthetic.mjs';

function prerequisites(){
  const labels={'app.kubernetes.io/name':'internal-rpc-authority','app.kubernetes.io/component':'migrator'};
  const sa={kind:'ServiceAccount',metadata:{name:'internal-rpc-authority-migrator',labels},automountServiceAccountToken:false};
  const egress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-migrator'},spec:{podSelector:{matchLabels:labels},policyTypes:['Ingress','Egress'],ingress:[],egress:[
    {to:[{namespaceSelector:{matchLabels:{'kubernetes.io/metadata.name':'kube-system'}},podSelector:{matchLabels:{'k8s-app':'kube-dns'}}}],ports:[{port:53,protocol:'UDP'},{port:53,protocol:'TCP'}]},
    {to:[{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}}}],ports:[{port:5432,protocol:'TCP'}]},
  ]}};
  const ingress={kind:'NetworkPolicy',metadata:{name:'internal-rpc-authority-postgresql-from-migrator'},spec:{podSelector:{matchLabels:{'app.kubernetes.io/name':'kodex-postgresql'}},policyTypes:['Ingress'],ingress:[{from:[{podSelector:{matchLabels:labels}}],ports:[{port:5432,protocol:'TCP'}]}]}};
  return {sa,egress,ingress};
}

function cliFixture(t){
  const f=fixture(),directory=mkdtempSync(join(tmpdir(),'catalog-authority-cli-'));
  t.after(()=>rmSync(directory,{recursive:true,force:true}));
  const kubeconfig=join(directory,'kubeconfig'),profile=join(directory,'target.json'),output=join(directory,'plan.json');
  writeFileSync(kubeconfig,'synthetic-unused-private-fixture',{mode:0o600});
  writeFileSync(profile,JSON.stringify(f.profile),{mode:0o600});
  const calls=[],{sa,egress,ingress}=prerequisites();
  const expectedPrefix=['--kubeconfig',kubeconfig,'--context',f.profile.context,'--request-timeout=30s'];
  const run=(command,args,input)=>{
    assert.equal(command,'kubectl');assert.deepEqual(args.slice(0,expectedPrefix.length),expectedPrefix);
    const argv=args.slice(expectedPrefix.length);calls.push(argv);
    if(argv[0]==='config')return f.profile.context;
    if(argv[0]==='auth')return 'yes';
    if(argv[0]==='get'){
      const [,kind,name]=argv;
      if(kind==='serviceaccount')return JSON.stringify(sa);
      if(kind==='networkpolicy')return JSON.stringify(name==='internal-rpc-authority-migrator'?egress:ingress);
      if(kind==='secret'||name==='internal-rpc-authority-postgresql-ca')return kind+'/'+name;
      const value=f.get(kind,name);assert.ok(value,'unexpected synthetic GET');return JSON.stringify(value);
    }
    if(argv[0]==='create'&&argv.includes('--dry-run=server'))return input;
    throw new Error('UNEXPECTED_SYNTHETIC_EFFECT');
  };
  const source=fileURLToPath(new URL('../..',import.meta.url)).replace(/\/$/,''),revision='a'.repeat(40);
  const args=()=>['plan','--policy-transition',catalogPolicyTransition,'--context',f.profile.context,
    '--kubeconfig',kubeconfig,'--portable-target',profile,'--source',source,'--revision',revision,'--action','status','--output',output];
  return {...f,directory,kubeconfig,profilePath:profile,output,calls,args,io:{run,inspectSource:()=>({revision})}};
}

test('public portable status planner pins context/kubeconfig and uses only bounded reads plus server dry-run',async t=>{
  const f=cliFixture(t);
  await rotationMain(f.args(),f.io);
  const plan=JSON.parse(readFileSync(f.output,'utf8'));
  assert.equal(plan.kubeconfig,f.kubeconfig);
  assert.equal(plan.policyTransition.targetSHA256,f.target.transition.targetSHA256);
  assert.equal(plan.cliImage,f.publisher.spec.template.spec.containers[0].image);
  assert.equal(f.calls.filter(args=>args[0]==='create'&&args.includes('--dry-run=server')).length,1);
  assert.equal(f.calls.filter(args=>['patch','replace','delete'].includes(args[0])).length,0);
});

test('public portable planner rejects changed installation/CP before dry-run and never falls back to ambient kubeconfig',async t=>{
  for(const mutate of [f=>{f.installation.data['profile.json']='foreign';},f=>{f.deployment.metadata.uid='00000000-0000-4000-8000-000000000099';},
    f=>{rmSync(f.kubeconfig);}]) {
    const f=cliFixture(t);mutate(f);
    await assert.rejects(rotationMain(f.args(),f.io));
    assert.equal(f.calls.filter(args=>['create','replace','patch','delete'].includes(args[0])).length,0);
  }
});

test('catalog portable scope cannot silently enable legacy77 on production labels',async t=>{
  const f=cliFixture(t),args=f.args();
  args.splice(args.indexOf('--policy-transition'),2);
  await assert.rejects(rotationMain(args,f.io),/EXACT_STAGING_CONTEXT_REQUIRED/);
  assert.equal(f.calls.length,0);
});
