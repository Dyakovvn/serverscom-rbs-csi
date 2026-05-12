#!/usr/bin/env bash
# Renders a single install manifest from the Helm chart.
#
# Requires: helm >= 3, yq >= 4.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
CHART="$ROOT/charts/serverscom-rbs-csi"
OUTPUT="${ROOT}/rbs-csi-deploy.yaml"
IMAGE_TAG=""

usage() {
  cat <<'EOF'
Usage: scripts/helm-render.sh [--output FILE] [--image-tag TAG]

Renders a single Kubernetes manifest without Secret or StorageClass objects.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      OUTPUT="$2"
      shift 2
      ;;
    --image-tag)
      IMAGE_TAG="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

TMP="$(mktemp -d "${TMPDIR:-$HOME}/render-deploy.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/values.yaml" <<'EOF'
api:
  token: "deploy-api-token-example"
EOF

SET_ARGS=()
if [[ -n "$IMAGE_TAG" ]]; then
  SET_ARGS=(
    --set-string "controller.driver.image.tag=${IMAGE_TAG}"
    --set-string "node.driver.image.tag=${IMAGE_TAG}"
  )
fi

helm template rbs-csi "$CHART" \
  --namespace kube-system \
  -f "$TMP/values.yaml" \
  "${SET_ARGS[@]}" \
  --output-dir "$TMP/rendered" >/dev/null

SRC="$TMP/rendered/serverscom-rbs-csi/templates"

# Strip labels/annotations.
# app.kubernetes.io/name is kept intentionally — it's a standard, useful label.
clean() {
  yq ea '
    del(
      .metadata.labels."helm.sh/chart",
      .metadata.labels."app.kubernetes.io/managed-by",
      .metadata.labels."app.kubernetes.io/instance",
      .spec.template.metadata.labels."helm.sh/chart",
      .spec.template.metadata.labels."app.kubernetes.io/managed-by",
      .spec.template.metadata.labels."app.kubernetes.io/instance"
    )
  ' -P - < "$1"
}

{
  echo "# Generated deploy manifest from charts/serverscom-rbs-csi"
  echo "# Secret and StorageClass objects are intentionally excluded"
  echo
  for file in \
    "$SRC/rbac.yaml" \
    "$SRC/node-daemonset.yaml" \
    "$SRC/controller.yaml" \
    "$SRC/csi-driver.yaml"
  do
    echo "---"
    clean "$file"
  done
} > "$OUTPUT"

echo "rendered $OUTPUT"
