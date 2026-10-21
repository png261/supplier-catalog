#!/usr/bin/env bash
# ==============================================================================
# Ingestion Utility Script for Milestone 6 Sample Data Fixtures
# ==============================================================================
# Usage:
#   ./scripts/ingest-fixtures.sh [API_URL] [FIXTURES_DIR]
# Defaults:
#   API_URL: http://localhost:8080
#   FIXTURES_DIR: mock_data
# ==============================================================================

set -euo pipefail

API_URL="${1:-${API_URL:-http://localhost:8080}}"
FIXTURES_DIR="${2:-${FIXTURES_DIR:-mock_data}}"

# Terminal color codes for clear visual demonstration
BOLD='\033[1m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BOLD}${BLUE}==============================================================================${NC}"
echo -e "${BOLD}${BLUE}  Coderpush Resilient Catalog Sync — Fixture Ingestion Demonstration  ${NC}"
echo -e "${BOLD}${BLUE}==============================================================================${NC}"
echo -e "Target Server:  ${BOLD}${API_URL}${NC}"
echo -e "Fixtures Dir:   ${BOLD}${FIXTURES_DIR}${NC}"
echo ""

# 1. Health check to ensure server is listening
echo -n "Checking server availability... "
if ! curl -s -f -o /dev/null --connect-timeout 2 "${API_URL}/api/jobs"; then
  echo -e "${RED}[FAILED]${NC}"
  echo -e "${RED}Error: Cannot connect to server at ${API_URL}.${NC}"
  echo -e "Please start the server first via: ${BOLD}make run-server${NC} or ${BOLD}go run ./cmd/server${NC}"
  exit 1
fi
echo -e "${GREEN}[OK]${NC}\n"

