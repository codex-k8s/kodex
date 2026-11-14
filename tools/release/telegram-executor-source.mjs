import {readTelegramPortRepair,telegramPortRepairManifestPaths} from './telegram-port-repair.mjs';
import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {inspectSource,sourceGitArguments} from './application-source.mjs';
import {fingerprint} from './scoped-release.mjs';

const fail=(ok)=>{if(!ok)throw Error('TELEGRAM_EXECUTOR_SOURCE_REJECTED');};
const protectedPaths=new Set(['tools/release/authority-telegram-transition.mjs','tools/release/telegram-notifications-catalog.gen.mjs']);

// Новый проверенный delivery executor может завершить прежний frozen application
// source. Старые image/DDL/source pins не переписываются; оба SHA записываются явно.
// Допуск закрыт для application/config/schema/policy изменений между источниками.
export function resolveTelegramExecutorSource(executorSource,{source,revision,saved,repairPlanPath}={}){
 if(saved){
  fail(source===undefined&&revision===undefined);
  fail(saved.schema==='kodex.telegram-executor-source/v1'&&
   Object.keys(saved).sort().join(',')===(saved.telegramPortRepair?'executorRevision,executorSource,revision,schema,source,telegramPortRepair':'executorRevision,executorSource,revision,schema,source'));
  fail(!repairPlanPath||repairPlanPath===saved.telegramPortRepair?.planPath);
  repairPlanPath=saved.telegramPortRepair?.planPath;source=saved.source;revision=saved.revision;
 }
 fail(typeof source==='string'&&source===resolve(source)&&/^[a-f0-9]{40}$/.test(revision??''));
 executorSource=resolve(executorSource);
 const executorRevision=inspectSource(executorSource).revision;
 fail(inspectSource(source).revision===revision&&executorRevision!==revision);
 const git=(...args)=>execFileSync('git',sourceGitArguments(executorSource,...args),
  {encoding:'utf8',timeout:15000,maxBuffer:4<<20,stdio:['ignore','pipe','pipe']});
 const repair=repairPlanPath?readTelegramPortRepair(repairPlanPath,{executorSource}):undefined;
 fail(!repair||repair.repair.application.path===source&&repair.repair.application.revision===revision);
 let changed;
 try{
  git('merge-base','--is-ancestor',revision,executorRevision);
  changed=git('diff','--no-ext-diff','--no-textconv','--no-renames','--name-only','-z',revision,executorRevision).split('\0').filter(Boolean);
 }catch{throw Error('TELEGRAM_EXECUTOR_SOURCE_REJECTED');}
 fail(changed.every(path=>(path.startsWith('tools/release/')||path.startsWith('docs/')||path==='Makefile'||repair&&telegramPortRepairManifestPaths.includes(path))&&
  !path.endsWith('.sql')&&!path.startsWith('tools/release/fixtures/authority-policy-')&&!protectedPaths.has(path)));
 const proof={schema:'kodex.telegram-executor-source/v1',source,revision,executorSource,executorRevision,...(repair?{telegramPortRepair:{planPath:repair.path,planSHA256:repair.sha256}}:{})};
 if(saved)fail(fingerprint(proof)===fingerprint(saved));
 return Object.freeze(proof);
}
