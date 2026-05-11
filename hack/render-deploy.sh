#!/usr/bin/env bash
# Regenerates deploy/ from the Helm chart in charts/serverscom-rbs-csi.
#
# Requires: helm >= 3, yq >= 4.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
CHART="$ROOT/charts/serverscom-rbs-csi"
OUT="$ROOT/deploy"
TMP="$(mktemp -d "${TMPDIR:-$HOME}/render-deploy.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

cat > "$TMP/values.yaml" <<'EOF'
api:
  token: "deploy-api-token-example"
EOF

helm template rbs-csi "$CHART" \
  --namespace kube-system \
  -f "$TMP/values.yaml" \
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

mkdir -p "$OUT"
clean "$SRC/controller.yaml"     > "$OUT/controller.yaml"
clean "$SRC/node-daemonset.yaml" > "$OUT/node-daemonset.yaml"
clean "$SRC/rbac.yaml"           > "$OUT/rbac.yaml"
clean "$SRC/csi-driver.yaml"     > "$OUT/csi-driver.yaml"

echo "deploy/ regenerated from chart"
