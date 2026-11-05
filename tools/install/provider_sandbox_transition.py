#!/usr/bin/env python3
"""Guarded maintenance-переход runtime profile без replay bootstrap и изменения image policy."""
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import select
import stat
import subprocess
import tempfile
import threading
import time

import yaml
import bootstrap_completion
from deployment_profile import require, Rejected
from portable_state import State, key, seal, verify_resource_set, verify_resource, verify_schemas
import provider_sandbox_profile as profile

# SHARE блокирует новые claims/запуски до завершения controller rollout.
# Advisory lock сериализует maintenance в разных локальных state directories.
FENCE_SQL = """BEGIN;
SET LOCAL lock_timeout='1s';
SET LOCAL statement_timeout='5s';
SET LOCAL idle_in_transaction_session_timeout='30s';
SELECT pg_try_advisory_xact_lock(7410616832235727);
LOCK TABLE control_plane.runs, control_plane.run_nodes, control_plane.runtime_leases IN SHARE MODE NOWAIT;
SELECT json_build_object('pid',pg_backend_pid(),
 'leases',(SELECT count(*) FROM control_plane.runtime_leases WHERE state NOT IN ('COMPLETED','CANCELLED','EXPIRED')),
 'runs',(SELECT count(*) FROM control_plane.runs WHERE state IN ('QUEUED','RUNNING','CANCELLING')),
 'nodes',(SELECT count(*) FROM control_plane.run_nodes WHERE state='RUNNING'));
"""


class Fence:
    """Ограниченная SQL-сессия выполняет только фиксированные lock/read/rollback statements."""
    HEARTBEAT_SECONDS = 2
    MAX_LIFETIME_SECONDS = 240

    def __init__(self, command):
        self.command = command
        self.process = None
        self.buffer = b''
        self.pid = None
        self.io_lock = threading.RLock()
        self.stop = threading.Event()
        self.worker = None
        self.lost = False
        self.deadline = None

    def line(self):
        deadline = time.monotonic()+10
        while b'\n' not in self.buffer:
            require(self.process.poll() is None, 'PROVIDER_PROFILE_FENCE_LOST')
            remaining = deadline-time.monotonic()
            require(remaining > 0 and select.select([self.process.stdout], [], [], remaining)[0], 'PROVIDER_PROFILE_FENCE_TIMEOUT')
            chunk = os.read(self.process.stdout.fileno(), 4096)
            require(chunk and len(self.buffer)+len(chunk) <= 8192, 'PROVIDER_PROFILE_FENCE_RESPONSE_REJECTED')
            self.buffer += chunk
        line, self.buffer = self.buffer.split(b'\n', 1)
        return line

    def send(self, sql):
        require(self.process.poll() is None, 'PROVIDER_PROFILE_FENCE_LOST')
        try:
            self.process.stdin.write(sql.encode()); self.process.stdin.flush()
        except (BrokenPipeError, OSError):
            raise Rejected('PROVIDER_PROFILE_FENCE_LOST') from None

    def __enter__(self):
        self.error = tempfile.TemporaryFile()
        self.process = subprocess.Popen(self.command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.error)
        try:
            self.send(FENCE_SQL)
            require(self.line() == b't', 'PROVIDER_PROFILE_MAINTENANCE_BUSY')
            state = json.loads(self.line())
            require(set(state) == {'pid','leases','runs','nodes'} and type(state['pid']) is int and state['pid'] > 0 and
                    all(type(state[k]) is int and state[k] == 0 for k in ('leases','runs','nodes')), 'ACTIVE_RUNTIME_REJECTED')
            self.pid = state['pid']
            self.deadline = time.monotonic()+self.MAX_LIFETIME_SECONDS
            self.worker = threading.Thread(target=self.keepalive, name='provider-profile-fence', daemon=True)
            self.worker.start()
            return self
        except BaseException:
            self.__exit__(None, None, None)
            raise

    def ping(self):
        with self.io_lock:
            require(not self.lost and time.monotonic() < self.deadline, 'PROVIDER_PROFILE_FENCE_LOST')
            self.send("SELECT pg_backend_pid();\n")
            require(self.line() == str(self.pid).encode(), 'PROVIDER_PROFILE_FENCE_LOST')

    def before_write(self):
        # HTTP CAS имеет предел20s; новую запись не начинаем у границы общего budget.
        require(self.deadline-time.monotonic() >= 25, 'PROVIDER_PROFILE_FENCE_BUDGET_EXHAUSTED')
        self.ping()

    def keepalive(self):
        while not self.stop.wait(self.HEARTBEAT_SECONDS):
            try:self.ping()
            except Exception:
                self.lost = True
                self.stop.set()
                return

    def __exit__(self, *_):
        self.stop.set()
        if self.worker is not None:self.worker.join(timeout=11)
        if self.process is not None:
            try:
                if self.process.poll() is None:
                    self.send('ROLLBACK;\n\\q\n')
                    self.process.stdin.close()
                    self.process.wait(timeout=10)
            except (OSError, Rejected, subprocess.TimeoutExpired):
                if self.process.poll() is None:
                    with contextlib.suppress(ProcessLookupError):self.process.terminate()
                try:self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    with contextlib.suppress(ProcessLookupError):self.process.kill()
                    self.process.wait(timeout=5)
            if self.process.stdout:self.process.stdout.close()
            if self.process.stdin and not self.process.stdin.closed:self.process.stdin.close()
        self.error.close()


