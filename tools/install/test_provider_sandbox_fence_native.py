"""Настоящий PostgreSQL18: maintenance lock не допускает нового claim и не переживает потерю backend."""
import json
import os
import subprocess
import time
import unittest
from unittest.mock import patch
from deployment_profile import Rejected
from provider_sandbox_transition import Fence

IMAGE='docker.io/library/postgres:18.3-alpine3.23@sha256:54451ecb8ab38c24c3ec123f2fd501303a3a1856a5c66e98cecf2460d5e1e9d7'


class Native(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.name='kodex-provider-profile-pg-'+str(os.getpid())
        created=subprocess.run(['docker','run','-d','--name',cls.name,'--label','kodex.test=provider-sandbox-fence','--network','none','--memory','256m',
            '--tmpfs','/var/lib/postgresql:rw,nosuid,nodev,size=192m','-e','POSTGRES_HOST_AUTH_METHOD=trust',IMAGE],capture_output=True,timeout=30)
        if created.returncode:raise RuntimeError('disposable native PostgreSQL startup failed')
        cls.addClassCleanup(lambda:subprocess.run(['docker','rm','-f',cls.name],capture_output=True,timeout=20))
        cls.command=['docker','exec','-i',cls.name,'psql','-XqAt','-U','postgres','-d','postgres','-v','ON_ERROR_STOP=1']
        deadline=time.monotonic()+30
        while time.monotonic()<deadline:
            # Временный initdb server слушает только Unix socket; ждём основной TCP listener.
            ready=subprocess.run(['docker','exec',cls.name,'pg_isready','-h','127.0.0.1','-U','postgres'],capture_output=True,timeout=5)
            if ready.returncode==0:break
            time.sleep(.2)
        else:raise RuntimeError('native PostgreSQL readiness timeout')
        cls.sql('CREATE SCHEMA control_plane; CREATE TABLE control_plane.runs(state text); CREATE TABLE control_plane.run_nodes(state text); CREATE TABLE control_plane.runtime_leases(state text);')

    @classmethod
    def sql(cls,value,check=True):
        result=subprocess.run(cls.command,input=value.encode(),capture_output=True,timeout=10)
        if check and result.returncode:raise RuntimeError('native PostgreSQL fixture statement failed')
        return result

    def setUp(self):
        self.sql('TRUNCATE control_plane.runs, control_plane.run_nodes, control_plane.runtime_leases;')

    def test_share_fence_blocks_insert_until_rollback_and_serializes_operators(self):
        with Fence(self.command)as fence:
            fence.ping()
            with self.assertRaises(Rejected):
                with Fence(self.command):self.fail('second maintenance writer entered')
            blocked=self.sql("SET lock_timeout='100ms'; INSERT INTO control_plane.runtime_leases VALUES ('CLAIMED');",check=False)
            self.assertNotEqual(blocked.returncode,0)
            self.assertEqual(self.sql('SELECT count(*) FROM control_plane.runtime_leases;').stdout.strip(),b'0')
            fence.ping()
        self.sql("INSERT INTO control_plane.runtime_leases VALUES ('CLAIMED');")
        self.assertEqual(self.sql('SELECT count(*) FROM control_plane.runtime_leases;').stdout.strip(),b'1')

    def test_active_and_unknown_lease_or_running_run_are_rejected(self):
        for table,state in [('runtime_leases','CLAIMED'),('runtime_leases','UNKNOWN'),('runs','RUNNING'),('run_nodes','RUNNING')]:
            with self.subTest(table=table,state=state):
                self.setUp();self.sql("INSERT INTO control_plane."+table+" VALUES ('"+state+"');")
                with self.assertRaises(Rejected):
                    with Fence(self.command):self.fail('active fixture admitted')
        self.setUp()
        with Fence(self.command)as fence:fence.ping()

    def test_idle_session_is_renewed_and_hard_budget_does_not_start_new_write(self):
        with patch.object(Fence,'HEARTBEAT_SECONDS',.02),Fence(self.command)as fence:
            with fence.io_lock:fence.send("SET LOCAL idle_in_transaction_session_timeout='150ms';\n")
            time.sleep(.35)
            fence.ping()
            fence.before_write()
            with fence.io_lock:fence.deadline=time.monotonic()+.05
            with self.assertRaises(Rejected):fence.before_write()
            time.sleep(.1)
            with self.assertRaises(Rejected):fence.ping()

    def test_backend_loss_closes_gate_and_releases_table_locks(self):
        with Fence(self.command)as fence:
            self.sql('SELECT pg_terminate_backend('+str(fence.pid)+');')
            with self.assertRaises(Rejected):fence.ping()
        self.sql("INSERT INTO control_plane.runtime_leases VALUES ('CLAIMED');")


if __name__=='__main__':unittest.main()
