import {execFileSync} from 'node:child_process';
import {appendFileSync,mkdtempSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';

// Оба checkout используют текущие phase bytes. Дочерний commit добавляет
// только разрешённую дельту документации, без привязки к локальной истории.
export function syntheticPersonalExecutor(t){
 const source=resolve(fileURLToPath(new URL('../../../',import.meta.url)));
 const git=(root,...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','pipe']}).trim();
 const revision=git(source,'rev-parse','HEAD');
 const directory=mkdtempSync(join(tmpdir(),'kodex-personal-frozen-app-')),executor=join(directory,'successor');
 let registered=false;
 t.after(()=>{
  try{if(registered)git(source,'worktree','remove','--force',executor);}
  finally{rmSync(directory,{recursive:true,force:true});}
 });
 git(source,'worktree','add','--detach',executor,revision);registered=true;
 const allowedPath='docs/operations/personal-email-2168.md';
 appendFileSync(join(executor,allowedPath),'\n<!-- Синтетическая дельта executor -->\n');
 git(executor,'add','--',allowedPath);
 git(executor,'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','-c','commit.gpgSign=false','commit','-m','Синтетический successor');
 return {source,revision,executor,executorRevision:git(executor,'rev-parse','HEAD')};
}
