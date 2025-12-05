#!/bin/bash
# Lobber GCP Infrastructure Provisioning (LITE)
# Cost-optimized setup: ~$5-15/mo
#
# Uses:
#   - Cloudflare (free) for wildcard SSL + proxy
#   - Neon/Supabase (free) for PostgreSQL
#   - Cloud Run with min-instances=0
#
# Usage: ./provision-lite.sh <project-id> [region]
#
# Prerequisites:
#   - gcloud CLI installed and authenticated
#   - Cloudflare account with lobber.dev added
#   - Neon or Supabase account with database created

set -e

PROJECT_ID=${1:?Usage: ./provision-lite.sh <project-id> [region]}
REGION=${2:-us-central1}
SERVICE_NAME="lobber-relay"
DOMAIN="lobber.dev"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

log() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}WARNING:${NC} $1"; }
info() { echo -e "${CYAN}INFO:${NC} $1"; }

echo "=========================================="
echo "  Lobber LITE Provisioning"
echo "  (Cost-optimized: ~\$5-15/mo)"
echo "=========================================="
echo ""
echo "Project:  ${PROJECT_ID}"
echo "Region:   ${REGION}"
echo "Domain:   ${DOMAIN}"
echo ""

# ============================================
# PRE-FLIGHT CHECKS
# ============================================
echo -e "${CYAN}Before continuing, you need:${NC}"
echo ""
echo "1. Cloudflare account with ${DOMAIN} added"
echo "   - Sign up: https://dash.cloudflare.com/sign-up"
echo "   - Add site: ${DOMAIN}"
echo "   - Update nameservers at your registrar"
echo ""
echo "2. Neon database (free tier)"
echo "   - Sign up: https://neon.tech"
echo "   - Create project 'lobber'"
echo "   - Copy connection string"
echo ""
read -p "Do you have both set up? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo ""
    echo "Setup instructions:"
    echo ""
    echo "=== NEON DATABASE ==="
    echo "1. Go to https://neon.tech and sign up"
    echo "2. Create new project: 'lobber'"
    echo "3. Copy the connection string (looks like):"
    echo "   postgres://user:pass@ep-xxx.us-east-1.aws.neon.tech/lobber?sslmode=require"
    echo ""
    echo "=== CLOUDFLARE ==="
    echo "1. Go to https://dash.cloudflare.com/sign-up"
    echo "2. Add site: ${DOMAIN}"
    echo "3. Update nameservers at your domain registrar"
    echo "4. Wait for DNS to propagate (can take up to 24h)"
    echo ""
    echo "Run this script again when ready."
    exit 0
fi

# Get database URL
echo ""
read -p "Paste your Neon/Supabase DATABASE_URL: " DATABASE_URL
if [ -z "$DATABASE_URL" ]; then
    error "DATABASE_URL is required"
    exit 1
fi

# Confirm
echo ""
read -p "Continue with provisioning? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 1
fi

# Set project
log "Setting GCP project..."
gcloud config set project ${PROJECT_ID}

# Enable required APIs (fewer than full version)
log "Enabling required GCP APIs..."
gcloud services enable \
    run.googleapis.com \
    secretmanager.googleapis.com \
    cloudbuild.googleapis.com \
    containerregistry.googleapis.com \
    --project=${PROJECT_ID}

sleep 5

# ============================================
# SECRETS SETUP
# ============================================
log "Setting up secrets..."

PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

create_secret() {
    local name=$1
    local value=$2

    if gcloud secrets describe ${name} --project=${PROJECT_ID} &>/dev/null; then
        echo "${value}" | gcloud secrets versions add ${name} --data-file=- --project=${PROJECT_ID}
    else
        echo "${value}" | gcloud secrets create ${name} --data-file=- --project=${PROJECT_ID}
    fi

    gcloud secrets add-iam-policy-binding ${name} \
        --member="serviceAccount:${SERVICE_ACCOUNT}" \
        --role="roles/secretmanager.secretAccessor" \
        --project=${PROJECT_ID} &>/dev/null
}

# Database URL
log "Storing database URL..."
create_secret "lobber-database-url" "${DATABASE_URL}"

# Stripe (optional)
echo ""
read -p "Stripe API Key (leave blank to skip): " STRIPE_KEY
if [ -n "$STRIPE_KEY" ]; then
    create_secret "lobber-stripe-key" "${STRIPE_KEY}"
else
    create_secret "lobber-stripe-key" "placeholder"
fi

read -p "Stripe Webhook Secret (leave blank to skip): " STRIPE_WEBHOOK
if [ -n "$STRIPE_WEBHOOK" ]; then
    create_secret "lobber-stripe-webhook" "${STRIPE_WEBHOOK}"
else
    create_secret "lobber-stripe-webhook" "placeholder"
fi

# ============================================
# BUILD & DEPLOY
# ============================================
log "Building and deploying container..."

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}/.."

log "Building container image..."
gcloud builds submit \
    --tag gcr.io/${PROJECT_ID}/${SERVICE_NAME} \
    --project=${PROJECT_ID} \
    -q

log "Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
    --image gcr.io/${PROJECT_ID}/${SERVICE_NAME} \
    --region ${REGION} \
    --platform managed \
    --allow-unauthenticated \
    --port 8080 \
    --timeout 3600 \
    --cpu 1 \
    --memory 512Mi \
    --min-instances 0 \
    --max-instances 10 \
    --concurrency 250 \
    --set-env-vars "DEV_MODE=true,HTTP_ADDR=:8080,SERVICE_DOMAIN=${DOMAIN}" \
    --set-secrets "DATABASE_URL=lobber-database-url:latest,STRIPE_API_KEY=lobber-stripe-key:latest,STRIPE_WEBHOOK_SECRET=lobber-stripe-webhook:latest" \
    --project=${PROJECT_ID}

# Get service URL
SERVICE_URL=$(gcloud run services describe ${SERVICE_NAME} --region ${REGION} --project=${PROJECT_ID} --format 'value(status.url)')

log "Cloud Run deployment complete!"
echo "    Service URL: ${SERVICE_URL}"

# ============================================
# CLOUDFLARE SETUP INSTRUCTIONS
# ============================================
echo ""
echo "=========================================="
echo "  CLOUDFLARE CONFIGURATION REQUIRED"
echo "=========================================="
echo ""
echo "1. Go to Cloudflare Dashboard: https://dash.cloudflare.com"
echo "2. Select ${DOMAIN}"
echo "3. Go to DNS settings and add these records:"
echo ""
echo "   Type  | Name | Content                | Proxy"
echo "   ------|------|------------------------|-------"
echo "   CNAME | @    | ${SERVICE_URL#https://} | ON (orange cloud)"
echo "   CNAME | *    | ${SERVICE_URL#https://} | ON (orange cloud)"
echo ""
echo "4. Go to SSL/TLS settings:"
echo "   - Set mode to 'Full' (not Full Strict)"
echo ""
echo "5. Go to Rules > Page Rules (optional):"
echo "   - Add rule for http://*${DOMAIN}/*"
echo "   - Setting: Always Use HTTPS"
echo ""
echo "=========================================="
echo "  PROVISIONING COMPLETE!"
echo "=========================================="
echo ""
echo "Cloud Run URL: ${SERVICE_URL}"
echo ""
echo "After Cloudflare DNS propagates:"
echo "  curl https://${DOMAIN}/health"
echo ""
echo "Estimated monthly cost: \$5-15"
echo "  - Cloud Run: \$5-15 (scales to zero)"
echo "  - Neon DB: \$0 (free tier)"
echo "  - Cloudflare: \$0 (free plan)"
echo ""
