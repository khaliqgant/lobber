#!/bin/bash
# Tear down Lobber GCP infrastructure
# Usage: ./teardown.sh <project-id> [region]
#
# WARNING: This will delete all resources!

set -e

PROJECT_ID=${1:?Usage: ./teardown.sh <project-id> [region]}
REGION=${2:-us-central1}

RED='\033[0;31m'
NC='\033[0m'

echo -e "${RED}WARNING: This will delete all Lobber infrastructure!${NC}"
echo "Project: ${PROJECT_ID}"
echo ""
read -p "Type 'DELETE' to confirm: " CONFIRM

if [ "$CONFIRM" != "DELETE" ]; then
    echo "Aborted."
    exit 1
fi

echo "==> Deleting Load Balancer resources..."
gcloud compute forwarding-rules delete lobber-https-rule --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute forwarding-rules delete lobber-http-rule --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute target-https-proxies delete lobber-https-proxy --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute target-http-proxies delete lobber-http-proxy --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute url-maps delete lobber-urlmap --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute url-maps delete lobber-http-redirect --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute ssl-certificates delete lobber-cert --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute backend-services delete lobber-backend --global --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute network-endpoint-groups delete lobber-neg --region=${REGION} --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud compute addresses delete lobber-ip --global --project=${PROJECT_ID} -q 2>/dev/null || true

echo "==> Deleting Cloud Run service..."
gcloud run services delete lobber-relay --region=${REGION} --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud run domain-mappings delete --domain=lobber.dev --region=${REGION} --project=${PROJECT_ID} -q 2>/dev/null || true

echo "==> Deleting secrets..."
gcloud secrets delete lobber-database-url --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud secrets delete lobber-stripe-key --project=${PROJECT_ID} -q 2>/dev/null || true
gcloud secrets delete lobber-stripe-webhook --project=${PROJECT_ID} -q 2>/dev/null || true

echo "==> Deleting Cloud SQL instance..."
echo "    (This will delete all data!)"
read -p "Delete database? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    gcloud sql instances delete lobber-db --project=${PROJECT_ID} -q 2>/dev/null || true
fi

echo "==> Deleting container images..."
gcloud container images delete gcr.io/${PROJECT_ID}/lobber-relay --force-delete-tags -q 2>/dev/null || true

echo ""
echo "==> Teardown complete!"
