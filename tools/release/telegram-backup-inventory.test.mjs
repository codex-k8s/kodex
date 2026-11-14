import test from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync,writeFileSync,rmSync,chmodSync} from 'node:fs';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {backupMaintenanceFixture} from './fixtures/backup-controller-maintenance-synthetic.mjs';
import {canonicalPostgresBackupSpec} from './backup-postgres-boundary.mjs';
import {readBackupProducerInventory,requireBackupProducerBoundary} from './backup-controller-maintenance.mjs';

test('native fixture wire перечисляет migration Job/Pod и закрывает foreign backup producer',t=>{
 const directory=mkdtempSync('/tmp/kodex-telegram-backup-inventory-');chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));
 const f=backupMaintenanceFixture(),pg=canonicalPostgresBackupSpec(),uid=n=>`10000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
 const name='control-plane-migrate-synthetic',job={apiVersion:'batch/v1',kind:'Job',metadata:{name,namespace:'kodex-system',uid:uid(7001),labels:{}},
  spec:{template:{spec:{serviceAccountName:'control-plane-migrator',containers:[{name:'migrate',image:'synthetic.invalid/migration@sha256:'+'a'.repeat(64)}],volumes:[{name:'postgres',secret:{secretName:'control-plane-postgres-migration'}}]}}},status:{succeeded:1}};
 const pod={apiVersion:'v1',kind:'Pod',metadata:{name:name+'-pod',namespace:'kodex-system',uid:uid(7002),labels:{},ownerReferences:[{kind:'Job',uid:job.metadata.uid,controller:true}]},spec:structuredClone(job.spec.template.spec),status:{phase:'Succeeded'}};
 const state={kubeconfig:'/synthetic/explicit-kubeconfig',context:f.target.context,target:f.target,resources:{},componentNames:[],jobs:{[name]:job},jobPods:{[name]:pod},calls:[],effects:[],
  backup:{deployment:f.deployment,rs:f.rs,pods:f.state.pods,postgres:f.postgres,installation:f.observed.installation,extra:[],effects:0,
   pgStatefulSet:{apiVersion:'apps/v1',kind:'StatefulSet',metadata:{name:'kodex-postgresql',namespace:'kodex-system',uid:f.postgres.ownerUID,labels:{'app.kubernetes.io/name':'kodex-postgresql'}},spec:{template:{spec:pg}}},
   pgPod:{apiVersion:'v1',kind:'Pod',metadata:{name:f.postgres.pod,namespace:'kodex-system',uid:f.postgres.uid,labels:{'app.kubernetes.io/name':'kodex-postgresql'},ownerReferences:[{kind:'StatefulSet',uid:f.postgres.ownerUID,controller:true}]},spec:{...structuredClone(pg),volumes:[...pg.volumes,{name:'data',persistentVolumeClaim:{claimName:'data-kodex-postgresql-0'}}]}}}};
 const statePath=directory+'/state.json',store=()=>writeFileSync(statePath,JSON.stringify(state),{mode:0o600});store();
 const backend=fileURLToPath(new URL('./fixtures/telegram-deployment-kube.cjs',import.meta.url));
 const kube=args=>execFileSync(process.execPath,[backend,'--kubeconfig',state.kubeconfig,'--context',state.context,'--request-timeout=25s',...args],
  {encoding:'utf8',timeout:20000,maxBuffer:1<<20,stdio:['ignore','pipe','pipe'],env:{PATH:process.env.PATH,HOME:directory,LANG:'C.UTF-8',KODEX_TELEGRAM_SYNTHETIC_STATE:statePath}});
 const inventory=readBackupProducerInventory(kube);
 assert.ok(inventory.items.some(r=>r.kind==='Job'&&r.metadata.uid===job.metadata.uid));
 assert.ok(inventory.items.some(r=>r.kind==='Pod'&&r.metadata.uid===pod.metadata.uid));
 assert.doesNotThrow(()=>requireBackupProducerBoundary(inventory,f.deployment.metadata.uid,false,f.postgres));
 job.spec.template.spec.volumes.push({name:'foreign-backup',secret:{secretName:'backup-controller-credentials'}});store();
 assert.throws(()=>requireBackupProducerBoundary(readBackupProducerInventory(kube),f.deployment.metadata.uid,false,f.postgres),/BACKUP_MAINTENANCE_OTHER_PRODUCER_PRESENT/);
});
