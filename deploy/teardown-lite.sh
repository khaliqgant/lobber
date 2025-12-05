#!/bin/bash
# Tear down Lobber LITE infrastructure
# Usage: ./teardown-lite.sh <project-id> [region]

set -e

PROJECT_ID=${1:?Usage: ./teardown-lite.sh <project-id> [region]}
REGION=${2:-us-central1}

RED='\033[0;31m'
NC='\033[0m'

echo -e "${RED}WARNING: This will delete Lobber Cloud Run infrastructure!${NC}"
echo "Project: ${PROJECT_ID}"
echo ""
echo "Note: Cloudflare and Neon resources must be deleted manually."
echo ""
read -p "Type 'DELETE' to confirm: " CONFIRM

if [ "$CONFIRM" != "DELETE" ]; then
    echo "Aborted."
    exit 1
fi

echo "==> Deleting Cloud Run service..."
gcloud run services delete lobber-relay --region=${REGION} --project=${PROJECT_ID} -q 2>/dev/null || true

echo "==> Deleting secrets..."
gcloud secrets delete lobber-database-url --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud secrets delete lobber-stripe-key --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud secrets delete lobber-stripe-webhook --project=${PROJECT_ID} -q 2>/dev/null || true

echo "==> Deleting container images..."
gcloud container images delete gcr.io/${PROJECT_ID}/lobber-relay --force-delete-tags -q 2>/dev/null || true

echo ""
echo "==> GCP teardown complete!"
echo ""
echo "Manual cleanup required:"
echo "  1. Cloudflare: Remove DNS records for lobber.dev"
echo "  2. Neon: Delete project at https://console.neon.tech"
echo ""
