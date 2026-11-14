import test from 'node:test';
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {verifyAuthorityObserverSource} from './component-api-traffic.mjs';

test('routine observer proves synthetic orphan source by exact full tree and actual canonical ancestry',()=>{
 const root=fileURLToPath(new URL('../../',import.meta.url));
 const git=(...args)=>execFileSync('git',['-C',root,...args],{encoding:'utf8',stdio:['ignore','pipe','pipe'],timeout:30000}).trim();
 const equivalent=git('rev-parse','HEAD^'),current=git('rev-parse','HEAD');
 const tree=revision=>git('rev-parse',`${revision}^{tree}`),ancestor=(old,next)=>git('merge-base','--is-ancestor',old,next);
 const original=git('-c','commit.gpgSign=false','commit-tree',tree(equivalent),'-m','Исходный synthetic squash source2140');
 assert.throws(()=>ancestor(original,current));assert.equal(tree(original),tree(equivalent));ancestor(equivalent,current);
 verifyAuthorityObserverSource(original,current,original,ancestor,{revision:equivalent,tree});
 assert.throws(()=>verifyAuthorityObserverSource(original,current,undefined,ancestor,{revision:equivalent,tree}),/SOURCE_CHANGED/);
 assert.throws(()=>verifyAuthorityObserverSource(original,current,original,ancestor));
 assert.throws(()=>verifyAuthorityObserverSource(original,current,original,ancestor,{revision:current,tree}),/EQUIVALENT_TREE_CHANGED/);
 const orphan=git('-c','commit.gpgSign=false','commit-tree',tree(original),'-m','Независимый synthetic source equivalence2140');
 assert.equal(tree(orphan),tree(original));
 assert.throws(()=>verifyAuthorityObserverSource(original,current,original,ancestor,{revision:orphan,tree}),/EQUIVALENT_UNRELATED/);
 assert.throws(()=>verifyAuthorityObserverSource(original,current,original,ancestor,{revision:equivalent,tree:()=>{throw Error('unreadable');}}),/EQUIVALENT_UNREADABLE/);
});
