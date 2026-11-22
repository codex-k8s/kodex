import assert from 'node:assert/strict';
import test from 'node:test';
import {existsSync,readFileSync,realpathSync} from 'node:fs';
import {basename,dirname,relative,resolve} from 'node:path';
import {fileURLToPath} from 'node:url';
import {componentMigrationConfiguration} from './portable-component-migration.mjs';

const root=fileURLToPath(new URL('../../',import.meta.url));
const profile=componentMigrationConfiguration('personal-integrations');

// Без Go/network/Docker: каждый local replacement, включая замыкание библиотек,
// должен существовать в том же immutable context и быть COPY до mod download.
for(const component of profile.components.filter(name=>name!=='staff-control-center'))test(`${component}: local Go modules входят в exact Docker build inputs`,()=>{
 const directory=resolve(root,profile.sources[component]),dockerfile=resolve(directory,'Dockerfile');
 const raw=readFileSync(dockerfile,'utf8'),download=raw.indexOf('RUN go mod download');
 assert(download>0,'canonical mod download boundary missing');
 const copies=prefix=>{
  const directories=new Set(),manifests=new Set();
  for(const [,line] of prefix.matchAll(/^COPY (.+)$/gm)){
   const tokens=line.trim().split(/\s+/),destination=tokens.pop();
   for(const source of tokens){
    assert(!source.startsWith('--'),'external/context COPY requires explicit review');
    if(source.endsWith('/')){
     assert.equal(source,destination,'directory COPY must preserve module layout');directories.add(source.slice(0,-1));
    }else{
     const target=destination.endsWith('/')?destination+basename(source):destination;
     assert.equal(target,source,'file COPY must preserve module layout');
     if(source.endsWith('/go.mod'))manifests.add(dirname(source));
    }
   }
  }
  return {directories,manifests};
 };
 const prefix=raw.slice(0,download),early=copies(prefix),build=copies(raw.slice(0,raw.indexOf('\nFROM ',raw.indexOf('\nFROM ')+1)));
 // В Go candidate context нет ignore-фильтра. Новый фильтр требует отдельной
 // проверки его реальной семантики, а не молчаливого обхода этого guard.
 assert.equal(existsSync(resolve(root,'.dockerignore')),false,'root ignore requires explicit build-input review');
 assert.equal(existsSync(dockerfile+'.dockerignore'),false,'component ignore requires explicit build-input review');
 assert.equal(/^COPY \.\s/m.test(prefix),false,'whole checkout copy is not a module-input proof');
 const pending=[resolve(directory,'go.mod')],seen=new Set();
 while(pending.length){
  const manifest=pending.pop();if(seen.has(manifest))continue;seen.add(manifest);
  assert.equal(realpathSync(manifest),manifest,'module input must remain within pinned checkout');
  const text=readFileSync(manifest,'utf8');
  assert.equal(/=>\s+"/.test(text),false,'quoted replacement needs explicit parser review');
  for(const [,replacement] of text.matchAll(/=>\s+(\.\.?\/[^\s)]+)/g)){
   const target=resolve(dirname(manifest),replacement),source=relative(root,target);
   assert(/^libs\/go\/[a-z0-9]+$/.test(source),'replacement escaped canonical library scope');
   assert(early.directories.has(source)||early.manifests.has(source),`${component}: missing COPY ${source}/go.mod before go mod download`);
   assert(build.directories.has(source),`${component}: missing full COPY ${source} before compilation`);
   pending.push(resolve(target,'go.mod'));
  }
 }
 assert(build.directories.has(profile.sources[component].replace(/\/$/,'')),'service source COPY missing');
 const runtime=raw.slice(raw.lastIndexOf('\nFROM '));
 assert.equal(/COPY (?:libs|services)\//.test(runtime),false,'runtime must receive compiled output only');
});
