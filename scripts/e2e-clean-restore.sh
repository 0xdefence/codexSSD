#!/usr/bin/env bash
#
# End-to-end "safe sandbox" test for the clean/restore flow.
#
# Builds the codexssd binary and drives it against a THROWAWAY $HOME containing
# fake Codex logs — it never touches the real ~/.codex. Asserts the full
# round-trip: dry-run is read-only, `clean --yes` moves logs into a recoverable
# bin with a manifest, `restore <id>` brings them back, restore refuses to
# overwrite a live log, and an unknown id fails cleanly.
#
# Runs in CI (see .github/workflows/ci.yml) and locally:  ./scripts/e2e-clean-restore.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

bin="$workdir/codexssd"
sandbox="$workdir/home"
codexdir="$sandbox/.codex"
mkdir -p "$codexdir"

go build -o "$bin" ./cmd/codexssd

# Fake Codex logs in the sandbox home.
printf 'fake-sqlite-db' >"$codexdir/logs_2.sqlite"
head -c 1048576 /dev/zero >"$codexdir/logs_2.sqlite-wal" # 1 MiB
printf 'shm' >"$codexdir/logs_2.sqlite-shm"

# Every invocation runs against the sandbox home, never the real one.
run() { HOME="$sandbox" "$bin" "$@"; }
fail() {
	echo "E2E FAIL: $1" >&2
	exit 1
}

echo "== 1. dry-run is read-only =="
run clean >/dev/null
[ -f "$codexdir/logs_2.sqlite" ] || fail "dry-run moved logs_2.sqlite"
[ ! -d "$codexdir/codexssd-backups" ] || fail "dry-run created a backup dir"

echo "== 2. clean --yes moves logs aside with a manifest =="
run clean --yes >/dev/null
[ ! -f "$codexdir/logs_2.sqlite" ] || fail "clean --yes left logs_2.sqlite in place"
[ ! -f "$codexdir/logs_2.sqlite-wal" ] || fail "clean --yes left logs_2.sqlite-wal in place"
backup="$(find "$codexdir/codexssd-backups" -mindepth 1 -maxdepth 1 -type d | head -n1)"
[ -n "$backup" ] || fail "no backup directory created"
[ -f "$backup/logs_2.sqlite" ] || fail "logs_2.sqlite not in backup"
[ -f "$backup/manifest.json" ] || fail "manifest.json missing from backup"
grep -q '"hold_until"' "$backup/manifest.json" || fail "manifest missing hold_until"

echo "== 3. restore brings the logs back =="
id="$(basename "$backup")"
run restore "$id" >/dev/null
[ -f "$codexdir/logs_2.sqlite" ] || fail "restore did not return logs_2.sqlite"
[ -f "$codexdir/logs_2.sqlite-wal" ] || fail "restore did not return logs_2.sqlite-wal"
[ "$(cat "$codexdir/logs_2.sqlite")" = "fake-sqlite-db" ] || fail "restored logs_2.sqlite content changed"
[ ! -d "$backup" ] || fail "backup dir not removed after restore"

echo "== 4. restore refuses to overwrite a live log =="
run clean --yes >/dev/null
printf 'fresh-live-log' >"$codexdir/logs_2.sqlite" # a new log appeared (Codex ran again)
newid="$(basename "$(find "$codexdir/codexssd-backups" -mindepth 1 -maxdepth 1 -type d | head -n1)")"
if run restore "$newid" >/dev/null 2>&1; then fail "restore overwrote an existing live log"; fi
[ "$(cat "$codexdir/logs_2.sqlite")" = "fresh-live-log" ] || fail "live log was clobbered by a refused restore"

echo "== 5. unknown backup id fails cleanly =="
if run restore definitely-not-a-real-id >/dev/null 2>&1; then fail "unknown id did not exit non-zero"; fi

echo "E2E PASS"
