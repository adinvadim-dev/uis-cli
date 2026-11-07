#!/usr/bin/env python3
"""Read-only, bounded autodial summary. Prints JSON; never sends messages."""
import argparse
import concurrent.futures
import datetime as dt
import json
import math
import os
import re
import shutil
import subprocess
import tempfile
import threading
import time
from zoneinfo import ZoneInfo

MSK = ZoneInfo('Europe/Moscow')
LIMITATION = 'Перевод — отметка UIS; полнота истории не гарантирована.'


class SourceError(Exception):
    def __init__(self, code, auth=False):
        super().__init__(code)
        self.auth = auth


class Cli:
    def __init__(self, binary, account):
        self.binary, self.account = binary, account

    def request(self, args, timeout):
        command = [self.binary, '--account', self.account, '--json', '--no-input',
                   '--timeout', f'{max(1, timeout - 1):.1f}s', *args]
        try:
            process = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            raise SourceError('timeout') from None
        except OSError:
            raise SourceError('cli_unavailable') from None
        try:
            envelope = json.loads(process.stdout)
        except (ValueError, TypeError):
            raise SourceError('invalid_json') from None
        error = envelope.get('error', {}) if isinstance(envelope, dict) else {}
        code = str(error.get('code', '')) if isinstance(error, dict) else ''
        auth = process.returncode == 3 or code in ('authentication_required', 'auth_required')
        if process.returncode or not isinstance(envelope, dict) or envelope.get('ok') is not True:
            # Provider messages/stdout/stderr can contain customer data or credentials.
            raise SourceError('authentication_required' if auth else 'source_failed', auth)
        return envelope

    def refresh(self, timeout):
        self.request(['auth', 'refresh', '--surface', 'private'], timeout)


def safe_name(value):
    text = re.sub(r'\s+', ' ', str(value))[:80]
    text = re.sub(r'\+?\d[\d ()-]{8,}\d', '[номер скрыт]', text)
    return re.sub(r'([_*`\[\]\\])', r'\\\1', text)


