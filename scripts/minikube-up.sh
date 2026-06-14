#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
# Bring up the local minikube cluster used by ckad-trainer.
#
# Installs minikube/kubectl hints if missing, starts the cluster, and enables the
# addons several scenarios need (metrics-server, ingress). Safe to re-run.
set -euo pipefail

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing: $1"
    echo "  install it first:"
    case "$1" in
      minikube) echo "  curl -fsSL https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 -o /usr/local/bin/minikube && chmod +x /usr/local/bin/minikube" ;;
      kubectl)  echo "  curl -fsSL https://dl.k8s.io/release/\$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl" ;;
    esac
    exit 1
  fi
}

need minikube
need kubectl

echo "==> starting minikube"
minikube start

echo "==> enabling addons (metrics-server, ingress)"
minikube addons enable metrics-server || echo "warn: metrics-server addon failed"
minikube addons enable ingress || echo "warn: ingress addon failed"

echo "==> minikube status"
minikube status

echo
echo "ready. point config.yaml at context 'minikube' and run: ckad-trainer doctor"
