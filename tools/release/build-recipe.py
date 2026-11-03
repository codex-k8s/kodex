#!/usr/bin/env python3
"""Явная замена только OpenCode download stage при неизменном application source."""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess

RECIPE='services/jobs/agent-runner/Dockerfile'


def require(value):
    if not value:raise ValueError('Build recipe contract rejected')


def git(root,*args):
    value=subprocess.run(['git','-C',str(root),*args],capture_output=True,timeout=20)
    require(value.returncode==0 and len(value.stdout)<=2<<20)
    return value.stdout


def split_stage(raw):
    matches=list(re.finditer(rb'^FROM [^\r\n]+ AS opencode-binary\n',raw,re.M))
    require(len(matches)==1)
    match=matches[0];following=re.search(rb'^FROM ',raw[match.end():],re.M)
    require(following is not None)
    end=match.end()+following.start()
    return raw[:match.start()],raw[match.start():end],raw[end:]


def materialize(repository,tooling_root,source_sha,recipe_sha,context):
    require(all(re.fullmatch('[a-f0-9]{40}',value)for value in [source_sha,recipe_sha]))
    require(git(repository,'rev-parse','HEAD').decode().strip()==source_sha)
    require(git(tooling_root,'rev-parse','HEAD').decode().strip()==recipe_sha)
    for root in [repository,tooling_root]:require(not git(root,'status','--porcelain','--untracked-files=no').strip())
    changed=set(git(repository,'diff','--name-only',source_sha,recipe_sha,'--','services','libs','contracts').decode().splitlines())
    require(changed=={RECIPE})
    previous=git(repository,'show',source_sha+':'+RECIPE);replacement=git(repository,'show',recipe_sha+':'+RECIPE)
    before,_,after=split_stage(previous);new_before,stage,new_after=split_stage(replacement)
    require(before==new_before and after==new_after)
    # Digest артефакта и native version probe остаются зафиксированы в рецепте.
    for token in re.findall(rb"[a-f0-9]{64}  /tmp/opencode.tar.gz|test \"\$\(/out/opencode --version\)\" = '[^']+'",split_stage(previous)[1]):require(token in stage)
    target=context/RECIPE
    require(context.is_dir()and not context.is_symlink()and target.is_file()and not target.is_symlink()and target.stat().st_nlink==1 and target.resolve().is_relative_to(context.resolve()) and target.read_bytes()==previous)
    target.write_bytes(replacement)
    return {'schema':'kodex.build-recipe/v1','source_sha':recipe_sha,'application_source_sha':source_sha,
            'files':[{'path':RECIPE,'sha256':hashlib.sha256(replacement).hexdigest()}]}


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ['repository','tooling-root','context']:parser.add_argument('--'+name,type=Path,required=True)
    parser.add_argument('--source-sha',required=True);parser.add_argument('--recipe-sha',required=True)
    args=parser.parse_args()
    print(json.dumps(materialize(args.repository,args.tooling_root,args.source_sha,args.recipe_sha,args.context),sort_keys=True))

if __name__=='__main__':
    try:main()
    except (OSError,ValueError,subprocess.SubprocessError):raise SystemExit('Build recipe contract rejected')from None
