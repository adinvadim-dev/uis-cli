#!/usr/bin/env python3
"""Drive the real UIS executable; keep evidence separate from disposable state."""
import argparse
import datetime as dt
import hashlib
import http.client
import http.server
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import threading

REPO = Path(__file__).resolve().parents[4]
SCHEMA = 'uis.verification/v1'


class VerificationFailure(Exception):
    pass


def require(condition, message):
    if not condition:
        raise VerificationFailure(message)


def git(*args):
    return subprocess.check_output(['git', '-C', str(REPO), *args], text=True).strip()


def fingerprint():
    # Include work-in-progress Go code as well as the recorded commit.
    digest = hashlib.sha256()
    for name in sorted(git('ls-files', '--cached', '--others', '--exclude-standard').splitlines()):
        path = REPO / name
        if path.suffix == '.go' or name in ('go.mod', 'go.sum'):
            digest.update(name.encode())
            digest.update(path.read_bytes())
    return digest.hexdigest()


def save(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')
    path.chmod(0o600)


def load_run(path):
    path = Path(path).expanduser().resolve()
    manifest = json.loads((path / 'run.json').read_text())
    require(manifest.get('schema') == SCHEMA and manifest.get('repo') == str(REPO), 'Wrong verification run/repository')
    require(not (path / 'scratch').is_symlink(), 'Scratch symlink refused')
    return path, manifest


def child_env(run):
    env = dict(os.environ)
    for key in list(env):
        if key.startswith('UIS_') or key.lower() in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy'):
            env.pop(key)
    env.update(UIS_CONFIG=str(run / 'scratch' / 'config.toml'), UIS_ACCOUNT='verification',
               XDG_CONFIG_HOME=str(run / 'scratch' / 'config'), XDG_STATE_HOME=str(run / 'scratch' / 'state'))
    return env


def record(run, args, code, **observations):
    with (run / 'evidence' / 'commands.jsonl').open('a') as stream:
        stream.write(json.dumps({'command': args, 'exit': code, **observations}) + '\n')


def cli(run, args, env=None, private=False, want=0):
    command = [str(run / 'scratch' / 'uis'), '--json', '--no-input', '--retries', '0', '--timeout', '30s', *args]
    public_args = [('[id]' if re.fullmatch(r'[0-9]+', arg) else arg) for arg in args] if private else args
    try:
        result = subprocess.run(command, env=env or child_env(run), capture_output=True, text=True, timeout=45)
    except subprocess.TimeoutExpired:
        record(run, public_args, -1, error='timeout')
        raise VerificationFailure('CLI timeout; response suppressed') from None
    try:
        value = json.loads(result.stdout)
    except (ValueError, UnicodeError):
        record(run, public_args, result.returncode, error='invalid_json')
        raise VerificationFailure('CLI did not emit exactly one JSON document; response suppressed') from None
    require(isinstance(value, dict), 'CLI envelope must be an object')
    record(run, public_args, result.returncode, ok=value.get('ok'), error=(value.get('error') or {}).get('code'),
           **({} if private else {'stdout': value, 'stderr': result.stderr}))
    require(result.returncode == want and value.get('schemaVersion') == 'uis.cli/v1' and value.get('ok') == (want == 0),
            'CLI exit/envelope mismatch; response suppressed')
    return value.get('data')


def launch(args):
    require((REPO / 'go.mod').is_file(), 'Run the project-local verifier from the UIS source checkout, not a standalone skill bundle')
    root = Path(args.evidence_root).expanduser().resolve()
    require(not root.is_relative_to(REPO), 'Evidence must be outside the repository')
    root.mkdir(parents=True, exist_ok=True)
    run = Path(tempfile.mkdtemp(prefix='uis-', dir=root))
    run.chmod(0o700)
    (run / 'evidence').mkdir(mode=0o700)
    (run / 'scratch').mkdir(mode=0o700)
    print('Verification run: ' + str(run), file=sys.stderr)
    save(run / 'run.json', {'schema': SCHEMA, 'repo': str(REPO), 'commit': git('rev-parse', 'HEAD'),
                           'source_sha256': fingerprint(), 'driver_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'created_at': dt.datetime.now(dt.timezone.utc).isoformat(),
                           'cleaned': False})
    try:
        built = subprocess.run(['go', 'build', '-o', str(run / 'scratch' / 'uis'), './cmd/uis'], cwd=REPO,
                               capture_output=True, timeout=180)
        # Compiler output contains source locations, not live provider responses.
        (run / 'evidence' / 'build.log').write_bytes(built.stdout + built.stderr)
        require(built.returncode == 0, 'Build failed; see build.log')
        marker = run / 'scratch' / 'credential-called'
        helper = run / 'scratch' / 'credential.py'
        helper.write_text('from pathlib import Path\nPath(' + repr(str(marker)) + ').touch()\nraise SystemExit(99)\n')
        helper.chmod(0o700)
        config = 'default_account="verification"\n[accounts.verification]\nexpected_login="verify@example.invalid"\n[accounts.verification.credential_process]\ncommand=' + json.dumps([sys.executable, str(helper)]) + '\n'
        (run / 'scratch' / 'config.toml').write_text(config)
        path, manifest = load_run(run)
        manifest['binary_sha256'] = hashlib.sha256((run / 'scratch' / 'uis').read_bytes()).hexdigest()
        save(path / 'run.json', manifest)
        print(run)
    except BaseException:
        cleanup_run(run)
        raise


def doctor(run):
    path, manifest = load_run(run)
    require(not manifest['cleaned'], 'Run was cleaned; launch a new one')
    require(manifest['source_sha256'] == fingerprint(), 'Go source changed; launch a new binary')
    require(manifest['driver_sha256'] == hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'Verification helper changed; launch a new run')
    binary = path / 'scratch' / 'uis'
    require(hashlib.sha256(binary.read_bytes()).hexdigest() == manifest['binary_sha256'], 'Binary changed')
    version = subprocess.run([str(binary), '--version'], capture_output=True, text=True, timeout=10)
    require(version.returncode == 0 and version.stdout.strip(), 'Binary is not usable')
    help_result = subprocess.run([str(binary), 'staff', '--help'], capture_output=True, text=True, timeout=10)
    require(help_result.returncode == 0 and all(x in help_result.stdout for x in ('employees', 'groups', 'sip-lines', 'softphone')), 'staff entry points missing')
    save(path / 'evidence' / 'doctor.json', {'ok': True, 'binary_sha256': manifest['binary_sha256'],
         'source_sha256': manifest['source_sha256'], 'driver_sha256': manifest['driver_sha256'], 'version': version.stdout.strip(), 'staff_help': True,
         'auth': 'not tested; staff-read performs authenticated provider requests'})
    return path


def snapshot(path):
    return {str(p.relative_to(path)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in path.rglob('*') if p.is_file()}


def staff_guards(run):
    requests = []

    class Trap(http.server.BaseHTTPRequestHandler):
        def do_CONNECT(self):
            requests.append('CONNECT')
            self.send_error(502)

        def do_GET(self):
            requests.append('GET')
            self.send_error(502)

        def do_POST(self):
            requests.append('POST')
            self.send_error(502)

        def log_message(self, *unused):
            pass

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Trap)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        probe = http.client.HTTPConnection(*server.server_address, timeout=3)
        probe.request('CONNECT', 'example.invalid:443')
        require(probe.getresponse().status == 502 and requests == ['CONNECT'], 'Network observer not working')
        probe.close()
        requests.clear()
        env = child_env(run)
        proxy = 'http://127.0.0.1:' + str(server.server_port)
        env.update(HTTP_PROXY=proxy, HTTPS_PROXY=proxy, ALL_PROXY=proxy, NO_PROXY='')
        fixtures = {'employees': {'last_name': 'Synthetic Operator'}, 'softphone': {'is_need_hide_numbers': True},
                    'sip-lines': {'state': 'manual_lock', 'channels_count': 2, 'is_multiterminal': True}}
        for resource, payload in fixtures.items():
            save(run / 'scratch' / (resource + '.json'), payload)
        before, refs = snapshot(run / 'scratch'), git('show-ref')
        for resource in fixtures:
            command = ['staff', resource, 'update', '123', '--payload-file', str(run / 'scratch' / (resource + '.json'))]
            plan = cli(run, ['--dry-run', *command], env)
            require(plan['network'] is False and plan['mutation'] is True and plan['read_back'] is True and plan['method'] == 'PUT', 'Incorrect update plan')
            cli(run, command, env, want=10)
        require(not requests, 'Dry-run/confirmation guard attempted network')
        require(snapshot(run / 'scratch') == before and git('show-ref') == refs, 'Guard changed scratch files or git refs')
        require(not (run / 'scratch' / 'credential-called').exists(), 'Guard invoked credentials')
        save(run / 'evidence' / 'staff-guards.json', {'ok': True, 'boundary': 'real CLI, synthetic local patches; no provider mutation',
             'resources': list(fixtures), 'proxy_observer_self_test': True, 'http_proxy_requests': len(requests),
             'credential_invoked': False, 'scratch_unchanged': True, 'git_refs_unchanged': True,
             'limitation': 'HTTP observer covers the configured Go HTTP transport; this is not an OS-wide egress monitor'})
    finally:
        server.shutdown()
        server.server_close()
        worker.join(timeout=5)


def staff_read(run, config, account):
    require(config and account and re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}', account), 'Explicit --config and --account required')
    config = Path(config).expanduser().resolve(strict=True)
    config_before = hashlib.sha256(config.read_bytes()).hexdigest()
    account_config = cli(run, ['--config', str(config), '--account', account, 'account', 'show'], private=True)
    login = account_config.get('ExpectedLogin')
    require(isinstance(login, str) and login, 'Bound expected_login required')
    source_root = Path(os.environ.get('XDG_STATE_HOME', str(Path.home() / '.local/state')))
    source = source_root / 'uis' / 'accounts' / account / 'session.json'
    raw = source.read_bytes()
    before = hashlib.sha256(raw).hexdigest()
    # Copy only this selected session. Never start login or resolve credentials.
    target = run / 'scratch' / 'state' / 'uis' / 'accounts' / 'verification'
    target.mkdir(parents=True, exist_ok=True, mode=0o700)
    session = target / 'session.json'
    session.touch(mode=0o600)
    session.write_bytes(raw)
    staged = run / 'scratch' / 'config.toml'
    staged.write_text('default_account="verification"\n[accounts.verification]\nexpected_login=' + json.dumps(login) + '\n' +
                      ('expected_customer_id=' + json.dumps(account_config['ExpectedCustomerID']) + '\n' if account_config.get('ExpectedCustomerID') else ''))
    try:
        def read(resource, action, *args):
            data = cli(run, ['staff', resource, action, *args], private=True)
            require(data.get('source') == 'private.cabinet' and data.get('observed_at'), 'Missing live provenance')
            return data['data']
        employees = read('employees', 'list')
        groups = read('groups', 'list')
        lines = read('sip-lines', 'list')
        require(employees and groups and lines, 'Live fixture needs employees, a group and SIP lines')
        selected = {False: None, True: None}
        for line in lines:
            if isinstance(line.get('is_registered'), bool) and line.get('employee_id'):
                selected[line['is_registered']] = str(line['employee_id'])
        require(all(selected.values()), 'Fixture missing registered or unregistered employee line; both comparisons NOT verified')
        proof = []
        for registered, employee_id in selected.items():
            card = read('employees', 'get', employee_id)
            membership = read('employees', 'groups', employee_id)
            softphone = read('softphone', 'get', employee_id)
            linked = read('sip-lines', 'list', '--employee-id', employee_id)
            require(str(card.get('id')) == employee_id and linked and all(str(x.get('employee_id')) == employee_id for x in linked), 'Employee/line identity mismatch')
            require(any(x.get('is_registered') is registered for x in linked), 'Registration changed during comparison; rerun explicitly')
            require({str(x['id']) for x in card['sip_lines']} == {str(x['id']) for x in linked}, 'Card/line views disagree')
            require(str(membership['employee_id']) == employee_id and str(softphone['employee_id']) == employee_id, 'Related view identity mismatch')
            require(set(softphone['settings']) == {'is_need_hide_numbers', 'is_click_to_call_auto_answer_enabled', 'is_out_calls_auto_answer_enabled'}, 'Softphone fields missing')
            for line in linked:
                detail = read('sip-lines', 'get', str(line['id']))
                require(str(detail['id']) == str(line['id']), 'SIP detail identity mismatch')
            proof.append({'registered': registered, 'linked_lines': len(linked), 'groups': len(membership['groups']), 'softphone_fields': sorted(softphone['settings'])})
        group = read('groups', 'get', str(groups[0]['id']))
        require(str(group['id']) == str(groups[0]['id']), 'Group detail identity mismatch')
        require(before == hashlib.sha256(source.read_bytes()).hexdigest() and config_before == hashlib.sha256(config.read_bytes()).hexdigest(), 'Original session/config changed')
        save(run / 'evidence' / 'staff-read.json', {'ok': True, 'boundary': 'live UIS GETs through real CLI; no provider writes',
             'employee_count': len(employees), 'group_count': len(groups), 'line_count': len(lines), 'comparisons': proof,
             'source_session_unchanged': True, 'source_config_unchanged': True, 'raw_output_saved': False})
    finally:
        # Never retain copied live credentials, even when a fixture/assertion fails.
        shutil.rmtree(run / 'scratch' / 'state', ignore_errors=True)
        staged.write_text('default_account="verification"\n[accounts.verification]\nexpected_login="verify@example.invalid"\n')


def cleanup_run(run):
    run, manifest = load_run(run)
    shutil.rmtree(run / 'scratch', ignore_errors=False) if (run / 'scratch').exists() else None
    manifest['cleaned'] = True
    save(run / 'run.json', manifest)
    require((run / 'evidence').is_dir(), 'Evidence lost in cleanup')
    save(run / 'evidence' / 'cleanup.json', {'ok': True, 'scratch_removed': not (run / 'scratch').exists(), 'evidence_retained': True})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest='action', required=True)
    p = subs.add_parser('launch')
    p.add_argument('--evidence-root', default=str(Path.home() / '.local/state/uis/verification'))
    for command in ('doctor', 'cleanup', 'drive'):
        p = subs.add_parser(command)
        p.add_argument('--run-dir', required=True)
        if command == 'drive':
            p.add_argument('--feature', choices=['staff-guards', 'staff-read'], required=True)
            p.add_argument('--config')
            p.add_argument('--account')
    args = parser.parse_args()
    try:
        if args.action == 'launch':
            launch(args)
            return
        run, _ = load_run(args.run_dir)
        if args.action == 'cleanup':
            cleanup_run(run)
        elif args.action == 'doctor':
            doctor(run)
        else:
            doctor(run)
            if args.feature == 'staff-guards':
                staff_guards(run)
            else:
                staff_read(run, args.config, args.account)
        print(json.dumps({'ok': True, 'action': args.action, 'run_dir': str(run)}))
    except (VerificationFailure, OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        # Unexpected provider/config payloads never become exception text in evidence.
        if hasattr(args, 'run_dir'):
            run, _ = load_run(args.run_dir)
            save(run / 'evidence' / 'failure.json', {'ok': False, 'action': args.action,
                 'feature': getattr(args, 'feature', None), 'error': str(error) if isinstance(error, VerificationFailure) else type(error).__name__})
        print('Verification failed; inspect sanitized failure.json/build.log and run cleanup.', file=sys.stderr)
        raise SystemExit(1) from None


if __name__ == '__main__':
    main()
