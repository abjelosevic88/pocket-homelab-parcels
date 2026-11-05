# Backup

One SQLite file: `DATA_DIR/pocket-parcels.db` (WAL mode). Take a consistent copy with `pocket-parcels backup` (writes `DATA_DIR/backups/pocket-parcels-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-parcels.db` (remove `-wal`/`-shm` files), start.

Every person can also export their own data as JSON (`/api/v1/export.json`: parcels, timelines, papers; re-importable) or their parcels as CSV.
