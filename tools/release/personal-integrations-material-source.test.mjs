import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,mkdirSync,writeFileSync,rmSync,chmodSync} from 'node:fs';
import {join,dirname} from 'node:path';
import {tmpdir} from 'node:os';
import {verifyPersonalIntegrationsMaterialSource} from './personal-integrations-material-source.mjs';

// Синтетические Git blobs содержат только имена public исходников. Ни material
// values, ни cluster IO, ни изменение actual registry в этой оснастке не нужны.
function fixture(t){
 const source=mkdtempSync(join(tmpdir(),'kodex-material-source-'));chmodSync(source,0o755);t.after(()=>rmSync(source,{recursive:true,force:true}));
 const git=(...args)=>execFileSync('git',['-C',source,'-c','core.hooksPath=/dev/null','-c','commit.gpgsign=false',
  '-c','user.name=Synthetic','-c','user.email=synthetic@example.invalid',...args],{encoding:'utf8',stdio:['ignore','pipe','pipe']}).trim();
 git('init','-q');git('remote','add','origin','https://github.com/codex-k8s/kodex.git');
 const paths=['tools/release/artifact-retention-issuer-material.mjs','tools/release/artifact-retention-issuer-login-precondition.sql',
  'tools/release/artifact-retention-issuer-login-readback.sql','services/internal/internal-rpc-authority/cmd/cli/migrations/20261115000200_artifact_retention_issuer.sql'];
 // Публичные файлы моделируют исходники read-only монтирования даже при umask077.
 const writeSource=(relative,text)=>{
  const file=join(source,relative);
  mkdirSync(dirname(file),{recursive:true});
  for(let directory=dirname(file);directory!==source;directory=dirname(directory))chmodSync(directory,0o755);
  writeFileSync(file,text);
  chmodSync(file,0o644);
 };
 for(const path of paths)writeSource(path,'synthetic public source\n');
 git('add','.');git('commit','-qm','synthetic material source');const materialSourceSHA=git('rev-parse','HEAD');
 const commit=()=>{git('add','.');git('commit','-qm','synthetic successor');return git('rev-parse','HEAD');};
 writeSource('unrelated','synthetic unrelated change\n');const sourceSHA=commit();
 return {source,sourceSHA,materialSourceSHA,paths,commit,git,writeSource};
}

test('earlier material source сохраняет собственный SHA при exact4 source equality',t=>{
 const f=fixture(t),proof=verifyPersonalIntegrationsMaterialSource(f);
 assert.equal(proof.materialSourceSHA,f.materialSourceSHA);assert.equal(proof.sourceSHA,f.sourceSHA);assert.equal(proof.files.length,4);
});

test('changed SQL/helper, dirty source и unrelated revision не допускают material reuse',t=>{
 const f=fixture(t);f.writeSource(f.paths[1],'changed\n');
 assert.throws(()=>verifyPersonalIntegrationsMaterialSource(f));f.sourceSHA=f.commit();
 assert.throws(()=>verifyPersonalIntegrationsMaterialSource(f),/SOURCE_CHANGED/);
 const other=fixture(t);other.git('checkout','--orphan','foreign');other.git('rm','-rf','.');
 other.writeSource('foreign','foreign\n');other.sourceSHA=other.commit();
 assert.throws(()=>verifyPersonalIntegrationsMaterialSource(other),/SOURCE_UNRELATED/);
});
