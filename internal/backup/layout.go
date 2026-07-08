// Package backup owns SSHDock's portable single-node backup archive.
//
// Archive layout v1:
//   - manifest.json: format version, source paths, entry metadata, restore guardrails.
//   - data/: copy of SSHDOCK_DATA_DIR, excluding the default backups directory.
//   - caddy/generated.caddyfile: configured generated SSHDock Caddy route file, when present.
//   - caddy/main.Caddyfile: configured Caddy main file, when present.
//   - docker/volumes.json: Docker volume inventory only. Volume contents are not included.
//
// Restore extracts to a temporary directory first, validates the manifest, safe
// archive paths, required database entry, config-key mode and length, safe
// symlinks, and target directory modes, then mutates configured target paths.
// This keeps validation failures from partially replacing runtime state.
package backup
