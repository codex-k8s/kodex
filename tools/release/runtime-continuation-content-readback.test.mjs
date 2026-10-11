import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {readRuntimeContinuationContent,validateContinuationReadbackInput,validateContinuationReadback} from './runtime-continuation-content-readback.mjs';

const input={organizationRef:'org_fixture001',runRef:'run_fixture001',revisionRef:'rrev_fixture001',revisionVersion:1,revisionDigest:'a'.repeat(64),sessionRef:'ses_fixture001',turnRef:'trn_fixture001',attempt:1};
function result(){const {revisionDigest,...pins}=input;return {version:1,kind:'RUNTIME_CONTINUATION_CONTENT_READBACK',status:'OBSERVED',...pins,role:'USER',bytes:50000,codepoints:40000,trimEmpty:false,hasNUL:false,validUTF8:true,noticeCount:1,noticeContentMatches:true,noticeDigestMatches:true};}
const sql=readFileSync(new URL('./runtime-continuation-content-readback.sql',import.meta.url),'utf8');
const body=sql.replace(/--[^\n]*/g,'');
test('one exact pinned read-only transaction and bounded single JSON row',()=>{
 assert.match(body,/BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;/);
 assert.match(body,/statement_timeout = '10s'/);assert.match(body,/lock_timeout = '1s'/);
 assert.match(body,/octet_length\(body::text\)<=8192/);assert.match(body,/COMMIT;\s*$/);
 assert.equal(body.split(';').filter(value=>value.trim()).length,5);
 assert.doesNotMatch(body,/\b(?:UPDATE|INSERT|DELETE|MERGE|COPY|CALL|DO|ALTER|CREATE|DROP|TRUNCATE|GRANT|REVOKE|FOR\s+UPDATE|FOR\s+SHARE)\b/i);
 assert.doesNotMatch(body,/SELECT\s+(?:[a-z_.]*\*)|set_config|pg_sleep|dblink/i);
 for(const key of ['organization_ref','run_ref','revision_ref','revision_version','revision_digest','session_ref','turn_ref','attempt'])assert.ok(body.includes(":'"+key+"'"),key);
 for(const owner of ['organization_id','session_id','turn_id','node_id','attempt'])assert.ok(body.includes('notice.'+owner+'=target.'+owner),owner);
 assert.match(body,/notice.current_runtime_revision_id=target.id/);
 assert.match(body,/revision.generation=:'revision_version'::bigint/);
 assert.match(body,/revision.revision_digest=:'revision_digest'/);
});
test('projection is closed metadata, not history/snapshot/digest/source',()=>{
 const document=body.slice(body.indexOf('document AS'));
 const keys=[...new Set([...document.matchAll(/'([a-z][A-Za-z0-9]*)'\s*,/g)].map(match=>match[1]))].sort();
 assert.deepEqual(keys,['version','kind','status','organizationRef','runRef','revisionRef','revisionVersion','sessionRef','turnRef','attempt','role','bytes','codepoints','trimEmpty','hasNUL','validUTF8','noticeCount','noticeContentMatches','noticeDigestMatches'].sort());
 assert.doesNotMatch(document,/safe_snapshot|\brevision_digest\b|\bnotice_digest\b|notice\.content|secret|credential|password/i);
 assert.match(document,/octet_length\(final_content\)/);assert.match(document,/char_length\(final_content\)/);
 assert.match(document,/getdatabaseencoding\(\)='UTF8'/);
 assert.ok(document.includes("\\0085\\00A0\\1680"),'Go Unicode whitespace list');
 assert.ok(document.includes("\\2028\\2029\\202F\\205F\\3000"),'Go Unicode whitespace tail');
 assert.match(body,/count\(\*\)=1 AND bool_and\(notice.content=target.final_message->>'content'\)/);
 assert.match(body,/notice.materialization_digest=target.notice_digest/);
});
test('strict input rejects injection and authority/secret expansion',()=>{
 assert.doesNotThrow(()=>validateContinuationReadbackInput(input));
 for(const change of [{runRef:"run_bad' OR true--"},{revisionVersion:0},{attempt:2147483648},{revisionDigest:'private'},{ownerRole:'owner'},{sql:'SELECT secret'},{organizationRef:null}])assert.throws(()=>validateContinuationReadbackInput({...input,...change}),/^Error: READBACK_/);
});
test('strict output preserves safe facts and rejects extra content, foreign pins and multiple rows',()=>{
 const evidence=result();assert.deepEqual(validateContinuationReadback(input,evidence),evidence);
 for(const change of [{content:'synthetic-private'},{safe_snapshot:{}},{revisionRef:'rrev_foreign001'},{role:'private-name'},{bytes:-1},{codepoints:50001},{noticeCount:2},{noticeCount:0},{validUTF8:'true'}])assert.throws(()=>validateContinuationReadback(input,{...evidence,...change}),/^Error: READBACK_/);
 const missing={...evidence,status:'NOT_FOUND'};for(const key of ['role','bytes','codepoints','trimEmpty','hasNUL','validUTF8','noticeCount','noticeContentMatches','noticeDigestMatches'])missing[key]=null;
 assert.deepEqual(validateContinuationReadback(input,missing),missing);
});
test('execution is psql exact frozen file, protected inherited transport, no raw errors',()=>{
 const source={PATH:'/usr/bin:/bin',PGHOST:'/run/postgresql',PGDATABASE:'synthetic',PGUSER:'synthetic',PGPASSWORD:'synthetic-private',PGOPTIONS:'-c role=owner',OPENAI_API_KEY:'synthetic-private'};
 let called=false;
 const evidence=readRuntimeContinuationContent(input,source,(command,args,options)=>{
  called=true;assert.equal(command,'psql');assert.ok(args.includes('--no-password'));assert.ok(args.includes('ON_ERROR_STOP=1'));
  assert.equal(options.timeout,15000);assert.equal(options.maxBuffer,8192);assert.deepEqual(options.stdio,['ignore','pipe','pipe']);
  assert.equal(options.env.PGPASSWORD,'synthetic-private');assert.equal(options.env.PGOPTIONS,undefined);assert.equal(options.env.OPENAI_API_KEY,undefined);
  assert.match(args.at(-1),/runtime-continuation-content-readback.sql$/);return JSON.stringify(result())+'\n';
 });
 assert.equal(called,true);assert.equal(evidence.codepoints,40000);
 for(const execute of [()=>{throw new Error('synthetic-private')},()=>JSON.stringify(result())+'\n'+JSON.stringify(result()),()=>JSON.stringify({...result(),content:'synthetic-private'})]){
  assert.throws(()=>readRuntimeContinuationContent(input,source,execute),error=>/^READBACK_[A-Z_]+$/.test(error.message)&&!error.message.includes('private'));
 }
});
