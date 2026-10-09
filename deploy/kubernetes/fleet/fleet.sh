#!/usr/bin/env bash
# The operator's command for a Region's control plane fleet (docs/adr/0013):
#
#   fleet.sh up        order the server from Cherry, install the fleet, write the config
#   fleet.sh order     order the server and wait until it answers over SSH
#   fleet.sh install   install or finish installing the fleet on it (idempotent)
#   fleet.sh config    write what the kubernetes service needs into its dev.toml
#   fleet.sh status    the server and the fleet's components
#   fleet.sh destroy   delete the server at Cherry
#
# Cherry credentials come from CHERRY_TOKEN and CHERRY_PROJECT_ID, or, in
# development, from cmd/droplet/.conf/dev.toml. The server's id and address are
# kept in .local/kubernetes-fleet-<region>.json; the fleet's credentials only in
# the kubernetes service's dev.toml (gitignored) or the environment.
set -euo pipefail

cd "$(dirname "$0")/../../.."

REGION=${FLEET_REGION:-vno1}
CHERRY_REGION=${FLEET_CHERRY_REGION:-LT-Siauliai}
PLAN=${FLEET_PLAN:-G1-4-16gb-100nv-ded}
IMAGE=${FLEET_IMAGE:-ubuntu_24_04_64bit}
SSH_KEY=${FLEET_SSH_KEY:-$HOME/.ssh/nodera_ops_ed25519}
STATE=.local/kubernetes-fleet-${REGION}.json
KNOWN_HOSTS=.local/kubernetes-fleet-known_hosts
DEV_TOML=cmd/kubernetes/.conf/dev.toml
API=https://api.cherryservers.com/v1

droplet_dev() { sed -n "/^\[providers.cherry\]/,/^\[/p" cmd/droplet/.conf/dev.toml 2>/dev/null | grep "^$1" | sed 's/^[^=]*= *//; s/"//g'; }
CHERRY_TOKEN=${CHERRY_TOKEN:-$(droplet_dev token)}
CHERRY_PROJECT_ID=${CHERRY_PROJECT_ID:-$(droplet_dev project_id)}
: "${CHERRY_TOKEN:?set CHERRY_TOKEN}" "${CHERRY_PROJECT_ID:?set CHERRY_PROJECT_ID}"

cherry() { curl -sS -m 60 -H "Authorization: Bearer ${CHERRY_TOKEN}" -H "Content-Type: application/json" "$@"; }
state() { python3 -c "import json,sys; print(json.load(open('$STATE'))[sys.argv[1]])" "$1"; }
remote() { ssh -i "$SSH_KEY" -o BatchMode=yes -o ConnectTimeout=10 -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="$KNOWN_HOSTS" "root@$(state ip)" "$@"; }
say() { printf '==> %s\n' "$*"; }

