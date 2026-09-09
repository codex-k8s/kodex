import assert from 'node:assert/strict';
import test from 'node:test';
import {chmodSync,mkdtempSync,readFileSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {spawn} from 'node:child_process';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {buildSourceMigrationReceipt,verifySourceMigrationReadback} from './authority-rotation-source-delivery-model.mjs';
import {publishSourceMigrationIntent,publishSourceMigrationReceipt,readSourceMigrationIntent,readSourceMigrationReceipt,resolveSourceMigrationReceipt} from './authority-source-migration-receipt.mjs';

const plan={version:1,kind:'AUTHORITY_SOURCE_DELIVERY',intentID:'14390000-0000-4000-8000-000000000001'};
const expected={metadata:{name:`authority-source-${plan.intentID}`,namespace:'kodex-system',annotations:{'kodex.dev/source-delivery-intent':plan.intentID}},spec:{backoffLimit:0,template:{metadata:{labels:{}},spec:{restartPolicy:'Never'}}}};
const actual=uid=>({metadata:{...expected.metadata,uid},spec:{...structuredClone(expected.spec),selector:{matchLabels:{'batch.kubernetes.io/controller-uid':uid}}}});

test('atomic first observation is idempotent only for the same UID',t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json');
 publishSourceMigrationIntent(path,expected,plan);
 const a=buildSourceMigrationReceipt(actual('14390000-0000-4000-8000-00000000000a'),expected,plan),b=buildSourceMigrationReceipt(actual('14390000-0000-4000-8000-00000000000b'),expected,plan);
 assert.deepEqual(publishSourceMigrationReceipt(path,a,expected,plan),a);assert.deepEqual(publishSourceMigrationReceipt(path,a,expected,plan),a);
 assert.throws(()=>publishSourceMigrationReceipt(path,b,expected,plan),/RECEIPT_CONFLICT/);assert.deepEqual(readSourceMigrationReceipt(path,expected,plan),a);
});

test('concurrent first observations elect exactly one UID',async t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json');
 publishSourceMigrationIntent(path,expected,plan);
 const candidates=['14390000-0000-4000-8000-00000000000a','14390000-0000-4000-8000-00000000000b'].map(uid=>buildSourceMigrationReceipt(actual(uid),expected,plan));
 const moduleURL=pathToFileURL(fileURLToPath(new URL('./authority-source-migration-receipt.mjs',import.meta.url))).href;
 const script=`import {publishSourceMigrationReceipt} from ${JSON.stringify(moduleURL)};const [path,receipt,expected,plan]=process.argv.slice(1);publishSourceMigrationReceipt(path,JSON.parse(receipt),JSON.parse(expected),JSON.parse(plan));`;
 const statuses=await Promise.all(candidates.map(candidate=>new Promise(resolve=>{const child=spawn(process.execPath,['--input-type=module','-e',script,path,JSON.stringify(candidate),JSON.stringify(expected),JSON.stringify(plan)],{stdio:'ignore'});child.on('exit',code=>resolve(code));})));
 assert.deepEqual(statuses.sort(),[0,1]);assert.ok(candidates.some(candidate=>candidate.jobUID===readSourceMigrationReceipt(path,expected,plan).jobUID));
});

test('missing, foreign, corrupt, public and tampered receipts fail closed',t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json'),a=buildSourceMigrationReceipt(actual('14390000-0000-4000-8000-00000000000a'),expected,plan);
 assert.equal(readSourceMigrationReceipt(path,expected,plan,{optional:true}),null);assert.throws(()=>readSourceMigrationReceipt(path,expected,plan),/RECEIPT_REQUIRED/);
 assert.throws(()=>publishSourceMigrationReceipt(path,a,expected,plan),/SOURCE_MIGRATION_INTENT_REQUIRED/);
 writeFileSync(path,JSON.stringify({...a,jobUID:'14390000-0000-4000-8000-00000000000b'}),{mode:0o600});const foreign=readSourceMigrationReceipt(path,expected,plan);assert.throws(()=>verifySourceMigrationReadback(actual(a.jobUID),expected,foreign),/SOURCE_MIGRATION_REPLACED/);
 writeFileSync(path,'{broken',{mode:0o600});assert.throws(()=>readSourceMigrationReceipt(path,expected,plan));
 writeFileSync(path,JSON.stringify(a),{mode:0o600});chmodSync(path,0o644);assert.throws(()=>readSourceMigrationReceipt(path,expected,plan),/FILE_REJECTED/);
 assert.equal(readFileSync(path,'utf8'),JSON.stringify(a));
});

test('UNKNOWN recovery is explicit readback and every later phase keeps the same UID',t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json'),a=actual('14390000-0000-4000-8000-00000000000a'),b=actual('14390000-0000-4000-8000-00000000000b');
 publishSourceMigrationIntent(path,expected,plan);assert.equal(readSourceMigrationIntent(path,expected,plan).intentID,plan.intentID);
 assert.throws(()=>resolveSourceMigrationReceipt(path,a,expected,plan),/RECEIPT_REQUIRED/);
 const receipt=resolveSourceMigrationReceipt(path,a,expected,plan,{allowFirstObservation:true});assert.equal(receipt.jobUID,a.metadata.uid);
 assert.equal(resolveSourceMigrationReceipt(path,a,expected,plan).jobUID,a.metadata.uid);
 assert.throws(()=>resolveSourceMigrationReceipt(path,b,expected,plan),/SOURCE_MIGRATION_REPLACED/);a.status={succeeded:1};b.status={succeeded:1};assert.throws(()=>resolveSourceMigrationReceipt(path,b,expected,plan),/SOURCE_MIGRATION_REPLACED/);
});

test('durable create intent survives UNKNOWN and rejects another plan',t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json');
 const intent=publishSourceMigrationIntent(path,expected,plan);assert.deepEqual(publishSourceMigrationIntent(path,expected,plan),intent);
 const foreign={...plan,intentID:'14390000-0000-4000-8000-000000000099'},foreignExpected={...expected,metadata:{...expected.metadata,name:`authority-source-${foreign.intentID}`,annotations:{'kodex.dev/source-delivery-intent':foreign.intentID}}};
 assert.throws(()=>readSourceMigrationIntent(path,foreignExpected,foreign),/SOURCE_MIGRATION_INTENT_REJECTED/);
});

test('server-side dry-run validation cannot create a live receipt',t=>{
 const directory=mkdtempSync(join(tmpdir(),'authority-receipt-'));chmodSync(directory,0o700);t.after(()=>rmSync(directory,{recursive:true,force:true}));const path=join(directory,'receipt.json');
 const dryRun=actual('14390000-0000-4000-8000-00000000000a');delete dryRun.metadata.uid;delete dryRun.spec.selector;
 verifySourceMigrationReadback(dryRun,expected);assert.equal(readSourceMigrationReceipt(path,expected,plan,{optional:true}),null);
});
