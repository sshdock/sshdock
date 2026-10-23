#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 1 ]; then
	printf 'usage: %s OUTPUT_FILE\n' "${0##*/}" >&2
	exit 1
fi

output="$1"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
export LC_ALL=C
shopt -s nullglob nocaseglob

append_notices() {
	local title="$1" directory="$2" file license_found=false
	for file in "$directory"/LICENSE* "$directory"/LICENCE* "$directory"/COPYING* "$directory"/UNLICENSE*; do
		if [ -f "$file" ] && [ -s "$file" ]; then
			license_found=true
		fi
	done
	if [ "$license_found" != true ]; then
		printf 'release notices: no root license for %s\n' "$title" >&2
		exit 1
	fi

	find "$directory" -type f \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' -o -iname 'LEGAL*' -o -iname 'UNLICENSE*' \) -print > "$work/files"
	sort "$work/files" -o "$work/files"
	printf '\n============================================================\n%s\n' "$title" >> "$work/notices"
	while IFS= read -r file; do
		printf '\n--- %s ---\n' "${file#"$directory"/}" >> "$work/notices"
		cat "$file" >> "$work/notices"
		printf '\n' >> "$work/notices"
	done < "$work/files"
}

printf 'Third-party notices for SSHDock\n\nSSHDock itself is covered by the accompanying LICENSE.\n' > "$work/notices"
goroot="$(go env GOROOT)"
goversion="$(go env GOVERSION)"
append_notices "Go toolchain $goversion" "$goroot"

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
(
	cd "$root"
	go list -deps -f '{{with .Module}}{{.Path}}{{"\t"}}{{if .Version}}{{.Version}}{{else}}(devel){{end}}{{"\t"}}{{if .Replace}}{{.Replace.Dir}}{{else}}{{.Dir}}{{end}}{{"\t"}}{{.Main}}{{end}}' ./cmd/sshdock ./cmd/sshdockd
) > "$work/modules"
sort -u "$work/modules" -o "$work/modules"
while IFS=$'\t' read -r module version directory main; do
	if [ -n "$module" ] && [ "$main" != true ]; then
		append_notices "$module $version" "$directory"
	fi
done < "$work/modules"

cp "$work/notices" "$output"
