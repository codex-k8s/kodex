#!/usr/bin/env node
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {dirname,join,resolve} from 'node:path';
import {mkdtempSync,readFileSync,readdirSync,realpathSync,rmSync,writeFileSync} from 'node:fs';
import {inspectSource} from './application-source.mjs';

const sha=value=>createHash('sha256').update(value).digest('hex');
try {
 const args=process.argv.slice(2),options={};while(args.length){const key=args.shift();if(!['--source','--revision','--output'].includes(key)||options[key]||!args.length)throw new Error('INVALID_ARGUMENT');options[key]=args.shift();}
 const source=realpathSync(options['--source']),output=resolve(options['--output']);
 if(inspectSource(source).revision!==options['--revision']||output.startsWith(source+'/'))throw new Error('EXACT_CLEAN_SOURCE_REQUIRED');
 const env={...process.env,GOENV:'off',GOFLAGS:'',GOTOOLCHAIN:'local',CGO_ENABLED:'0',GOWORK:'off',GOOS:'linux',GOARCH:'amd64',GOAMD64:'v1',GOMAXPROCS:'4'};
 const go=execFileSync('go',['env','GOVERSION'],{env,encoding:'utf8',stdio:'pipe',timeout:10_000}).trim();if(go!=='go1.26.6')throw new Error('EXACT_GO_VERSION_REQUIRED');
 const temporary=mkdtempSync(join(dirname(output),'authority-source-build-'));
 try {
  const binary=join(temporary,'publisher');
  execFileSync('go',['-C',join(source,'services/internal/internal-rpc-authority'),'build','-p','2','-trimpath','-buildvcs=false','-o',binary,'./cmd/internal-rpc-authority-publisher'],{env,stdio:'pipe',timeout:300_000});
  const migrationDirectory=join(source,'services/internal/internal-rpc-authority/cmd/cli/migrations');
  const migrations=readdirSync(migrationDirectory).filter(name=>/^[0-9]{14}_[a-z0-9_]+\.sql$/.test(name)).sort().map(name=>({name,sha256:sha(readFileSync(join(migrationDirectory,name)))}));
  if(migrations.length===0)throw new Error('AUTHORITY_MIGRATIONS_REQUIRED');
  const result={version:1,protocol:1,revision:options['--revision'],go,publisherSHA256:sha(readFileSync(binary)),migrations,
   sourceJobWrapperSHA256:sha(readFileSync(join(source,'tools/dev/run-go-command.sh'))),hotReloadWrapperSHA256:sha(readFileSync(join(source,'tools/dev/run-go-hot-reload.sh'))),
   recipe:'CGO_ENABLED=0 GOWORK=off go build -trimpath -buildvcs=false ./cmd/internal-rpc-authority-publisher'};
  if(inspectSource(source).revision!==options['--revision'])throw new Error('SOURCE_CHANGED');
  writeFileSync(output,JSON.stringify(result,null,2)+'\n',{flag:'wx',mode:0o600});process.stdout.write(`Authority source capability: ${sha(JSON.stringify(result))}\n`);
 }finally{rmSync(temporary,{recursive:true,force:true});}
}catch(error){process.stderr.write(`Authority source capability failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'BUILD_FAILED'}\n`);process.exitCode=1;}
