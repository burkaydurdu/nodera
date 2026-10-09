#!/usr/bin/env bash
# Runs as root on a fresh Ubuntu 24.04 server and turns it into a Region's
# control plane fleet (docs/adr/0013): a kubeadm management cluster with Cilium,
# whose node IPAM gives every Control plane's LoadBalancer Service the server's
# address on the Cluster's own ports (docs/adr/0015), cert-manager and Kamaji,
# and the account the kubernetes service drives it with. Every step is
# idempotent, so a run that failed half-way is finished by the next.
#
# Environment: PUBLIC_IP, REGION, ENDPOINT_DOMAIN (a Cluster answers at
# <id>.<REGION>.<ENDPOINT_DOMAIN>:<its port>). The versions below are pinned on
# purpose: a fleet moves to another only after `make test-kamaji-contract`
# passed on it.
set -euo pipefail

: "${PUBLIC_IP:?}" "${REGION:?}" "${ENDPOINT_DOMAIN:?}"

KUBERNETES_MINOR=1.36
KUBERNETES_VERSION=1.36.5
HELM_VERSION=v3.22.0
CILIUM_VERSION=1.20.2
LOCAL_PATH_VERSION=v0.0.37
CERT_MANAGER_VERSION=v1.21.2
KAMAJI_TAG=26.10.2-edge

# The management cluster's own API server answers on a port no Cluster is
# given, apart from the Endpoint ports the kubernetes service hands out.
MANAGEMENT_API_PORT=16443
POD_CIDR=10.244.0.0/16
NAMESPACE="nodera-${REGION}"

export DEBIAN_FRONTEND=noninteractive
export KUBECONFIG=/etc/kubernetes/admin.conf
step() { printf '\n==> %s\n' "$*"; }

step "swap off, kernel modules and sysctls"
# The kubelet refuses to run with swap on; Cherry's bare-metal images ship a
# swap file, and the fstab line would bring it back at the next boot.
swapoff -a
sed -i '/^[^#].*\sswap\s/ s/^/#/' /etc/fstab
printf 'overlay\nbr_netfilter\n' > /etc/modules-load.d/k8s.conf
modprobe overlay && modprobe br_netfilter
printf 'net.bridge.bridge-nf-call-iptables = 1\nnet.bridge.bridge-nf-call-ip6tables = 1\nnet.ipv4.ip_forward = 1\n' > /etc/sysctl.d/k8s.conf
sysctl --system >/dev/null

step "containerd and the Kubernetes ${KUBERNETES_VERSION} packages"
apt-get update -q
apt-get install -y -q apt-transport-https ca-certificates curl gpg containerd jq
if ! grep -q 'SystemdCgroup = true' /etc/containerd/config.toml 2>/dev/null; then
  mkdir -p /etc/containerd
  containerd config default > /etc/containerd/config.toml
  sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
  systemctl restart containerd
fi
mkdir -p /etc/apt/keyrings
curl -fsSL "https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_MINOR}/deb/Release.key" | gpg --dearmor --yes -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${KUBERNETES_MINOR}/deb/ /" > /etc/apt/sources.list.d/kubernetes.list
apt-get update -q
apt-get install -y -q -o Dpkg::Options::=--force-confold --allow-change-held-packages \
  "kubelet=${KUBERNETES_VERSION}-*" "kubeadm=${KUBERNETES_VERSION}-*" "kubectl=${KUBERNETES_VERSION}-*"
apt-mark hold kubelet kubeadm kubectl >/dev/null
systemctl enable --now kubelet

step "the management cluster"
if [ ! -f /etc/kubernetes/admin.conf ]; then
  # kube-proxy is left out: Cilium replaces it.
  kubeadm init \
    --kubernetes-version "v${KUBERNETES_VERSION}" \
    --apiserver-bind-port "${MANAGEMENT_API_PORT}" \
    --apiserver-cert-extra-sans "${PUBLIC_IP}" \
    --control-plane-endpoint "${PUBLIC_IP}:${MANAGEMENT_API_PORT}" \
    --pod-network-cidr "${POD_CIDR}" \
    --skip-phases addon/kube-proxy
fi

step "helm ${HELM_VERSION}"
if ! helm version --short 2>/dev/null | grep -q "${HELM_VERSION}"; then
  curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" | tar -xz -C /tmp
  install /tmp/linux-amd64/helm /usr/local/bin/helm
