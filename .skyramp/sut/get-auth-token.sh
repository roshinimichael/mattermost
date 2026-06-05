#!/bin/bash
# Obtain a Mattermost session token for the sysadmin account.
# On a fresh install (empty DB), creates the first admin user then logs in.
# On a subsequent run the create call fails silently and login succeeds directly.
set -euo pipefail

MM_BASE_URL="http://localhost:8065"
ADMIN_USER="sysadmin"
ADMIN_PASS="Sys@dmin-sample1"
ADMIN_EMAIL="sysadmin@sample.mattermost.com"

# Attempt to create the first admin user; benign if the user already exists.
curl -sf -X POST \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"${ADMIN_EMAIL}\",\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}" \
  "${MM_BASE_URL}/api/v4/users" >/dev/null 2>&1 || true

# Login and capture session token from response headers.
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -d "{\"login_id\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}" \
  -D /tmp/mm_auth_headers.txt \
  "${MM_BASE_URL}/api/v4/users/login")

if [ "${HTTP_STATUS}" != "200" ]; then
  echo "Error: Mattermost login returned HTTP ${HTTP_STATUS}" >&2
  cat /tmp/mm_auth_headers.txt >&2
  exit 1
fi

TOKEN=$(grep -i "^token:" /tmp/mm_auth_headers.txt | awk '{print $2}' | tr -d '\r\n')

if [ -z "${TOKEN}" ]; then
  echo "Error: No token found in login response headers" >&2
  exit 1
fi

echo "${TOKEN}"