# Helper function to submit a single batch file
ingest_batch_file() {
  local batch_file="$1"
  local step_num="$2"
  local step_name="$3"
  local invariant_desc="$4"

  echo -e "${BOLD}${YELLOW}------------------------------------------------------------------------------${NC}"
  echo -e "${BOLD}Step ${step_num}: ${step_name}${NC}"
  echo -e "Invariant:   ${BLUE}${invariant_desc}${NC}"
  echo -e "Batch File:  ${batch_file}"

  if [ ! -f "${batch_file}" ]; then
    echo -e "${RED}Error: Fixture file ${batch_file} not found!${NC}"
    exit 1
  fi

  # Build JSON payload using python3 or jq (falling back to simple string embedding)
  local job_id="job-fixture-step-${step_num}-$(date +%s)"
  local payload=""

  if command -v python3 >/dev/null 2>&1; then
    payload=$(python3 -c "
import json, sys
with open(sys.argv[1]) as f:
    batch = json.load(f)
batch['batch_index'] = 0
req = {
    'mode': 'batches',
    'job_id': sys.argv[2],
    'batches': [batch]
}
print(json.dumps(req))
" "${batch_file}" "${job_id}")
  elif command -v jq >/dev/null 2>&1; then
    payload=$(jq -c --arg jid "${job_id}" '{mode: "batches", job_id: $jid, batches: [(. * {batch_index: 0})]}' "${batch_file}")
  else
    local content
    content=$(cat "${batch_file}")
    payload="{\"mode\":\"batches\",\"job_id\":\"${job_id}\",\"batches\":[${content}]}"
  fi

  # Send POST /api/jobs request
  local response
  response=$(curl -s -w "\n%{http_code}" -X POST "${API_URL}/api/jobs" \
    -H "Content-Type: application/json" \
    -d "${payload}")

  local http_code
  http_code=$(echo "${response}" | tail -n1)
  local body
  body=$(echo "${response}" | sed '$d')

  if [ "${http_code}" -ge 200 ] && [ "${http_code}" -lt 300 ]; then
    echo -e "Status:      ${GREEN}HTTP ${http_code} ACCEPTED${NC}"
  else
    echo -e "Status:      ${RED}HTTP ${http_code} FAILED${NC}"
    echo -e "Response:    ${body}"
    exit 1
  fi

  # Wait for asynchronous job completion (up to 5s)
  local waited=0
  local job_status="RUNNING"
  local job_details=""
  while [ "${waited}" -lt 25 ]; do
    job_details=$(curl -s "${API_URL}/api/jobs/${job_id}" || true)
    if [ -n "${job_details}" ]; then
      job_status=$(echo "${job_details}" | python3 -c '
import json, sys
try:
    print(json.load(sys.stdin).get("job", {}).get("status", "UNKNOWN"))
except Exception:
    print("UNKNOWN")
' 2>/dev/null || true)
      if [ "${job_status}" = "COMPLETED" ] || [ "${job_status}" = "INTERRUPTED" ] || [ "${job_status}" = "FAILED" ]; then
        break
      fi
    fi
    sleep 0.2
    waited=$((waited + 1))
  done

  if [ "${job_status}" = "COMPLETED" ]; then
    local summary_str
    summary_str=$(echo "${job_details}" | python3 -c '
import json, sys
data = json.load(sys.stdin).get("job", {})
s = data.get("summary", {})
print("+Added: " + str(s.get("added",0)) + ", ~Updated: " + str(s.get("updated",0)) + ", -Withdrawn: " + str(s.get("withdrawn",0)) + ", ↷Skipped: " + str(s.get("skipped",0)) + ", ⚠DeadLetter: " + str(s.get("dead_letter",0)))
' 2>/dev/null || echo "")
    echo -e "Outcome:     ${GREEN}COMPLETED${NC} (${summary_str})"
  else
    echo -e "Outcome:     ${YELLOW}${job_status}${NC}"
  fi

  # Brief pause between batches for SSE event propagation and readability
  sleep 0.5
  echo ""
}

# --- Step 1: Baseline Ingestion ---
ingest_batch_file \
  "${FIXTURES_DIR}/batch_001_initial.json" \
  "1" \
  "Baseline Catalog Insertion (10 Products)" \
  "Standard Initial Ingestion (All ACTIVE, diverse prices)"

# --- Step 2: Updates, Idempotency & Tombstones ---
ingest_batch_file \
  "${FIXTURES_DIR}/batch_002_updates_and_idempotency.json" \
  "2" \
  "Updates, Idempotency Replay & Tombstones" \
  "Invariant A (Zero Drift Replay), Invariant C (Status -> WITHDRAWN), Valid Updates"

# --- Step 3: Fast-Forward Skips & Dead-Letter Isolation ---
ingest_batch_file \
  "${FIXTURES_DIR}/batch_003_anomalies_and_deadletter.json" \
  "3" \
  "Anomalies, Clock Skew Skips & Dead-Letter Quarantine" \
  "Invariant B (T_new < T_old Skipped), Invariant D (Negative Price & Empty SKU Quarantined, Valid Items Succeeded)"

# --- Step 4: Tombstone Reactivation Attempt ---
ingest_batch_file \
  "${FIXTURES_DIR}/batch_004_tombstone_reactivation_attempt.json" \
  "4" \
  "Tombstone Preservation Enforcement" \
  "Invariant C (WITHDRAWN Items Cannot Be Resurrected to ACTIVE Even With Newer Timestamp)"

echo -e "${BOLD}${GREEN}==============================================================================${NC}"
echo -e "${BOLD}${GREEN}  All 4 Batches Successfully Ingested! Verification Inspection Below:         ${NC}"
echo -e "${BOLD}${GREEN}==============================================================================${NC}\n"

echo -e "${BOLD}Current Catalog Products (Total Count & Sample):${NC}"
curl -s "${API_URL}/api/products?limit=15" | {
  if command -v jq >/dev/null 2>&1; then
    jq '{total: .total, sample_products: [.products[] | {sku: .sku, name: .name, price: .price, status: .status, supplier_timestamp: .supplier_timestamp}]}'
  else
    cat
  fi
}
echo ""

echo -e "${BOLD}Isolated Dead-Letter Issues Table (Invariant D):${NC}"
curl -s "${API_URL}/api/dead-letter?limit=10" | {
  if command -v jq >/dev/null 2>&1; then
    jq '{total_issues: .total, issues: [.issues[] | {issue_id: .issue_id, sku: .sku, reason: .reason, batch_index: .batch_index}]}'
  else
    cat
  fi
}
echo ""

echo -e "${BOLD}${BLUE}Demonstration Complete! View Real-Time Dashboard at: ${BOLD}${API_URL}${NC}\n"