def collect(instant, client, day=None, budget=150, clock=time.monotonic):
    if instant.tzinfo is None:
        raise ValueError('UTC instant must include an offset')
    started = clock()
    deadline = started + budget
    local = instant.astimezone(MSK)
    historical = day is not None
    weekend = local.weekday() >= 5
    result = {'msk_now': local.isoformat(), 'day': str(day or local.date()),
              'mode': 'historical' if historical else 'scheduled', 'weekend_msk': weekend,
              'should_send': False, 'campaigns': [], 'failures': [], 'limitations': [LIMITATION]}
    if weekend and not historical:
        result.update(skip_reason='weekend_msk', elapsed_sec=round(clock() - started, 3))
        return result
    refresh_lock = threading.Lock()
    refresh_attempted = False
    refresh_ok = False

    def remaining(cap=40):
        value = min(cap, deadline - clock())
        if value <= 0:
            raise SourceError('deadline_exceeded')
        return value

    def request(args):
        nonlocal refresh_attempted, refresh_ok
        try:
            return client.request(args, remaining())
        except SourceError as error:
            if not error.auth:
                raise
            with refresh_lock:
                if not refresh_attempted:
                    refresh_attempted = True
                    try:
                        client.refresh(remaining(60))
                        refresh_ok = True
                    except SourceError:
                        refresh_ok = False
            if not refresh_ok:
                raise SourceError('authentication_required', True)
            return client.request(args, remaining())

    payload = {'begin_date': f'{result["day"]} 00:00:00',
               'end_date': f'{result["day"]} 23:59:59', 'statuses': ['in_process']}
    fd, path = tempfile.mkstemp(suffix='.json')
    try:
        with os.fdopen(fd, 'w') as file:
            json.dump(payload, file)
        envelope = request(['autodial', 'list', '--payload-file', path])
        rows = envelope.get('data', {}).get('data')
        if not isinstance(rows, list):
            raise SourceError('invalid_campaign_list')
        campaigns = []
        ids = set()
        for row in rows:
            if not isinstance(row, dict) or not isinstance(row.get('id'), int) or isinstance(row['id'], bool) or row['id'] <= 0:
                raise SourceError('invalid_campaign_row')
            if row['id'] in ids:
                raise SourceError('duplicate_campaign_id')
            ids.add(row['id'])
            if row.get('status') == 'in_process':
                campaigns.append({'id': row['id'], 'name': safe_name(row.get('name') or row['id'])})
    except (SourceError, AttributeError, TypeError) as error:
        result['failures'].append({'source': 'campaign_list', 'code': str(error) if isinstance(error, SourceError) else 'invalid_campaign_list'})
        result['message_text'] = '📋 Автообзвон\nСписок кампаний недоступен; это не нулевой результат.'
        result.update(should_send=not weekend and not historical, elapsed_sec=round(clock() - started, 3))
        return result
    finally:
        os.unlink(path)

    def calls(campaign):
        try:
            envelope = request(['autodial', 'calls', str(campaign['id'])])
            grid = envelope['data']['data']['grid_data']
            if not isinstance(grid, list):
                raise SourceError('invalid_calls')
            answered = transfers = 0
            for row in grid:
                if not isinstance(row, dict):
                    raise SourceError('invalid_call_row')
                talk = row.get('talk_duration')
                if talk is None:
                    talk = 0
                if isinstance(talk, bool) or not isinstance(talk, (float, int)) or not math.isfinite(talk) or talk < 0:
                    raise SourceError('invalid_talk_duration')
                marks = row.get('marks') or []
                if isinstance(marks, str):
                    marks = [marks]
                if not isinstance(marks, list) or any(not isinstance(mark, str) for mark in marks):
                    raise SourceError('invalid_marks')
                answered += talk > 0
                transfers += any('Перевод' in mark for mark in marks)
            return {**campaign, 'status': 'ok', 'attempts': len(grid), 'answered': answered,
                    'transfers_uis_mark': transfers}
        except (SourceError, KeyError, TypeError) as error:
            return {**campaign, 'status': 'error', 'code': str(error) if isinstance(error, SourceError) else 'invalid_calls'}

    # Every real subprocess receives the remaining global budget, including queued reads.
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
        result['campaigns'] = list(executor.map(calls, campaigns))
    result['failures'] = [{'source': f'calls:{row["id"]}', 'code': row['code']}
                          for row in result['campaigns'] if row['status'] == 'error']
    lines = ['📋 Автообзвон']
    for row in result['campaigns']:
        if row['status'] == 'error':
            lines.append(f'{row["name"]}: данные недоступны, не ноль.')
        else:
            n, m, k = row['attempts'], row['answered'], row['transfers_uis_mark']
            percent = lambda part, whole: round(part * 100 / whole) if whole else 0
            lines.append(f'{row["name"]}: набрано {n} → дозвон {m} ({percent(m, n)}%) → перевод {k} ({percent(k, m)}%)')
    if not campaigns:
        lines.append('Запущенных кампаний автообзвона сейчас нет.')
    lines.append(LIMITATION)
    if historical:
        lines.append(f'Исторические данные за {day}; не запуск расписания.')
    result.update(message_text='\n'.join(lines), should_send=not weekend and not historical,
                  auth_refresh_used=refresh_attempted, elapsed_sec=round(clock() - started, 3))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--uis-bin', default=shutil.which('uis') or 'uis')
    parser.add_argument('--account', default='medivey')
    parser.add_argument('--date', type=dt.date.fromisoformat, help='Historical Moscow date; never sendable')
    parser.add_argument('--now', help='Offset-aware ISO instant for verification; never sendable')
    args = parser.parse_args()
    try:
        instant = dt.datetime.fromisoformat(args.now.replace('Z', '+00:00')) if args.now else dt.datetime.now(dt.timezone.utc)
        result = collect(instant, Cli(args.uis_bin, args.account), day=args.date)
    except ValueError as error:
        parser.error(str(error))
    if args.now:
        result.update(should_send=False, clock_overridden=True)
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
