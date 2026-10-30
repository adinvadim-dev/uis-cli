#!/usr/bin/env python3
"""Validate/package the UIS skill and its relative document dependencies."""
import argparse
import json
from pathlib import Path
import re
import shutil
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]


def manifest(root=ROOT):
    paths = {'SKILL.md', 'scripts/call_brief.py'}
    pending = ['SKILL.md']
    while pending:
        name = pending.pop()
        text = (root / name).read_text()
        for target in re.findall(r'\]\(([^)]+)\)', text):
            target = target.split('#', 1)[0]
            if not target or urlsplit(target).scheme or target.startswith('/'):
                continue
            dep = ((root / name).parent / unquote(target)).resolve()
            rel = str(dep.relative_to(root.resolve()))
            if not dep.is_file():
                raise ValueError(f'{name}: missing dependency {target}')
            if rel not in paths:
                paths.add(rel)
                if dep.suffix == '.md':
                    pending.append(rel)
    # Markdown dependencies are checked recursively; executable recipe pointers are explicit.
    if not (root / 'scripts/call_brief.py').is_file():
        raise ValueError('missing compact projection helper')
    return sorted(paths)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, help='new bundle directory; existing paths refused')
    args = parser.parse_args()
    paths = manifest()
    if args.output:
        args.output.mkdir(parents=True, exist_ok=False)
        for name in paths:
            dest = args.output / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, dest)
        # Also check the actual packaged tree, not only the source.
        manifest(args.output)
    print(json.dumps({'ok': True, 'files': paths}))


if __name__ == '__main__':
    main()
