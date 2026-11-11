#!/bin/sh
# Copies the live board database to backups/, checks the copy, and keeps the
# last KEEP_DAYS days. SQLite's .backup is safe while the board is running.
# Runs as the container's user (65532) so it never leaves root-owned -wal or
# -shm files that the board could not write.
set -eu

DIR=${AGENTBOARD_DIR:-/opt/agentboard}
KEEP_DAYS=${KEEP_DAYS:-14}
UID_GID=65532
STAMP=$(date -u +%Y%m%d-%H%M%S)
OUT="$DIR/backups/board-$STAMP.db"

as_board() { setpriv --reuid=$UID_GID --regid=$UID_GID --clear-groups "$@"; }

install -d -o $UID_GID -g $UID_GID -m 700 "$DIR/backups"
as_board sqlite3 "$DIR/data/board.db" ".timeout 10000" ".backup '$OUT'"

if [ "$(as_board sqlite3 "$OUT" 'PRAGMA integrity_check;')" != "ok" ]; then
	echo "backup $OUT failed its integrity check" >&2
	exit 1
fi
as_board gzip "$OUT"

find "$DIR/backups" -name 'board-*.db.gz' -mtime +"$KEEP_DAYS" -delete
echo "backed up to $OUT.gz"
