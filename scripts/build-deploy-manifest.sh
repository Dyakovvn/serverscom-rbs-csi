#!/bin/bash
set -euo pipefail

VERSION="$1"

OUTPUT="rbs-csi-deploy-${VERSION}.yaml"

echo "# Generated deploy manifest for version ${VERSION}" > "$OUTPUT"
echo "# Do not edit manually" >> "$OUTPUT"
echo >> "$OUTPUT"

for f in deploy/*.yaml; do
  echo "---" >> "$OUTPUT"
  sed "s|{{VERSION}}|v${VERSION}|g" "$f" >> "$OUTPUT"
done

echo "Manifest generated: $OUTPUT"