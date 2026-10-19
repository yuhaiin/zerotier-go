#!/usr/bin/env bash
# This is a mandatory, real ZeroTier P2P link test. No fake network/UDP sockets.
set -euo pipefail
: "${ZEROTIER_GO_TEST_LIBRARY:?set path to real compiled native library}"
NAME="ztgo-ci-controller-${GITHUB_RUN_ID:-local}-$$"
DIR="$(mktemp -d -t ztgo-ci-controller-XXXXXXXX)"
START=$(date +%s)
cleanup() {
  docker logs "$NAME" >"$DIR/controller.log" 2>&1 || true
  mkdir -p integration/logs
  cp "$DIR/controller.log" integration/logs/controller.log || true
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  echo "Controller logs: $DIR/controller.log"
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    echo "Real ZeroTier controller and two Go nodes: $(($(date +%s)-START)) seconds" >>"$GITHUB_STEP_SUMMARY"
    echo "[Job logs](https://github.com/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID})" >>"$GITHUB_STEP_SUMMARY"
  fi
}
trap cleanup EXIT
# Pin official image at release tag and run the controller as its own daemon.
docker run -d --name "$NAME" --network host --cap-add NET_ADMIN --device /dev/net/tun \
 -v "$DIR:/var/lib/zerotier-one" zerotier/zerotier:1.14.2 >/dev/null
READY=0
for i in $(seq 1 50); do
 if docker exec "$NAME" zerotier-cli info >/dev/null 2>&1;then READY=1;break;fi
 sleep 1
done
if [[ "$READY" != 1 ]];then echo 'official controller failed to initialize';exit 1;fi
TOKEN="$(docker exec "$NAME" cat /var/lib/zerotier-one/authtoken.secret | tr -d '\r\n')"
ID="$(docker exec "$NAME" zerotier-cli info | awk '{print $3}' | tr -d '\r\n')"
[[ ${#ID} -eq 10 && -n $TOKEN ]]
BASE="http://127.0.0.1:9993"
RESP="$(curl --fail-with-body -sS -X POST "$BASE/controller/network/${ID}______" -H "X-ZT1-AUTH: $TOKEN" -H 'Content-Type: application/json' -d '{}')"
NWID="$(jq -r '.nwid // .id // empty' <<<"$RESP")"
[[ ${#NWID} -eq 16 ]] || { echo "network ID not returned: $RESP";exit 1; }
curl --fail-with-body -sS -X POST "$BASE/controller/network/$NWID" -H "X-ZT1-AUTH: $TOKEN" -H 'Content-Type: application/json' \
 -d '{"name":"ztgo-ci-interop","private":true,"enableBroadcast":true,"mtu":2800,"ipAssignmentPools":[{"ipRangeStart":"10.147.20.2","ipRangeEnd":"10.147.20.200"}],"routes":[{"target":"10.147.20.0/24","via":null}],"v4AssignMode":"zt"}' >/dev/null
export ZTGO_CI_NETWORK="$NWID" ZTGO_CI_TOKEN="$TOKEN" ZTGO_CI_CONTROLLER="$BASE"
echo "Official controller node=$ID network=$NWID, starting two real Go ZeroTier peers"
go test -v -count=1 -timeout=4m -tags=integration -run TestOfficialControllerTwoGoNodesPingReconnect ./integration
