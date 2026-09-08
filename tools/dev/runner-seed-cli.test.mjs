import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { spawnSync } from 'node:child_process';

for (const sudo of [false, true]) test(`runner seed uses exact ${sudo ? 'sudo k3s' : 'ordinary kubectl'} prefix through port-forward`, () => {
  const root=mkdtempSync(join(tmpdir(),'kodex-seed-prefix-'));
  try {
    const bin=join(root,'bin'), state=join(root,'state'), log=join(root,'calls.jsonl');
    mkdirSync(bin); mkdirSync(state); mkdirSync(join(state,'cache'));
    writeFileSync(join(state,'image-supply-chain-tools-docker-tag'),`kodex-local/image-admission-tools:${'a'.repeat(64)}`);
    writeFileSync(join(state,'agent-runner-image'),`registry.invalid/runner@sha256:${'b'.repeat(64)}`);
    writeFileSync(join(state,'cache','agent-runner-fixture.oci.tar'),'fixture');
    const stub=`#!${process.execPath}
const fs=require('node:fs'),path=require('node:path');
let name=path.basename(process.argv[1]),args=process.argv.slice(2),prefix=[];
if(name==='sudo'){if(JSON.stringify(args.slice(0,3))!==JSON.stringify(['-n','k3s','kubectl']))process.exit(91);prefix=args.splice(0,3);name='kubectl';}
fs.appendFileSync(process.env.CALLS,JSON.stringify({name,args,prefix,uid:process.getuid()})+'\\n');
if(name==='kubectl'){
 if(args[0]!=='--context'||args[1]!=='fixture')process.exit(92);args=args.slice(2);
 if(args[0]==='config'){process.stdout.write('fixture');process.exit(0);}
 if(args.includes('port-forward'))process.exit(23);
 if(args.includes('secret/kodex-image-promotion-writer')){let data={};for(const key of ['registry-client.crt','registry-client.key','ca.pem','promotion.username','promotion.password'])data[key]=Buffer.from('SENTINEL_PRIVATE_VALUE').toString('base64');process.stdout.write(JSON.stringify({data}));process.exit(0);}
 if(args.includes('get'))process.stdout.write(JSON.stringify({metadata:{namespace:'kodex-system',labels:{'app.kubernetes.io/part-of':'kodex','kodex.dev/local-profile':'hot-reload'}}}));
}else if(name==='docker'){if(args[0]==='image')process.stdout.write('sha256:'+'a'.repeat(64));else process.exit(24);}
else if(name==='tar')process.stdout.write(JSON.stringify({manifests:[{digest:'sha256:'+'b'.repeat(64)}]}));
`;
    for(const name of ['sudo','kubectl','docker','tar','yq'])writeFileSync(join(bin,name),stub,{mode:0o755});
    const result=spawnSync('bash',['tools/dev/seed-local-image-supply-chain.sh','--context','fixture','--state-directory',state,'--component','runner','--readback-only','--evidence',join(root,'evidence.jsonl'),...(sudo?['--k3s-sudo']:[])],{env:{...process.env,PATH:`${bin}:${process.env.PATH}`,CALLS:log,TMPDIR:root},encoding:'utf8',timeout:10000});
    assert.notEqual(result.status,0,'stub port-forward must stop before a real registry operation');
    const raw=readFileSync(log,'utf8'), calls=raw.trim().split('\n').map(JSON.parse), kube=calls.filter(item=>item.name==='kubectl');
    assert.ok(kube.some(item=>item.args.includes('config')));
    assert.ok(kube.some(item=>item.args.includes('secret/kodex-image-promotion-writer')));
    assert.ok(kube.some(item=>item.args.includes('port-forward')));
    assert.ok(kube.every(item=>JSON.stringify(item.prefix)===JSON.stringify(sudo?['-n','k3s','kubectl']:[])));
    assert.ok(kube.every(item=>item.args[0]==='--context'&&item.args[1]==='fixture'));
    assert.ok(calls.filter(item=>item.name==='docker').every(item=>item.prefix.length===0&&item.uid===process.getuid()));
    assert.ok(!(raw+result.stdout+result.stderr).includes('SENTINEL_PRIVATE_VALUE'));
    assert.ok(!(raw+result.stdout+result.stderr).includes(Buffer.from('SENTINEL_PRIVATE_VALUE').toString('base64')));
  } finally { rmSync(root,{recursive:true,force:true}); }
});
