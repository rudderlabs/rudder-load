#!/usr/bin/env bash
# Fails if govulncheck finds a vulnerability reachable from this module's code, unless it is listed in IGNORED.
set -Eeuo pipefail

GOVULNCHECK="${GOVULNCHECK:-golang.org/x/vuln/cmd/govulncheck@v1.8.0}"

# Only vulnerabilities without a fixed version belong here, each with the reason it is accepted. Remove an entry as
# soon as a fix is available.
IGNORED=(
  # github.com/hamba/avro/v2, pulled in by pulsar-client-go: CPU exhaustion, integer overflow and unbounded map
  # allocations when decoding untrusted Avro. The module is unmaintained and has no fixed version. rudder-load never
  # uses Avro schemas: it produces raw payloads to brokers we operate.
  GO-2026-5046
  GO-2026-5047
  GO-2026-5048
)

# Symbol-level findings (trace[0].function set) are the ones reachable from our code; module- and package-level
# findings are not.
found=$(go run "$GOVULNCHECK" -format json ./... |
  jq -r 'select(.finding != null and .finding.trace[0].function != null) | .finding.osv' | sort -u)

unexpected=()
for id in $found; do
  if [[ " ${IGNORED[*]} " != *" $id "* ]]; then
    unexpected+=("$id")
  fi
done

for id in "${IGNORED[@]}"; do
  if [[ $'\n'"$found"$'\n' != *$'\n'"$id"$'\n'* ]]; then
    echo "warning: $id is ignored but no longer reported, remove it from IGNORED in $0" >&2
  fi
done

if ((${#unexpected[@]} > 0)); then
  echo "govulncheck found vulnerabilities reachable from this module: ${unexpected[*]}" >&2
  echo "Run 'go run $GOVULNCHECK ./...' for details." >&2
  exit 1
fi
echo "govulncheck: no unexpected vulnerabilities (ignored: ${IGNORED[*]})"