fi
helm repo add cilium https://helm.cilium.io >/dev/null 2>&1 || true
helm repo add jetstack https://charts.jetstack.io >/dev/null 2>&1 || true
helm repo add clastix https://clastix.github.io/charts >/dev/null 2>&1 || true
helm repo update >/dev/null

step "cilium ${CILIUM_VERSION}"
# Node IPAM hands the server's own address to every LoadBalancer Service, so
# each Control plane answers on the fleet's public IP at its own ports without
# a load balancer in front. A fleet of three servers puts the Region's one
# address in an LB IPAM pool instead; Kamaji's Services carry the sharing key.
helm upgrade --install cilium cilium/cilium --version "${CILIUM_VERSION}" --namespace kube-system \
  --set kubeProxyReplacement=true \
  --set k8sServiceHost="${PUBLIC_IP}" --set k8sServicePort="${MANAGEMENT_API_PORT}" \
  --set ipam.mode=kubernetes \
  --set nodeIPAM.enabled=true \
  --set defaultLBServiceIPAM=nodeipam \
  --set operator.replicas=1 \
  --wait --timeout 10m

step "a single server schedules workloads on its control plane"
kubectl taint nodes --all node-role.kubernetes.io/control-plane- 2>/dev/null || true
# kubeadm keeps control-plane nodes out of load balancers; on one server the
# Control planes' address is that node's, so Cilium's node IPAM must be allowed it.
kubectl label nodes --all node.kubernetes.io/exclude-from-external-load-balancers- 2>/dev/null || true

step "local-path-provisioner ${LOCAL_PATH_VERSION}, the datastore's storage"
kubectl apply -f "https://raw.githubusercontent.com/rancher/local-path-provisioner/${LOCAL_PATH_VERSION}/deploy/local-path-storage.yaml"
kubectl patch storageclass local-path -p '{"metadata":{"annotations":{"storageclass.kubernetes.io/is-default-class":"true"}}}'

step "cert-manager ${CERT_MANAGER_VERSION}, for Kamaji's webhooks"
helm upgrade --install cert-manager jetstack/cert-manager --version "${CERT_MANAGER_VERSION}" \
  --namespace cert-manager --create-namespace --set crds.enabled=true --wait --timeout 10m

step "kamaji ${KAMAJI_TAG} and its default etcd datastore"
helm upgrade --install kamaji clastix/kamaji --version 0.0.0+latest \
  --namespace kamaji-system --create-namespace \
  --set image.tag="${KAMAJI_TAG}" \
  --wait --timeout 15m

step "the account the kubernetes service drives the fleet with"
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f - <<YAML
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nodera-kubernetes
  namespace: ${NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: nodera-kubernetes
  namespace: ${NAMESPACE}
rules:
  - apiGroups: ["kamaji.clastix.io"]
    resources: ["tenantcontrolplanes"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: nodera-kubernetes
  namespace: ${NAMESPACE}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: nodera-kubernetes
subjects:
  - kind: ServiceAccount
    name: nodera-kubernetes
    namespace: ${NAMESPACE}
---
apiVersion: v1
kind: Secret
metadata:
  name: nodera-kubernetes-token
  namespace: ${NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: nodera-kubernetes
type: kubernetes.io/service-account-token
YAML

for _ in $(seq 1 30); do
  kubectl -n "${NAMESPACE}" get secret nodera-kubernetes-token -o jsonpath='{.data.token}' 2>/dev/null | grep -q . && break
  sleep 2
done

step "done"
jq -n \
  --arg api_server "https://${PUBLIC_IP}:${MANAGEMENT_API_PORT}" \
  --arg token "$(kubectl -n "${NAMESPACE}" get secret nodera-kubernetes-token -o jsonpath='{.data.token}' | base64 -d)" \
  --arg ca_certificate "$(cat /etc/kubernetes/pki/ca.crt)" \
  --arg namespace "${NAMESPACE}" \
  --arg endpoint_domain "${ENDPOINT_DOMAIN}" \
  '{api_server: $api_server, token: $token, ca_certificate: $ca_certificate, namespace: $namespace,
    datastore: "default", endpoint_domain: $endpoint_domain}' > /root/nodera-fleet.json
chmod 600 /root/nodera-fleet.json
echo "fleet ready: /root/nodera-fleet.json"
