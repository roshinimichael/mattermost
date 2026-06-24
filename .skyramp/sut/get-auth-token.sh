#!/bin/bash
# Gets a Mattermost session token for use by Skyramp Testbot.
# Creates the sysadmin user on first run (idempotent).
set -e

COMPOSE_CMD="docker compose -f .skyramp/sut/docker-compose.testbot.yml --project-directory ."

# Create the sysadmin user via mmctl local mode (no-op if user already exists)
${COMPOSE_CMD} exec -T -w /home/mattermost-server/server server \
  bin/mmctl --local user create \
    --email sysadmin@sample.mattermost.com \
    --username sysadmin \
    --password "Sys@dmin-sample1" \
    --system-admin 2>/dev/null || true

# Obtain and print the session token
TOKEN=$(curl -si http://localhost:8065/api/v4/users/login \
  -H "Content-Type: application/json" \
  -d '{"login_id":"sysadmin","password":"Sys@dmin-sample1"}' \
  | grep -i "^token:" | awk '{print $2}' | tr -d '\r\n')

printf '%s\n' "${TOKEN}"
