#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {lstatSync,mkdtempSync,readFileSync,realpathSync,rmSync,writeFileSync} from 'node:fs';
import {dirname,join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {inspectSource} from './application-source.mjs';

const imagePattern=/^registry\.local\.kodex\/kodex\/image-admission@sha256:[a-f0-9]{64}$/;
const requireValue=(value,code)=>{if(!value)throw new Error(code);};
const safeCode=error=>/^[A-Z0-9_]+$/.test(error?.message??'')?error.message:'CAPABILITY_BUILD_FAILED';
const fields=['version','profile','revision','go','image','executableSHA256','recipe'];

export function validateHoldCapability(value) {
 requireValue(value&&typeof value==='object'&&!Array.isArray(value)&&Object.keys(value).sort().join('\0')===[...fields].sort().join('\0')&&
  value.version===1&&value.profile==='image-admission-hold-delivery'&&/^[a-f0-9]{40}$/.test(value.revision??'')&&
  value.go==='go1.26.6'&&imagePattern.test(value.image??'')&&/^[a-f0-9]{64}$/.test(value.executableSHA256??'')&&
  value.recipe==='CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/image-admission-controller',
 'INVALID_HOLD_DELIVERY_CAPABILITY');
 return value;
}

function main(args) {
 const options={};while(args.length){const key=args.shift();requireValue(['--source','--revision','--image','--output'].includes(key)&&!Object.hasOwn(options,key)&&args.length,'INVALID_ARGUMENT');options[key]=args.shift();}
 const source=realpathSync(options['--source']),revision=options['--revision'],output=resolve(options['--output']);
 requireValue(source===options['--source']&&inspectSource(source).revision===revision&&imagePattern.test(options['--image']??'')&&
  output.startsWith('/')&&!output.startsWith(source+'/'),'EXACT_CAPABILITY_INPUT_REQUIRED');
 try{lstatSync(output);throw new Error('CAPABILITY_OUTPUT_EXISTS');}catch(error){if(error.code!=='ENOENT')throw error;}
 const env={...process.env,GOENV:'off',GOFLAGS:'',GOTOOLCHAIN:'local',CGO_ENABLED:'0',GOWORK:'off',GOOS:'linux',GOARCH:'amd64',GOAMD64:'v1',GOMAXPROCS:'4'};
 const version=execFileSync('go',['env','GOVERSION'],{env,encoding:'utf8',stdio:'pipe',timeout:10_000}).trim();
 requireValue(version==='go1.26.6','EXACT_GO_VERSION_REQUIRED');
 const temporary=mkdtempSync(join(dirname(output),'image-admission-capability-')),binary=join(temporary,'image-admission-controller');
 try {
  execFileSync('go',['-C',join(source,'services/jobs/role-image-builder'),'build','-p','2','-trimpath','-buildvcs=false','-ldflags=-s -w','-o',binary,'./cmd/image-admission-controller'],
   {env,stdio:'pipe',timeout:300_000,maxBuffer:16<<20});
  requireValue(inspectSource(source).revision===revision,'SOURCE_CHANGED_DURING_BUILD');
  const result=validateHoldCapability({version:1,profile:'image-admission-hold-delivery',revision,go:version,image:options['--image'],
   executableSHA256:createHash('sha256').update(readFileSync(binary)).digest('hex'),
   recipe:'CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w" ./cmd/image-admission-controller'});
  writeFileSync(output,JSON.stringify(result,null,2)+'\n',{flag:'wx',mode:0o600});
  process.stdout.write(`Image admission hold capability: ${createHash('sha256').update(JSON.stringify(result)).digest('hex')}\n`);
 } finally {rmSync(temporary,{recursive:true,force:true});}
}

if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))try{main(process.argv.slice(2));}catch(error){
 process.stderr.write(`Image admission hold capability failed: ${safeCode(error)}\n`);process.exitCode=1;
}
