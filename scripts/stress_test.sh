#!/usr/bin/env bash
# Stress test: fire N concurrent requests at POST /v1/chat/completions
#
# Usage:
#   ./scripts/stress_test.sh [API_KEY] [NUM_REQUESTS] [HOST]
#
# Defaults:
#   NUM_REQUESTS = 10
#   HOST         = http://localhost:8080

API_KEY="${1:-sk-GWALtxPQJ32kynNSX19OrIOTP6mTkKT6FxyES9aw79DWqW6s}"
NUM_REQUESTS="${2:-20}"
HOST="${3:-http://localhost:8081}"
ENDPOINT="$HOST/v1/chat/completions"

# Model name is ignored — routing is driven entirely by the API key's subscription (sub_id).
# The proxy will replace this with whatever model is configured for the subscription.
PAYLOAD='{
  "model": "ignored",
  "messages": [
    {"role": "user", "content": "Say hello in one word."}
  ],
  "max_tokens": 10
}'

echo "========================================"
echo "  Stress Test: $ENDPOINT"
echo "  Requests   : $NUM_REQUESTS (concurrent)"
echo "  Note       : Routing is subscription-driven, model field is overridden by proxy"
echo "========================================"
echo ""

# macOS-compatible millisecond timestamp
ms() { python3 -c "import time; print(int(time.time() * 1000))"; }

START=$(ms)

# Fire all requests concurrently.
# Each request writes: body to TMP_BODY, HTTP status code to TMP_CODE.
declare -a PIDS
declare -a BODY_FILES
declare -a CODE_FILES

for i in $(seq 1 "$NUM_REQUESTS"); do
  TMP_BODY=$(mktemp)
  TMP_CODE=$(mktemp)
  BODY_FILES+=("$TMP_BODY")
  CODE_FILES+=("$TMP_CODE")

  curl -s -o "$TMP_BODY" -w "%{http_code}" \
    -X POST "$ENDPOINT" \
    -H "Authorization: Bearer $API_KEY" \
    -H "Content-Type: application/json" \
    -d "$PAYLOAD" > "$TMP_CODE" &

  PIDS+=($!)
done

# Wait for all requests to finish
for pid in "${PIDS[@]}"; do
  wait "$pid"
done

END=$(ms)
ELAPSED=$(( END - START ))

echo ""
echo "========================================"
echo "  Results"
echo "========================================"

SUCCESS=0
FAIL=0

for i in "${!BODY_FILES[@]}"; do
  REQ_NUM=$((i + 1))
  HTTP_CODE=$(cat "${CODE_FILES[$i]}")
  BODY=$(cat "${BODY_FILES[$i]}")

  if [[ "$HTTP_CODE" == "200" ]]; then
    echo "  [OK]  Request $REQ_NUM → HTTP $HTTP_CODE"
    SUCCESS=$((SUCCESS + 1))
  else
    echo "  [ERR] Request $REQ_NUM → HTTP $HTTP_CODE | ${BODY:0:120}"
    FAIL=$((FAIL + 1))
  fi

  rm -f "${BODY_FILES[$i]}" "${CODE_FILES[$i]}"
done

echo ""
echo "  Total    : $NUM_REQUESTS"
echo "  Success  : $SUCCESS"
echo "  Failed   : $FAIL"
echo "  Elapsed  : ${ELAPSED}ms"
echo "========================================"
