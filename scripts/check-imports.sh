#!/usr/bin/env bash
#
# Guards the zero-cycle invariant documented in PROJECT_RULES.md: pkg depends
# only on the standard library and the third-party modules declared in go.mod,
# never on an application or business module.
set -euo pipefail

self=$(go list -m)
allowed=$(go list -m -f '{{.Path}}' all | LC_ALL=C sort)
used=$(go list -deps -f '{{with .Module}}{{.Path}}{{end}}' ./... | LC_ALL=C sort -u | sed '/^$/d')

status=0

undeclared=$(LC_ALL=C comm -23 <(printf '%s\n' "$used") <(printf '%s\n' "$allowed"))
if [ -n "$undeclared" ]; then
	echo "error: imported but not declared in go.mod:" >&2
	printf '%s\n' "$undeclared" | sed 's/^/  /' >&2
	status=1
fi

siblings=$(printf '%s\n' "$used" | grep -E '^github\.com/Tangerg/' | grep -v -x -F "$self" || true)
if [ -n "$siblings" ]; then
	echo "error: pkg must not import business modules:" >&2
	printf '%s\n' "$siblings" | sed 's/^/  /' >&2
	status=1
fi

if [ "$status" -eq 0 ]; then
	echo "ok: dependencies are the standard library, ${self}, and go.mod modules"
fi
exit "$status"
