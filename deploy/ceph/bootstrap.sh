#!/bin/bash
#
# Starts a whole Ceph cluster in one container from the official image
# (docs/adr/0012): a monitor, a manager, one OSD and the RADOS Gateway the
# storage service talks to. Development (docker-compose) and the contract test
# (contexts/storage/engine/ceph/contract_test.go) both run this file, so what
# the adapter relies on is set in one place.
#
# Environment:
#   CEPH_DATA_DIR      where the cluster keeps its state (default /var/lib/nodera-ceph)
#   CEPH_OBJECTSTORE   memstore (default; data lives in RAM and the cluster is rebuilt
#                      on every start) or bluestore (kept in CEPH_DATA_DIR across starts)
#   RGW_PORT           the port the gateway listens on inside the container (default 7480)
#   RGW_ADMIN_ACCESS_KEY / RGW_ADMIN_SECRET_KEY
#                      the admin user's S3 key the storage service signs admin calls with
#   RGW_DNS_NAME       set to serve virtual-hosted buckets (<bucket>.<name>); unset, buckets
#                      are reached by path only, which is what development uses
set -euo pipefail

DATA_DIR="${CEPH_DATA_DIR:-/var/lib/nodera-ceph}"
OBJECTSTORE="${CEPH_OBJECTSTORE:-memstore}"
RGW_PORT="${RGW_PORT:-7480}"
ADMIN_ACCESS_KEY="${RGW_ADMIN_ACCESS_KEY:-nodera-admin}"
ADMIN_SECRET_KEY="${RGW_ADMIN_SECRET_KEY:-nodera-admin-secret}"
RGW_DNS_NAME="${RGW_DNS_NAME:-}"

MON_ID="a"
MGR_ID="x"
RGW_ID="nodera"
export CEPH_CONF="${DATA_DIR}/ceph.conf"

# memstore forgets everything with the OSD process, so a monitor map or a
# bucket index left from an earlier run would describe data that is gone: the
# cluster is rebuilt from nothing every time. bluestore keeps its state, so
# a second start only brings the daemons back.
fresh=true
if [ "${OBJECTSTORE}" = "bluestore" ] && [ -f "${DATA_DIR}/.bootstrapped" ]; then
	fresh=false
fi

if [ "${fresh}" = true ]; then
	rm -rf "${DATA_DIR:?}"/*
	mkdir -p "${DATA_DIR}/mon" "${DATA_DIR}/osd" "${DATA_DIR}/run" "${DATA_DIR}/log"

	# Everything listens on loopback: the daemons share one network namespace
	# and a container's address changes between starts, while 127.0.0.1 does
	# not, so a bluestore cluster comes back after a restart.
	cat > "${CEPH_CONF}" <<EOF
[global]
fsid = $(uuidgen)
mon host = 127.0.0.1
public addr = 127.0.0.1
cluster addr = 127.0.0.1
run dir = ${DATA_DIR}/run
log file = ${DATA_DIR}/log/\$name.log
# One container holds no second party to authenticate; cephx would only cost
# key management here.
auth cluster required = none
auth service required = none
auth client required = none
# Without cephx there is no key to encrypt with, and a daemon offered only the
# "secure" mode hangs at its first connection instead of falling back.
ms mon client mode = crc
ms mon cluster mode = crc
ms mon service mode = crc
ms client mode = crc
ms cluster mode = crc
ms service mode = crc
# One OSD: a single copy of each object, and the monitor must not warn about it.
osd pool default size = 1
osd pool default min size = 1
mon allow pool size one = true
mon warn on pool no redundancy = false
osd crush chooseleaf type = 0
# The gateway makes eight pools; at the default 32 PGs each they would pass
# the 250 one OSD may hold, so pools are small and stay small.
osd pool default pg num = 8
osd pool default pgp num = 8
osd pool default pg autoscale mode = off
# A laptop's Docker disk is often fuller than a server's; the monitor must not
# refuse to run, or warn, over it.
mon data avail crit = 1
mon data avail warn = 2
mon allow pool delete = true
# One OSD never recovers from another; deep scrubs only burn a laptop's CPU.
osd scrub load threshold = 0
osd deep scrub interval = 31536000

[mon.${MON_ID}]
mon data = ${DATA_DIR}/mon

[osd.0]
osd data = ${DATA_DIR}/osd
osd objectstore = ${OBJECTSTORE}
# memstore holds the whole store in RAM: this is the most the cluster may
# take; writes past it fail with ENOSPC.
memstore device bytes = $((4 * 1024 * 1024 * 1024))
# bluestore in a container has no disk of its own: a sparse file stands in,
# and the OSD's cache is held to the least it accepts rather than 4 GB.
bluestore block create = true
bluestore block size = $((20 * 1024 * 1024 * 1024))
osd memory target = 939524096

[client.rgw.${RGW_ID}]
rgw frontends = beast port=${RGW_PORT}
# Storage sells S3 only (docs/adr/0012): no Swift, no static website hosting,
# and the IAM API is what the adapter makes Access keys with.
rgw enable apis = s3, admin, iam
# Traffic per Storage is read from the usage log. Entries are flushed every
# second so the contract test (and a developer) sees a request within the
# same minute; production can keep the 30 s default.
rgw enable usage log = true
rgw usage log tick interval = 1
rgw usage log flush threshold = 1
rgw usage max shards = 32
rgw usage max user shards = 1
# Quotas are enforced from cached statistics. With no cache, a Storage that
# is full refuses the very next write instead of one minutes later.
rgw bucket quota ttl = 0
rgw user quota bucket sync interval = 0
rgw user quota sync interval = 0
EOF
	if [ -n "${RGW_DNS_NAME}" ]; then
		echo "rgw dns name = ${RGW_DNS_NAME}" >> "${CEPH_CONF}"
	fi

	ceph-mon --id "${MON_ID}" --mkfs --keyring /dev/null
	touch "${DATA_DIR}/mon/keyring"
	ceph-mon --id "${MON_ID}"
	ceph-mgr --id "${MGR_ID}"

	osd_id=$(ceph osd create)
	ceph osd crush add "osd.${osd_id}" 1 root=default
	ceph-osd --id "${osd_id}" --mkfs
	ceph-osd --id "${osd_id}"

	until ceph osd stat | grep -q '1 up'; do sleep 1; done

	# radosgw-admin reaches RADOS directly and makes the gateway's pools, so
	# the admin user exists before the gateway takes its first request. Its
	# capabilities are what the adapter uses and nothing more.
	radosgw-admin user create --uid admin --display-name "Nodera storage service" \
		--access-key "${ADMIN_ACCESS_KEY}" --secret-key "${ADMIN_SECRET_KEY}" \
		--caps "accounts=*;users=*;buckets=*;usage=read;ratelimit=*" >/dev/null

	touch "${DATA_DIR}/.bootstrapped"
else
	ceph-mon --id "${MON_ID}"
	ceph-mgr --id "${MGR_ID}"
	ceph-osd --id 0
	until ceph osd stat | grep -q '1 up'; do sleep 1; done
fi

echo "nodera ceph: ready, gateway on port ${RGW_PORT}"
exec radosgw -f -n "client.rgw.${RGW_ID}"
