#!/bin/bash
# Deploy Lobber to GCP Cloud Run
# Usage: ./deploy.sh <project-id> [region]

set -e

PROJECT_ID=${1:?Usage: ./deploy.sh <project-id> [region]}
REGION=${2:-us-central1}
SERVICE_NAME="lobber-relay"
IMAGE="gcr.io/${PROJECT_ID}/lobber-relay"

echo "==> Deploying Lobber to Cloud Run"
echo "    Project: ${PROJECT_ID}"
echo "    Region:  ${REGION}"
echo ""

# Ensure we're in the repo root
cd "$(dirname "$0")/../.."

# Build and push
echo "==> Building container image..."
docker build -t ${IMAGE}:latest -f docker/Dockerfile.relay .

echo "==> Pushing to GCR..."
docker push ${IMAGE}:latest

# Create secrets if they don't exist
echo "==> Checking secrets..."
if ! gcloud secrets describe lobber-database-url --project=${PROJECT_ID} &>/dev/null; then
    echo "    Creating lobber-database-url secret (you'll need to add the value)"
    echo "placeholder" | gcloud secrets create lobber-database-url --data-file=- --project=${PROJECT_ID}
    echo "    Run: gcloud secrets versions add lobber-database-url --data-file=- --project=${PROJECT_ID}"
fi

if ! gcloud secrets describe lobber-stripe-key --project=${PROJECT_ID} &>/dev/null; then
    echo "    Creating lobber-stripe-key secret"
    echo "placeholder" | gcloud secrets create lobber-stripe-key --data-file=- --project=${PROJECT_ID}
fi

if ! gcloud secrets describe lobber-stripe-webhook --project=${PROJECT_ID} &>/dev/null; then
    echo "    Creating lobber-stripe-webhook secret"
    echo "placeholder" | gcloud secrets create lobber-stripe-webhook --data-file=- --project=${PROJECT_ID}
fi

# Deploy
echo "==> Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
    --image ${IMAGE}:latest \
    --region ${REGION} \
    --platform managed \
    --allow-unauthenticated \
    --port 8080 \
    --timeout 3600 \
    --cpu 2 \
    --memory 1Gi \
    --min-instances 1 \
    --max-instances 10 \
    --concurrency 250 \
    --set-env-vars "DEV_MODE=true,HTTP_ADDR=:8080,SERVICE_DOMAIN=lobber.dev" \
    --set-secrets "DATABASE_URL=lobber-database-url:latest,STRIPE_API_KEY=lobber-stripe-key:latest,STRIPE_WEBHOOK_SECRET=lobber-stripe-webhook:latest" \
    --project ${PROJECT_ID}

# Get URL
URL=$(gcloud run services describe ${SERVICE_NAME} --region ${REGION} --project ${PROJECT_ID} --format 'value(status.url)')

echo ""
echo "==> Deployment complete!"
echo "    Service URL: ${URL}"
echo ""
echo "==> Next steps:"
echo "    1. Set up Cloud SQL PostgreSQL or use external DB"
echo "    2. Update secrets with real values:"
echo "       echo 'postgres://...' | gcloud secrets versions add lobber-database-url --data-file=- --project=${PROJECT_ID}"
echo "       echo 'sk_live_...' | gcloud secrets versions add lobber-stripe-key --data-file=- --project=${PROJECT_ID}"
echo "    3. Map custom domain in Cloud Run console"
echo "    4. Update DNS: lobber.dev -> Cloud Run service"
echo "    5. For wildcard subdomains (*.lobber.dev), use Cloud Load Balancer"
