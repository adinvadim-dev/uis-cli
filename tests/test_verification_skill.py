"""Protect evidence retention and the verification harness's privacy boundary."""
import argparse
import contextlib
import hashlib
import io
import importlib.util
import json
from pathlib import Path
import sys
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('uis_verifier', ROOT / '.agents/skills/verify-uis-cli/scripts/verify.py')
verify = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(verify)


class VerificationSkillTests(unittest.TestCase):
    def make_run(self, root):
        run = root / 'run'
        (run / 'scratch').mkdir(parents=True)
        (run / 'evidence').mkdir()
        verify.save(run / 'run.json', {'schema': verify.SCHEMA, 'repo': str(ROOT), 'cleaned': False})
        return run

    def test_fingerprint_accepts_tracked_go_file_deleted_in_working_tree(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            subprocess.run(['git', 'init', '-q', str(root)], check=True)
            module = root / 'go.mod'
            module.write_text('module fixture\n')
            deleted = root / 'removed.go'
            deleted.write_text('package fixture\n')
            subprocess.run(['git', '-C', str(root), 'add', 'go.mod', 'removed.go'], check=True)
            with mock.patch.object(verify, 'REPO', root):
                before = verify.fingerprint()
                deleted.unlink()
                expected = hashlib.sha256(b'go.mod' + module.read_bytes()).hexdigest()
                self.assertEqual(verify.fingerprint(), expected)
                self.assertNotEqual(before, expected)

    def test_launch_source_read_failure_is_cleanable_and_keeps_failure_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            with mock.patch.object(verify, 'fingerprint', side_effect=FileNotFoundError('synthetic source race')):
                with contextlib.redirect_stderr(io.StringIO()):
                    with self.assertRaises(FileNotFoundError):
                        verify.launch(argparse.Namespace(evidence_root=tmp))
            runs = list(Path(tmp).iterdir())
            self.assertEqual(len(runs), 1)
            run = runs[0]
            self.assertFalse((run / 'scratch').exists())
            self.assertTrue(json.loads((run / 'run.json').read_text())['cleaned'])
            self.assertTrue(json.loads((run / 'evidence/cleanup.json').read_text())['evidence_retained'])

    def test_cleanup_removes_only_owned_scratch_and_keeps_proof(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            run = self.make_run(root)
            (run / 'scratch/session.json').write_text('PRIVATE_SESSION')
            proof = run / 'evidence/commands.jsonl'
            proof.write_text('{"exit":0}\n')
            sibling = root / 'original-session'
            sibling.write_text('ORIGINAL')
            verify.cleanup_run(run)
            self.assertFalse((run / 'scratch').exists())
            self.assertEqual(proof.read_text(), '{"exit":0}\n')
            self.assertEqual(sibling.read_text(), 'ORIGINAL')
            self.assertTrue(json.loads((run / 'run.json').read_text())['cleaned'])
            self.assertTrue(json.loads((run / 'evidence/cleanup.json').read_text())['evidence_retained'])

    def test_cleanup_refuses_scratch_symlink(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            run = self.make_run(root)
            (run / 'scratch').rmdir()
            target = root / 'unrelated'
            target.mkdir()
            secret = target / 'session'
            secret.write_text('ORIGINAL')
            (run / 'scratch').symlink_to(target, target_is_directory=True)
            with self.assertRaises(verify.VerificationFailure):
                verify.cleanup_run(run)
            self.assertEqual(secret.read_text(), 'ORIGINAL')

    def test_private_process_evidence_discards_stdout_stderr_and_provider_messages(self):
        with tempfile.TemporaryDirectory() as tmp:
            run = self.make_run(Path(tmp))
            binary = run / 'scratch/uis'
            # This fixture is a subprocess boundary, not a substitute for live CLI proof.
            binary.write_text('#!' + sys.executable + '\nimport json,sys\n'
                'print(json.dumps({"schemaVersion":"uis.cli/v1","ok":False,"error":{"code":"auth_error","message":"PRIVATE_RESPONSE"}}))\n'
                'print("PRIVATE_STDERR",file=sys.stderr)\nraise SystemExit(3)\n')
            binary.chmod(0o700)
            verify.cli(run, ['staff', 'employees', 'get', '987654'], private=True, want=3)
            evidence = (run / 'evidence/commands.jsonl').read_text()
            self.assertNotIn('PRIVATE_RESPONSE', evidence)
            self.assertNotIn('PRIVATE_STDERR', evidence)
            self.assertNotIn('987654', evidence)
            value = json.loads(evidence)
            self.assertEqual(value['error'], 'auth_error')
            self.assertEqual(value['exit'], 3)
            self.assertEqual(value['command'][-1], '[id]')


if __name__ == '__main__':
    unittest.main()
