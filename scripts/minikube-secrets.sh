#!/usr/bin/env bash
set -euo pipefail

# Always use the intended Minikube profile instead of whichever context is current.
MINIKUBE_PROFILE="${MINIKUBE_PROFILE:-gpu-cpu-lab}"
NAMESPACE="${K8S_NAMESPACE:-tenant-control-plane}"
KUBECTL=(minikube -p "$MINIKUBE_PROFILE" kubectl --)

: "${DB_PASSWORD:?Set DB_PASSWORD (letters/numbers/_/- only)}"
: "${RABBITMQ_PASSWORD:?Set RABBITMQ_PASSWORD (letters/numbers/_/- only)}"
for name in DB_PASSWORD RABBITMQ_PASSWORD; do
  value="${!name}"
  if [[ ! "$value" =~ ^[A-Za-z0-9_-]+$ ]]; then
    printf '%s must use only letters, numbers, underscores or hyphens.\n' "$name" >&2
    exit 1
  fi
done

minikube -p "$MINIKUBE_PROFILE" status >/dev/null
"${KUBECTL[@]}" create namespace "$NAMESPACE" --dry-run=client -o yaml | "${KUBECTL[@]}" apply -f - >/dev/null

# Preserve the Erlang cookie on repeat deployments to avoid RabbitMQ startup failures.
if "${KUBECTL[@]}" -n "$NAMESPACE" get secret control-plane-credentials >/dev/null 2>&1; then
  existing_cookie=$("${KUBECTL[@]}" -n "$NAMESPACE" get secret control-plane-credentials -o 'jsonpath={.data.rabbitmq-cookie}')
  if [[ -n "$existing_cookie" ]]; then
    COOKIE=$(printf '%s' "$existing_cookie" | base64 --decode)
  else
    COOKIE=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
  fi
else
  COOKIE=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
fi

"${KUBECTL[@]}" -n "$NAMESPACE" create secret generic control-plane-credentials \
  --from-literal=db-password="$DB_PASSWORD" \
  --from-literal=rabbitmq-password="$RABBITMQ_PASSWORD" \
  --from-literal=rabbitmq-cookie="$COOKIE" \
  --from-literal=database-url="postgres://app:${DB_PASSWORD}@postgres:5432/tenants?sslmode=disable" \
  --from-literal=amqp-url="amqp://app:${RABBITMQ_PASSWORD}@rabbitmq:5672/" \
  --dry-run=client -o yaml | "${KUBECTL[@]}" apply -f - >/dev/null
printf 'Secret updated in namespace %s on Minikube profile %s.\n' "$NAMESPACE" "$MINIKUBE_PROFILE"
