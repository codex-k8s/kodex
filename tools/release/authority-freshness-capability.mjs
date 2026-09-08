#!/usr/bin/env node
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { mkdtempSync, readFileSync, writeFileSync, rmSync, realpathSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { inspectSource } from './application-source.mjs';

// Артефакт не содержит credentials. Сборка использует тот же pure-Go recipe,
// что и run-go-hot-reload; manifest затем связывает actual /proc/PID/exe.
try {
  const args=process.argv.slice(2),options={};
  while(args.length) { const key=args.shift();if(!['--source','--revision','--output'].includes(key)||options[key]||!args.length)throw new Error('INVALID_ARGUMENT');options[key]=args.shift(); }
  const source=realpathSync(options['--source']), revision=options['--revision'], output=resolve(options['--output']);
  if(inspectSource(source).revision!==revision||output.startsWith(source+'/'))throw new Error('EXACT_CLEAN_SOURCE_REQUIRED');
  const env={...process.env,GOENV:'off',GOFLAGS:'',GOTOOLCHAIN:'local',CGO_ENABLED:'0',GOWORK:'off',GOOS:'linux',GOARCH:'amd64',GOAMD64:'v1',GOMAXPROCS:'4'};
  const version=execFileSync('go',['env','GOVERSION'],{env,encoding:'utf8',stdio:'pipe',timeout:10_000}).trim();
  if(version!=='go1.26.6')throw new Error('EXACT_GO_VERSION_REQUIRED');
  const temporary=mkdtempSync(join(dirname(output),'authority-freshness-build-'));
  try {
    const binaries={},imageBinaries={};
    for (const role of ['issuer','verifier']) {
      const binary=join(temporary,`internal-rpc-authority-${role}`);
      execFileSync('go',['-C',join(source,'services/internal/internal-rpc-authority'),'build','-p','2','-trimpath','-buildvcs=false','-o',binary,`./cmd/internal-rpc-authority-${role}`],{env,stdio:'pipe',timeout:300_000});
      binaries[role]=createHash('sha256').update(readFileSync(binary)).digest('hex');
      execFileSync('go',['-C',join(source,'services/internal/internal-rpc-authority'),'build','-p','2','-trimpath','-buildvcs=false',`-ldflags=-s -w -X main.version=${revision}`,'-o',binary,`./cmd/internal-rpc-authority-${role}`],{env,stdio:'pipe',timeout:300_000});
      imageBinaries[role]=createHash('sha256').update(readFileSync(binary)).digest('hex');
    }
    if(inspectSource(source).revision!==revision)throw new Error('SOURCE_CHANGED');
    const result={version:1,protocol:2,revision,go:version,binaries,imageBinaries,imageVersion:revision,imageRecipe:'CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=${revision}"',migrationSHA256:createHash('sha256').update(readFileSync(join(source,'services/internal/internal-rpc-authority/cmd/cli/migrations/20260908000100_authority_bounded_freshness.sql'))).digest('hex'),rendererSHA256:createHash('sha256').update(readFileSync(join(source,'tools/render-image-admission-job.sh'))).digest('hex'),recipe:'CGO_ENABLED=0 GOWORK=off GOOS=linux GOARCH=amd64 GOAMD64=v1 go build -trimpath -buildvcs=false ./cmd/internal-rpc-authority-{issuer,verifier}'};
    writeFileSync(output,JSON.stringify(result,null,2)+'\n',{flag:'wx',mode:0o600});
    process.stdout.write(`Authority capability: ${createHash('sha256').update(JSON.stringify(result)).digest('hex')}\n`);
  } finally {rmSync(temporary,{recursive:true,force:true});}
} catch(error) {process.stderr.write(`Authority capability failed: ${/^[A-Z0-9_]+$/.test(error.message)?error.message:'BUILD_FAILED'}\n`);process.exitCode=1;}
