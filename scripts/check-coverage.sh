#!/usr/bin/env bash
set -euo pipefail

# scripts/check-coverage.sh
# Verifies that total statement coverage in a Go coverprofile meets or exceeds a target threshold.
#
# Usage:
#   ./scripts/check-coverage.sh [profile_path] [min_threshold]
#   Example: ./scripts/check-coverage.sh coverage.out 90.0

PROFILE="${1:-coverage.out}"
THRESHOLD="${2:-90.0}"

if [ ! -f "$PROFILE" ]; then
    echo "❌ Error: Coverage profile '$PROFILE' does not exist." >&2
    echo "Generate it first using: go test -coverprofile=$PROFILE ./..." >&2
    exit 1
fi

TOTAL_LINE=$(go tool cover -func="$PROFILE" | grep "^total:" || true)
if [ -z "$TOTAL_LINE" ]; then
    echo "❌ Error: Could not parse total statements line from '$PROFILE'." >&2
    exit 1
fi

# Extract coverage percentage (e.g., '93.5%' -> '93.5')
COVERAGE=$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')

if [ -z "$COVERAGE" ]; then
    echo "❌ Error: Extracted empty coverage value from '$PROFILE'." >&2
    exit 1
fi

echo "=================================================="
echo " Coverage Gate Verification"
echo " Profile:   $PROFILE"
echo " Reported:  ${COVERAGE}%"
echo " Threshold: ${THRESHOLD}%"
echo "=================================================="

# Perform float comparison using awk (POSIX compliant, no bc dependency)
PASS=$(awk -v cov="$COVERAGE" -v min="$THRESHOLD" 'BEGIN { if (cov + 0.0 >= min + 0.0) print 1; else print 0 }')

if [ "$PASS" -eq 1 ]; then
    echo "✅ PASS: Statement coverage (${COVERAGE}%) meets or exceeds threshold (${THRESHOLD}%)."
    exit 0
else
    echo "❌ FAIL: Statement coverage (${COVERAGE}%) is BELOW required threshold (${THRESHOLD}%)." >&2
    exit 1
fi
