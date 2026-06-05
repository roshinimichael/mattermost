#!/usr/bin/env bash
# Authenticates as the sysadmin user and prints the Mattermost Bearer token to stdout.
# Testbot captures this output and uses it as the Authorization header for API tests.
set -euo pipefail

MM_URL="${MM_BASE_URL:-http://localhost:8065}"

RESPONSE=$(curl -sf -i -X POST "${MM_URL}/api/v4/users/login" \
    -H "Content-Type: application/json" \
    -d '{"login_id":"sysadmin","password":"Sys@dmin-sample1"}' 2>&1) || {
    echo "ERROR: Login request to ${MM_URL}/api/v4/users/login failed" >&2
    exit 1
}

TOKEN=$(echo "$RESPONSE" | grep -i '^Token:' | awk '{print $2}' | tr -d '\r\n')
if [ -z "$TOKEN" ]; then
    echo "ERROR: Could not extract Token from login response" >&2
    echo "Response headers:" >&2
    echo "$RESPONSE" | head -20 >&2
    exit 1
fi

echo "$TOKEN"
