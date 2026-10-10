#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import { healthReadbackEnvironment } from './managed-mcp-health-readback.mjs';

const ensure=(ok,code)=>{if(!ok)throw new Error(code);};
const inputKeys=['organizationRef','runRef','revisionRef','revisionVersion','revisionDigest','sessionRef','turnRef','attempt'];
const resultKeys=['version','kind','status','organizationRef','runRef','revisionRef','revisionVersion','sessionRef','turnRef','attempt','role','bytes','codepoints','trimEmpty','hasNUL','validUTF8','noticeCount','noticeContentMatches','noticeDigestMatches'];
const ref=/^[a-z][a-z0-9]{1,11}_[A-Za-z0-9_-]{8,84}$/;
const exact=(value,keys)=>value && !Array.isArray(value) && Object.keys(value).length===keys.length && keys.every(key=>Object.hasOwn(value,key));

export function validateContinuationReadbackInput(input){
 ensure(exact(input,inputKeys),'READBACK_ARGUMENT_INVALID');
 for(const key of ['organizationRef','runRef','revisionRef','sessionRef','turnRef'])ensure(typeof input[key]==='string'&&ref.test(input[key]),'READBACK_REF_INVALID');
 for(const key of ['revisionVersion','attempt'])ensure(Number.isSafeInteger(input[key])&&input[key]>0&&input[key]<=2147483647,'READBACK_VERSION_INVALID');
 ensure(typeof input.revisionDigest==='string'&&/^[a-f0-9]{64}$/.test(input.revisionDigest),'READBACK_DIGEST_INVALID');
}

export function validateContinuationReadback(input,evidence){
 validateContinuationReadbackInput(input);
 ensure(exact(evidence,resultKeys)&&evidence.version===1&&evidence.kind==='RUNTIME_CONTINUATION_CONTENT_READBACK'&&['OBSERVED','NOT_FOUND'].includes(evidence.status),'READBACK_RESULT_INVALID');
 for(const key of ['organizationRef','runRef','revisionRef','revisionVersion','sessionRef','turnRef','attempt'])ensure(evidence[key]===input[key],'READBACK_SCOPE_MISMATCH');
 const fields=['role','bytes','codepoints','trimEmpty','hasNUL','validUTF8','noticeCount','noticeContentMatches','noticeDigestMatches'];
 if(evidence.status==='NOT_FOUND'){
  ensure(fields.every(key=>evidence[key]===null),'READBACK_RESULT_INVALID');
  return evidence;
 }
 ensure(['USER','ASSISTANT','SYSTEM','UNSUPPORTED'].includes(evidence.role),'READBACK_RESULT_INVALID');
 for(const key of ['bytes','codepoints'])ensure(evidence[key]===null||Number.isSafeInteger(evidence[key])&&evidence[key]>=0&&evidence[key]<=2097152,'READBACK_RESULT_INVALID');
 ensure((evidence.bytes===null)===(evidence.codepoints===null),'READBACK_RESULT_INVALID');
 if(evidence.bytes!==null)ensure(evidence.codepoints<=evidence.bytes&&evidence.bytes<=evidence.codepoints*4,'READBACK_RESULT_INVALID');
 for(const key of ['trimEmpty','hasNUL','validUTF8'])ensure(evidence[key]===null||typeof evidence[key]==='boolean','READBACK_RESULT_INVALID');
 ensure(evidence.bytes===null?['trimEmpty','hasNUL','validUTF8'].every(key=>evidence[key]===null):['trimEmpty','hasNUL','validUTF8'].every(key=>typeof evidence[key]==='boolean'),'READBACK_RESULT_INVALID');
 ensure(Number.isSafeInteger(evidence.noticeCount)&&evidence.noticeCount>=0&&evidence.noticeCount<=1,'READBACK_NOTICE_CARDINALITY_INVALID');
 for(const key of ['noticeContentMatches','noticeDigestMatches'])ensure(typeof evidence[key]==='boolean','READBACK_RESULT_INVALID');
 ensure(evidence.noticeCount===1||!evidence.noticeContentMatches&&!evidence.noticeDigestMatches,'READBACK_NOTICE_CARDINALITY_INVALID');
 return evidence;
}

// Только исходный унаследованный owner transport; не читает envfiles/Secrets,
// не меняет роль/RLS/GUC и не создаёт child execution/retry.
export function readRuntimeContinuationContent(input,source=process.env,execute=execFileSync){
 validateContinuationReadbackInput(input);
 const env=healthReadbackEnvironment(source);
 const names=['organization_ref','run_ref','revision_ref','revision_version','revision_digest','session_ref','turn_ref','attempt'];
 const args=['-X','-qAt','--no-password','-v','ON_ERROR_STOP=1',...inputKeys.flatMap((key,index)=>['-v',names[index]+'='+input[key]]),'-f',fileURLToPath(new URL('./runtime-continuation-content-readback.sql',import.meta.url))];
 let raw;
 try{raw=execute('psql',args,{encoding:'utf8',env,timeout:15000,maxBuffer:8192,stdio:['ignore','pipe','pipe']});}catch{throw new Error('READBACK_QUERY_FAILED');}
 let evidence;try{evidence=JSON.parse(raw);}catch{throw new Error('READBACK_RESULT_INVALID');}
 return validateContinuationReadback(input,evidence);
}
function main(){
 const mapping={'--organization-ref':'organizationRef','--run-ref':'runRef','--revision-ref':'revisionRef','--revision-version':'revisionVersion','--revision-digest':'revisionDigest','--session-ref':'sessionRef','--turn-ref':'turnRef','--attempt':'attempt'};
 const args=process.argv.slice(2),input={};
 while(args.length){const key=mapping[args.shift()];ensure(key&&args.length&&!Object.hasOwn(input,key),'READBACK_ARGUMENT_INVALID');const value=args.shift();if(['revisionVersion','attempt'].includes(key)){ensure(/^[1-9][0-9]*$/.test(value),'READBACK_VERSION_INVALID');input[key]=Number(value);}else input[key]=value;}
 process.stdout.write(JSON.stringify(readRuntimeContinuationContent(input))+'\n');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 try{main();}catch(error){process.stderr.write(JSON.stringify({status:'FAIL',code:/^READBACK_[A-Z_]+$/.test(error.message)?error.message:'READBACK_FAILED'})+'\n');process.exitCode=1;}
}