@contextlib.contextmanager
def guarded_writes(state, fence):
    """Каждый новый API write сверяет живую maintenance-сессию непосредственно перед отправкой."""
    original=state.call
    def call(arguments, value=None):
        if '--dry-run=server' not in arguments and any(v in ('create','patch','apply','replace','delete') for v in arguments):
            fence.before_write()
        return original(arguments, value)
    state.call=call
    try:yield
    finally:state.call=original


def private_json(path, value):
    if path.exists() or path.is_symlink():
        info=path.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, 'PRIVATE_STATE_FILE_REQUIRED')
    fd, temporary = tempfile.mkstemp(prefix='.provider-profile-', dir=path.parent)
    try:
        with os.fdopen(fd,'w') as stream:
            json.dump(value,stream,sort_keys=True);stream.write('\n');stream.flush();os.fsync(stream.fileno())
        os.replace(temporary,path)
        descriptor=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(descriptor)
        finally:os.close(descriptor)
    finally:
        if os.path.exists(temporary):os.unlink(temporary)


@contextlib.contextmanager
def state_lock(directory):
    require(not directory.is_symlink(), 'PRIVATE_STATE_DIRECTORY_REQUIRED')
    directory.mkdir(mode=0o700,parents=False,exist_ok=True)
    info=directory.stat()
    require(info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, 'PRIVATE_STATE_DIRECTORY_REQUIRED')
    descriptor=os.open(directory/'lock',os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW,0o600)
    try:
        info=os.fstat(descriptor)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,'PRIVATE_STATE_LOCK_REQUIRED')
        fcntl.flock(descriptor,fcntl.LOCK_EX|fcntl.LOCK_NB)
        yield
    finally:os.close(descriptor)


def read_render(path, sha):
    require(path.is_file() and not path.is_symlink() and re.fullmatch('[a-f0-9]{64}',sha), 'PINNED_RENDER_REQUIRED')
    raw=path.read_bytes()
    require(len(raw) <= 16 << 20 and hashlib.sha256(raw).hexdigest() == sha,'RENDER_DIGEST_MISMATCH')
    values=list(filter(None,yaml.safe_load_all(raw)))
    require(len(values) <= 10000 and len({key(r) for r in values}) == len(values),'UNIQUE_BOUNDED_RENDER_REQUIRED')
    return values


def namespace_uids(state):
    result={}
    for name in ('kube-system','kodex-system','kodex-runtime'):
        resource=json.loads(state.call(['get','namespace',name,'-o','json']))
        require(resource['metadata']['name'] == name and resource['metadata'].get('uid'),'NAMESPACE_IDENTITY_REQUIRED')
        result[name]=resource['metadata']['uid']
    return result


def runtime_guard(state, previous):
    """Maintenance допускает только installation warm Pod; чужие/ordinary workloads не останавливаются."""
    pods=json.loads(state.call(['-n','kodex-runtime','get','pods','-o','json']))['items']
    image=profile.source_policy(previous)['data']['nodeReadbackImage']
    require(len(pods) <= 1,'ACTIVE_RUNTIME_REJECTED')
    for pod in pods:
        metadata=pod['metadata'];containers=pod['spec'].get('containers',[])
        require(metadata['name'] == 'system-assistant-warm' and metadata.get('labels',{}).get('app.kubernetes.io/part-of') == 'kodex' and
                {c['name'] for c in containers} == {'role-runtime','provider-runtime','provider-credential-relay'} and
                all(c['image'] == image for c in containers),'INSTALLATION_WARM_ONLY')


def desired_observed(state, resources):
    for resource in resources:
        current=state.get(resource)
        if current is None:return False
        try:verify_resource(resource,current)
        except Rejected:return False
    return True