# The Cherry key holding the operational key's material, uploaded once.
ssh_key_id() {
  local material
  material=$(awk '{print $1" "$2}' "${SSH_KEY}.pub")
  local id
  id=$(cherry "$API/ssh-keys" | python3 -c '
import json, sys
material = sys.argv[1]
for k in json.load(sys.stdin):
    if " ".join(k["key"].split()[:2]) == material:
        print(k["id"]); break
' "$material")
  if [ -z "$id" ]; then
    id=$(cherry -X POST "$API/ssh-keys" -d "$(python3 -c 'import json,sys; print(json.dumps({"label": "nodera-ops-fleet", "key": open(sys.argv[1]).read().strip()}))' "${SSH_KEY}.pub")" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
  fi
  echo "$id"
}

order() {
  if [ -f "$STATE" ]; then
    say "fleet server already ordered: $(state id) at $(state ip)"
  else
    say "ordering ${PLAN} in ${CHERRY_REGION} for the ${REGION} fleet"
    local key body response id
    key=$(ssh_key_id)
    body=$(python3 -c 'import json,sys; print(json.dumps({"plan": sys.argv[1], "region": sys.argv[2], "image": sys.argv[3], "hostname": "nodera-fleet-" + sys.argv[4], "ssh_keys": [int(sys.argv[5])], "cycle": "hourly", "tags": {"role": "nodera-kubernetes-fleet", "region": sys.argv[4]}}))' "$PLAN" "$CHERRY_REGION" "$IMAGE" "$REGION" "$key")
    response=$(cherry -X POST "$API/projects/${CHERRY_PROJECT_ID}/servers" -d "$body")
    id=$(echo "$response" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("id") or "")')
    if [ -z "$id" ]; then echo "$response" >&2; exit 1; fi
    mkdir -p .local
    printf '{"id": %s, "ip": ""}\n' "$id" > "$STATE"
    say "ordered server ${id}"
  fi

  say "waiting for the server to be deployed"
  local ip=""
  for _ in $(seq 1 120); do
    read -r status ip < <(cherry "$API/servers/$(state id)" | python3 -c '
import json, sys
d = json.load(sys.stdin)
ip = next((a["address"] for a in d.get("ip_addresses", []) if a.get("type") == "primary-ip" and a.get("address_family") == 4), "")
print(d.get("status", ""), ip or "-")')
    [ "$status" = "deployed" ] && [ "$ip" != "-" ] && break
    sleep 15
  done
  [ "$status" = "deployed" ] || { echo "the server was not deployed in time" >&2; exit 1; }
  printf '{"id": %s, "ip": "%s"}\n' "$(state id)" "$ip" > "$STATE"

  say "waiting for SSH on ${ip}"
  for _ in $(seq 1 60); do remote true 2>/dev/null && break; sleep 10; done
  remote true
  say "server ${ip} answers"
}

install() {
  local ip domain
  ip=$(state ip)
  domain=${FLEET_ENDPOINT_DOMAIN:-$(echo "$ip" | tr . -).sslip.io}
  say "installing the fleet on ${ip}; Clusters answer at <id>.${REGION}.${domain}"
  remote "PUBLIC_IP=${ip} REGION=${REGION} ENDPOINT_DOMAIN=${domain} bash -s" < deploy/kubernetes/fleet/install.sh
}

config() {
  say "writing the fleet into ${DEV_TOML}"
  remote cat /root/nodera-fleet.json > .local/kubernetes-fleet-${REGION}-config.json
  chmod 600 .local/kubernetes-fleet-${REGION}-config.json
  python3 - "$DEV_TOML" ".local/kubernetes-fleet-${REGION}-config.json" "$REGION" <<'PY'
import json, re, sys
path, fleet_path, region = sys.argv[1], sys.argv[2], sys.argv[3]
fleet = json.load(open(fleet_path))
text = open(path).read()

def toml(value):
    if isinstance(value, int):
        return str(value)
    if "\n" in value:
        return '"""\n' + value.strip() + '\n"""'
    return json.dumps(value)

def set_key(section, key, value):
    global text
    block = re.search(r"(?ms)^\[" + re.escape(section) + r"\]\n(.*?)(?=^\[|\Z)", text)
    if not block:
        sys.exit(f"no [{section}] in {path}")
    body = block.group(1)
    line = re.compile(r'(?ms)^' + re.escape(key) + r' = ("""\n.*?\n"""|[^\n]*)$')
    if not line.search(body):
        sys.exit(f"no {key} in [{section}] of {path}")
    body = line.sub(lambda _: f"{key} = {toml(value)}", body, count=1)
    text = text[:block.start(1)] + body + text[block.end(1):]

set_key("engine", "kind", "kamaji")
set_key("engine", "region", region)
set_key("engine", "endpoint_domain", fleet["endpoint_domain"])
for key in ("api_server", "token", "ca_certificate", "namespace", "datastore"):
    set_key("engine.kamaji", key, fleet[key])
open(path, "w").write(text)
PY
  say "restart the kubernetes server and worker to take it: make stop && make start"
}

status() {
  [ -f "$STATE" ] || { echo "no fleet ordered for ${REGION}"; exit 0; }
  cherry "$API/servers/$(state id)?fields=id,hostname,state,status" ; echo
  remote "KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes -o wide; KUBECONFIG=/etc/kubernetes/admin.conf kubectl get pods -A --no-headers | awk '{print \$1, \$2, \$4}' | column -t; KUBECONFIG=/etc/kubernetes/admin.conf kubectl get tcp,svc -n nodera-${REGION}" || true
}

destroy() {
  [ -f "$STATE" ] || { echo "no fleet ordered for ${REGION}"; exit 0; }
  say "deleting server $(state id) at Cherry"
  cherry -X DELETE "$API/servers/$(state id)" ; echo
  rm -f "$STATE" ".local/kubernetes-fleet-${REGION}-config.json"
}

case "${1:-}" in
  up) order; install; config ;;
  order) order ;;
  install) install ;;
  config) config ;;
  status) status ;;
  destroy) destroy ;;
  *) sed -n '2,10p' "$0"; exit 1 ;;
esac
