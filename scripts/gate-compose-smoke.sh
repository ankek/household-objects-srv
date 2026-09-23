#!/usr/bin/env bash

source "$(dirname "$0")/_common.sh"
[ -f compose.yaml ] || [ -f docker-compose.yml ] || { echo "RESULT: fail: no compose file yet — NFR-026 gate cannot run (do not treat as pass)"; exit 2; }
have docker || { echo "RESULT: fail: docker not available"; exit 1; }
DC="docker compose"; $DC version >/dev/null 2>&1 || DC="docker-compose"

cleanup() { $DC down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

wait_healthy() {
  for _ in $(seq 1 60); do
    st=$($DC ps --format json 2>/dev/null | grep -o '"Health":"[a-z]*"' | head -1 | cut -d'"' -f4)
    [ "$st" = "healthy" ] && return 0
    sleep 1
  done
  return 1
}

run "compose-up" $DC up -d || { finish; }

wait_healthy && RAN+=("healthcheck") || FAILED+=("healthcheck: not healthy within 60s")

PORT="${HHO_PORT:-7745}"
run "smoke-request" bash -c "curl -fsS http://localhost:${PORT}/api/v1/status >/dev/null" || true

VOL=$(docker volume ls -q --filter name=hho-data | head -1)
if [ -z "$VOL" ]; then
  FAILED+=("data-layout: no hho-data volume found")
else

  EXPECTED_OWNER="65532:65532"
  run "data-layout" docker run --rm -e EXPECTED_OWNER="$EXPECTED_OWNER" -v "$VOL":/data busybox sh -c '
    set -e
    for d in /data/db /data/attachments /data/backups /data/tmp; do
      [ -d "$d" ] || { echo "missing: $d"; exit 1; }
      owner=$(stat -c "%u:%g" "$d")
      echo "$d -> owner $owner"
      [ "$owner" = "$EXPECTED_OWNER" ] || { echo "wrong ownership: $d is $owner, expected $EXPECTED_OWNER"; exit 1; }
    done
  ' || true
fi

#
CANARY_TOKEN="gate-canary-$$-$(date +%s%N)"
docker run --rm -v "$VOL":/data busybox sh -c "echo '$CANARY_TOKEN' > /data/db/.gate-canary" >/dev/null 2>&1
SCHEMA_BEFORE=$(curl -fsS "http://localhost:${PORT}/api/v1/status" 2>/dev/null | grep -o '"schema_version":[0-9]*' | head -1)
echo "--- restart-over-existing-volume: baseline before restart — canary planted, $SCHEMA_BEFORE"

if [ -z "$SCHEMA_BEFORE" ]; then
  FAILED+=("restart-over-existing-volume: could not capture pre-restart baseline (schema='$SCHEMA_BEFORE')")
else
  # A PLAIN down: no `-v`. The volume and its contents must survive exactly like an operator's
  # `docker compose down` (no flag) or a host reboot would — only `down -v`, exercised separately
  # below, is destructive.
  $DC down >/dev/null 2>&1
  VOL_SURVIVED=$(docker volume ls -q --filter name=hho-data | head -1)
  if [ "$VOL_SURVIVED" != "$VOL" ]; then
    FAILED+=("restart-over-existing-volume: volume did not survive a plain 'down' (expected $VOL, got '${VOL_SURVIVED:-<none>}')")
  else
    $DC up -d >/dev/null 2>&1
    if ! wait_healthy; then
      FAILED+=("restart-over-existing-volume: second 'up' over the pre-existing volume did not become healthy within 60s")
    else
      HHO_DB_PRESENT=false
      docker run --rm -v "$VOL":/data busybox test -f /data/db/hho.db && HHO_DB_PRESENT=true
      CANARY_AFTER=$(docker run --rm -v "$VOL":/data busybox cat /data/db/.gate-canary 2>/dev/null || echo "")
      SCHEMA_AFTER=$(curl -fsS "http://localhost:${PORT}/api/v1/status" 2>/dev/null | grep -o '"schema_version":[0-9]*' | head -1)
      echo "--- restart-over-existing-volume: after restart — hho.db present=$HHO_DB_PRESENT canary='$CANARY_AFTER' $SCHEMA_AFTER"
      if $HHO_DB_PRESENT && [ "$CANARY_AFTER" = "$CANARY_TOKEN" ] && [ "$SCHEMA_AFTER" = "$SCHEMA_BEFORE" ]; then
        RAN+=("restart-over-existing-volume")
      else
        FAILED+=("restart-over-existing-volume: data did not survive the restart — hho.db present=$HHO_DB_PRESENT, canary expected='$CANARY_TOKEN' got='$CANARY_AFTER', schema before='$SCHEMA_BEFORE' after='$SCHEMA_AFTER'")
      fi
    fi
  fi
fi

$DC down -v >/dev/null 2>&1
VOL_AFTER=$(docker volume ls -q --filter name=hho-data | head -1)
if [ -z "$VOL_AFTER" ]; then
  RAN+=("down-v-removes-volume")
  echo "--- down-v-removes-volume: confirmed no hho-data volume remains"
else
  FAILED+=("down-v-removes-volume: volume '$VOL_AFTER' still exists after 'down -v'")
fi

finish
