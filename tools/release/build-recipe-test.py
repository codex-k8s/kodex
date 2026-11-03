"""Синтетические Git contracts: recipe pin без изменения application source."""
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
SPEC=importlib.util.spec_from_file_location('recipe',ROOT/'tools/release/build-recipe.py');MODULE=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(MODULE)
BASE=b'FROM synthetic AS build\nRUN true\nFROM synthetic-debian AS opencode-binary\nRUN echo '+b'1'*64+b'  /tmp/opencode.tar.gz\nRUN test "$(/out/opencode --version)" = \'1.0.0\'\nFROM synthetic-node AS full-runtime\nRUN true\n'
NEW=BASE.replace(b'FROM synthetic-debian AS opencode-binary',b'FROM synthetic-node AS opencode-binary')

class Recipe(unittest.TestCase):
    def setUp(self):
        self.temporary=tempfile.TemporaryDirectory();self.root=Path(self.temporary.name);self.repo=self.root/'app';self.repo.mkdir()
        self.git('init','--quiet');self.git('config','user.name','Synthetic');self.git('config','user.email','test@example.invalid')
        target=self.repo/MODULE.RECIPE;target.parent.mkdir(parents=True);target.write_bytes(BASE)
        (self.repo/'services/synthetic.go').write_text('package synthetic\n');self.git('add','.');self.git('commit','-qm','base');self.base=self.git('rev-parse','HEAD').strip()
        target.write_bytes(NEW);self.git('commit','-qam','recipe');self.recipe=self.git('rev-parse','HEAD').strip()
        self.tooling=self.root/'tooling';self.git('worktree','add','--quiet','--detach',str(self.tooling),self.recipe);self.git('checkout','--quiet','--detach',self.base)
        self.context=self.root/'context';self.context.mkdir();target=self.context/MODULE.RECIPE;target.parent.mkdir(parents=True);target.write_bytes(BASE)
    def tearDown(self):self.temporary.cleanup()
    def git(self,*args):return subprocess.check_output(['git','-C',str(self.repo),*args],stderr=subprocess.DEVNULL,text=True)
    def materialize(self):return MODULE.materialize(self.repo,self.tooling,self.base,self.recipe,self.context)
    def test_exact_stage_is_recorded_without_modifying_application_tree(self):
        result=self.materialize();self.assertEqual((self.context/MODULE.RECIPE).read_bytes(),NEW)
        self.assertEqual((self.repo/MODULE.RECIPE).read_bytes(),BASE)
        self.assertEqual(result['source_sha'],self.recipe);self.assertEqual(result['application_source_sha'],self.base)
        self.assertEqual(len(result['files'][0]['sha256']),64)
    def test_changed_source_worktree_is_rejected(self):
        (self.repo/'services/synthetic.go').write_text('changed\n')
        with self.assertRaises(ValueError):self.materialize()
    def test_recipe_cannot_change_other_runtime_inputs(self):
        (self.tooling/'services/synthetic.go').write_text('package changed\n')
        subprocess.run(['git','-C',str(self.tooling),'commit','-qam','changed runtime'],check=True)
        self.recipe=subprocess.check_output(['git','-C',str(self.tooling),'rev-parse','HEAD'],text=True).strip()
        with self.assertRaises(ValueError):self.materialize()
    def test_recipe_cannot_change_another_stage(self):
        (self.tooling/MODULE.RECIPE).write_bytes(NEW.replace(b'RUN true',b'RUN false',1))
        subprocess.run(['git','-C',str(self.tooling),'commit','-qam','changed prefix'],check=True)
        self.recipe=subprocess.check_output(['git','-C',str(self.tooling),'rev-parse','HEAD'],text=True).strip()
        with self.assertRaises(ValueError):self.materialize()
    def test_digest_or_native_version_probe_cannot_be_removed(self):
        (self.tooling/MODULE.RECIPE).write_bytes(NEW.replace(b'1'*64,b'2'*64))
        subprocess.run(['git','-C',str(self.tooling),'commit','-qam','changed digest'],check=True)
        self.recipe=subprocess.check_output(['git','-C',str(self.tooling),'rev-parse','HEAD'],text=True).strip()
        with self.assertRaises(ValueError):self.materialize()
    def test_ambiguous_stage_or_context_mismatch_is_rejected(self):
        with self.assertRaises(ValueError):MODULE.split_stage(NEW+NEW)
        (self.context/MODULE.RECIPE).write_bytes(b'other')
        with self.assertRaises(ValueError):self.materialize()

if __name__=='__main__':unittest.main()
