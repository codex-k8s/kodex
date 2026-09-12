import {constants,closeSync,fsyncSync,linkSync,lstatSync,openSync,readFileSync,realpathSync,unlinkSync,writeSync} from 'node:fs';
import {randomBytes} from 'node:crypto';
import {dirname,resolve} from 'node:path';
import {fingerprint} from './scoped-release.mjs';
import {buildSourceMigrationIntent,buildSourceMigrationReceipt,validateSourceMigrationIntent,validateSourceMigrationReceipt,verifySourceMigrationReadback} from './authority-rotation-source-delivery-model.mjs';

const requireValue=(value,code)=>{if(!value)throw new Error(code);};

function readPrivate(path,{optional=false,missingCode,validate}) {
 const absolute=resolve(path),parentPath=dirname(absolute),owner=process.getuid?.(),parent=lstatSync(parentPath);
 requireValue(parent.isDirectory()&&!parent.isSymbolicLink()&&realpathSync(parentPath)===parentPath&&(parent.mode&0o077)===0&&(owner===undefined||parent.uid===owner),'PRIVATE_RECEIPT_DIRECTORY_REQUIRED');
 let stat;try{stat=lstatSync(absolute);}catch(error){if(error.code==='ENOENT'){if(optional)return null;throw new Error(missingCode);}throw error;}
 requireValue(stat.isFile()&&!stat.isSymbolicLink()&&stat.nlink===1&&(stat.mode&0o077)===0&&(owner===undefined||stat.uid===owner)&&stat.size>0&&stat.size<=4096,'SOURCE_MIGRATION_RECEIPT_FILE_REJECTED');
 return validate(JSON.parse(readFileSync(absolute,'utf8')));
}

// Временный файл, fsync и атомарная жёсткая ссылка закрепляют одного победителя
// первого наблюдения. Остальные принимают только ту же сохранённую запись.
function publishPrivate(path,value,{validate,read,conflictCode}) {
 validate(value);const absolute=resolve(path),parent=dirname(absolute),temporary=`${absolute}.${randomBytes(8).toString('hex')}.tmp`;let fd;
 const owner=process.getuid?.(),parentStat=lstatSync(parent);
 requireValue(parentStat.isDirectory()&&!parentStat.isSymbolicLink()&&realpathSync(parent)===parent&&(parentStat.mode&0o077)===0&&(owner===undefined||parentStat.uid===owner),'PRIVATE_RECEIPT_DIRECTORY_REQUIRED');
 try {
  fd=openSync(temporary,constants.O_WRONLY|constants.O_CREAT|constants.O_EXCL|constants.O_NOFOLLOW,0o600);writeSync(fd,JSON.stringify(value,null,2)+'\n');fsyncSync(fd);closeSync(fd);fd=undefined;
  try{linkSync(temporary,absolute);}catch(error){if(error.code!=='EEXIST')throw error;}
  unlinkSync(temporary);const directory=openSync(parent,constants.O_RDONLY|constants.O_DIRECTORY|constants.O_NOFOLLOW);try{fsyncSync(directory);}finally{closeSync(directory);}
  const stored=read();requireValue(fingerprint(stored)===fingerprint(value),conflictCode);return stored;
 }finally{if(fd!==undefined)closeSync(fd);try{unlinkSync(temporary);}catch(error){if(error.code!=='ENOENT')throw error;}}
}

const intentPath=path=>`${resolve(path)}.intent`;

export function readSourceMigrationIntent(path,expected,plan,{optional=false}={}) {
 return readPrivate(intentPath(path),{optional,missingCode:'SOURCE_MIGRATION_INTENT_REQUIRED',validate:value=>validateSourceMigrationIntent(value,expected,plan)});
}

export function publishSourceMigrationIntent(path,expected,plan) {
 const value=buildSourceMigrationIntent(expected,plan);
 return publishPrivate(intentPath(path),value,{validate:item=>validateSourceMigrationIntent(item,expected,plan),read:()=>readSourceMigrationIntent(path,expected,plan),conflictCode:'SOURCE_MIGRATION_INTENT_CONFLICT'});
}

export function readSourceMigrationReceipt(path,expected,plan,{optional=false}={}) {
 return readPrivate(path,{optional,missingCode:'SOURCE_MIGRATION_RECEIPT_REQUIRED',validate:value=>validateSourceMigrationReceipt(value,expected,plan)});
}

export function publishSourceMigrationReceipt(path,receipt,expected,plan) {
 readSourceMigrationIntent(path,expected,plan);
 return publishPrivate(path,receipt,{validate:value=>validateSourceMigrationReceipt(value,expected,plan),read:()=>readSourceMigrationReceipt(path,expected,plan),conflictCode:'SOURCE_MIGRATION_RECEIPT_CONFLICT'});
}

export function resolveSourceMigrationReceipt(path,actual,expected,plan,{allowFirstObservation=false}={}){
 let receipt=readSourceMigrationReceipt(path,expected,plan,{optional:allowFirstObservation});
 if(!receipt&&allowFirstObservation)receipt=publishSourceMigrationReceipt(path,buildSourceMigrationReceipt(actual,expected,plan),expected,plan);
 requireValue(receipt,'SOURCE_MIGRATION_RECEIPT_REQUIRED');verifySourceMigrationReadback(actual,expected,receipt);return receipt;
}