def controller_ready(state, deployment):
    current=state.get(deployment);verify_resource(deployment,current)
    status=current.get('status',{});desired=deployment['spec'].get('replicas',1)
    if not (current['metadata']['generation'] == status.get('observedGeneration') and
            all(status.get(name,0) == desired for name in ('updatedReplicas','readyReplicas','replicas','availableReplicas'))):return False
    pods=json.loads(state.call(['-n','kodex-system','get','pods','-l','app.kubernetes.io/name=runtime-controller','-o','json']))['items']
    return bool(pods) and all(p.get('status',{}).get('phase') in ('Failed','Succeeded') or
        (not p['metadata'].get('deletionTimestamp') and p['metadata'].get('annotations',{}).get(profile.DIGEST) ==
         deployment['spec']['template']['metadata']['annotations'][profile.DIGEST]) for p in pods)


def execute(state, previous, desired, directory, mode, identity, completion=None, maintenance=False, resume=False, fence_factory=None):
    touched=profile.transition(previous,desired)
    state.discover(previous+desired)
    namespaces=namespace_uids(state)
    source_current=state.get(profile.source_policy(previous))
    require(source_current is not None,'LIVE_IMAGE_POLICY_REQUIRED')
    identity={**identity,'namespace_uids':namespaces,'source_image_policy_uid':source_current['metadata']['uid']}
    operation=directory/'operation.json'
    prior=json.loads(bootstrap_completion.private_bytes(operation)) if operation.exists() else None
    if prior:require(prior['identity'] == identity,'PROVIDER_PROFILE_OPERATION_IDENTITY_DRIFT')
    if prior:
        for resource in touched:
            saved=prior.get('inventory',{}).get(key(resource));current=state.get(resource)
            if saved is not None:require(current is not None and current['metadata']['uid'] == saved['uid'],'PROVIDER_PROFILE_RESOURCE_UID_DRIFT')
    changed={key(touched[1]),key(touched[2])}
    unchanged=[r for r in previous if key(r) not in changed]
    verify_resource_set(state,unchanged,completion)
    all_desired=desired_observed(state,touched) and controller_ready(state,touched[-1])
    if all_desired and not (prior and mode == 'apply' and resume and prior.get('phase') != 'PROVIDER_PROFILE_APPLIED'):
        result={'phase':'PROVIDER_PROFILE_ALREADY_OBSERVED','identity':identity,'effects_repeated':False}
        if prior:
            result['phase']='PROVIDER_PROFILE_APPLIED' if prior.get('phase') == 'PROVIDER_PROFILE_APPLIED' else 'PROVIDER_PROFILE_DESIRED_OBSERVED_RECONCILIATION_REQUIRED'
        if prior and mode == 'readback':private_json(directory/'readback.json',{'identity':identity,'inventory':state.inventory(touched)})
        return result
    if mode == 'readback':
        if prior:private_json(directory/'readback.json',{'identity':identity,'inventory':state.inventory(touched)})
        return {'phase':'PROVIDER_PROFILE_NOT_READY_RECONCILIATION_REQUIRED','identity':identity,'effects_repeated':False}
    require(mode in ('plan','apply'),'PROVIDER_PROFILE_MODE_REJECTED')
    inventory=state.inventory(touched)
    if prior:
        require(resume and prior['phase'] != 'PROVIDER_PROFILE_APPLIED','EXPLICIT_PROVIDER_PROFILE_RESUME_REQUIRED')
        readback=directory/'readback.json'
        require(readback.exists(),'PROVIDER_PROFILE_READBACK_REQUIRED_BEFORE_RESUME')
        observed=json.loads(bootstrap_completion.private_bytes(readback))
        require(observed['identity'] == identity and observed['inventory'] == inventory,'PROVIDER_PROFILE_READBACK_DRIFT')
        recovered=prior.get('inventory',{})
        for resource in touched:
            current=state.get(resource);expected=recovered.get(key(resource))
            if current is not None:
                try:verify_resource(resource,current)
                except Rejected:require(expected is not None and current['metadata']['uid'] == expected['uid'] and seal(current) == expected['seal'],'PROVIDER_PROFILE_PREIMAGE_DRIFT')
            else:require(expected is None,'PROVIDER_PROFILE_RESOURCE_DISAPPEARED')
    else:
        require(state.get(touched[0]) is None,'NEW_RESOURCE_COLLISION')
        for resource in touched[1:]:
            before=next(r for r in previous if key(r) == key(resource))
            verify_resource(before,state.get(resource))
    runtime_guard(state,previous)
    for resource in touched:state.preflight_apply(resource)
    if mode == 'plan':
        result={'phase':'PROVIDER_PROFILE_PLAN','identity':identity,'server_dry_run':'PASS','resources':[key(r) for r in touched],'maintenance_required':True}
        private_json(directory/'plan.json',{'identity':identity,'inventory':inventory})
        return result
    if prior is None:
        require((directory/'plan.json').exists(),'PROVIDER_PROFILE_PLAN_REQUIRED')
        plan=json.loads(bootstrap_completion.private_bytes(directory/'plan.json'))
        require(plan == {'identity':identity,'inventory':inventory},'PROVIDER_PROFILE_PLAN_DRIFT')
    require(maintenance,'EXPLICIT_MAINTENANCE_REQUIRED')
    record=prior or {'phase':'PLANNED','identity':identity,'inventory':inventory,'effects':[]}
    if prior is None:
        private_json(directory/'preimage.json',{'identity':identity,'resources':[state.get(r) for r in touched]})
        private_json(operation,record)
    fence_factory=fence_factory or (lambda:Fence(['kubectl','--context',state.context,'--request-timeout=15s','-n','kodex-system','exec','-i','kodex-postgresql-0','--',
        'psql','-XqAt','-U','postgres','-d','control_plane','-v','ON_ERROR_STOP=1']))
    record.setdefault('attempts',[]).append({'number':len(record.get('attempts',[]))+1,'phase':'STARTED','fence':'NOT_ACQUIRED'})
    attempt=record['attempts'][-1]
    try:
        with fence_factory() as fence, guarded_writes(state, fence):
            record['fence_pid']=fence.pid;attempt['fence']='HELD';attempt['pid']=fence.pid;private_json(operation,record)
            runtime_guard(state,previous)
            for resource in touched:
                fence.ping()
                current=state.get(resource)
                if current is not None:
                    try:verify_resource(resource,current)
                    except Rejected:pass
                    else:continue
                record['phase']='PROVIDER_PROFILE_WRITE_INTENT';record['writing']=key(resource);private_json(operation,record)
                state.apply(resource,inventory)
                record['effects'].append({'resource':key(resource),'uid':inventory[key(resource)]['uid']})
                record['inventory']=inventory;record['phase']='PROVIDER_PROFILE_WRITE_OBSERVED';record.pop('writing',None);private_json(operation,record)
            deadline=time.monotonic()+120
            while not controller_ready(state,touched[-1]):
                fence.ping();require(time.monotonic() < deadline,'PROVIDER_PROFILE_ROLLOUT_TIMEOUT');time.sleep(1)
            fence.ping()
            verify_resource(profile.source_policy(previous),state.get(profile.source_policy(previous)))
            verify_schemas(state,previous)
            fence.ping()
            record['phase']='PROVIDER_PROFILE_APPLIED';record['fence_continuity']='CURRENT_ATTEMPT_VERIFIED';attempt['phase']='APPLIED';attempt['fence']='VERIFIED';private_json(operation,record)
        return {'phase':record['phase'],'identity':identity,'effects':record['effects'],'image_policy_changed':False,'bootstrap_replayed':False}
    except BaseException:
        record['phase']='PROVIDER_PROFILE_RECONCILIATION_REQUIRED';attempt['phase']='RECONCILIATION_REQUIRED';attempt['fence']='OUTCOME_UNVERIFIED';private_json(operation,record)
        raise


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('mode',choices=('plan','apply','readback'))
    parser.add_argument('--context',required=True)
    parser.add_argument('--previous-render',type=Path,required=True)
    parser.add_argument('--previous-render-sha256',required=True)
    parser.add_argument('--render',type=Path,required=True)
    parser.add_argument('--render-sha256',required=True)
    parser.add_argument('--state-directory',type=Path,required=True)
    parser.add_argument('--maintenance-confirmed',action='store_true')
    parser.add_argument('--resume',action='store_true')
    bootstrap_completion.add_arguments(parser)
    args=parser.parse_args()
    require(re.fullmatch('[A-Za-z0-9._:-]{1,128}',args.context),'EXACT_CONTEXT_REQUIRED')
    previous=read_render(args.previous_render,args.previous_render_sha256);desired=read_render(args.render,args.render_sha256)
    completion=bootstrap_completion.from_arguments(args)
    identity={'context':args.context,'previous_render_sha256':args.previous_render_sha256,'render_sha256':args.render_sha256,
              'image_policy_sha256':profile.source_policy(previous)['data']['policySHA256']}
    with state_lock(args.state_directory):
        result=execute(State(args.context),previous,desired,args.state_directory,args.mode,identity,completion,args.maintenance_confirmed,args.resume)
        print(json.dumps(result,sort_keys=True))


if __name__ == '__main__':
    try:main()
    except (Rejected, OSError, ValueError, KeyError, TypeError, yaml.YAMLError, subprocess.SubprocessError):
        raise SystemExit('Provider sandbox transition requires reconciliation') from None
