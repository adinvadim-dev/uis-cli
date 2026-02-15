# uis-cli

Command-line client for UIS Data API (JSON-RPC).

Status: Go implementation (this repo).

## Build

```bash
go build ./cmd/uis
./uis --help
```

## Auth

```bash
./uis auth login
# or for CI:
UIS_TOKEN=... ./uis campaigns list
```

## Human-Friendly Commands

```bash
./uis campaigns list --limit 10
./uis campaigns create --help
./uis contacts list --help
```

Default output is human-readable (tables / key-value). Use `--json` to force JSON:

```bash
./uis --json campaigns list --limit 10
```

Preview without network (validates flags + builds JSON-RPC request):

```bash
./uis --dry-run ct-summary-report get --date-from "2026-01-01 00:00:00" --date-till "2026-01-02 00:00:00"
./uis --json --dry-run campaigns list --limit 10
```

## Low-Level Raw Call (escape hatch)

```bash
./uis call get.campaigns --param limit:=10
./uis call get.campaigns --params-json '{"limit":10}'
```

## Update Embedded Spec

This repo embeds a snapshot of UIS docs into `internal/spec/data/api_spec.json`.

```bash
make spec
make gen
make build
```
