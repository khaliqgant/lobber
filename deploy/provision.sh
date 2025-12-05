#!/bin/bash
# Lobber GCP Infrastructure Provisioning
# This script sets up everything needed to run Lobber on GCP
#
# Usage: ./provision.sh <project-id> [region]
#
# Prerequisites:
#   - gcloud CLI installed and authenticated
#   - Billing account linked to project
#   - Domain (lobber.dev) DNS access

set -e

PROJECT_ID=${1:?Usage: ./provision.sh <project-id> [region]}
REGION=${2:-us-central1}
DB_INSTANCE="lobber-db"
DB_NAME="lobber"
DB_USER="lobber"
SERVICE_NAME="lobber-relay"
DOMAIN="lobber.dev"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() { echo -e "${GREEN}==>${NC} $1"; }
warn() { echo -e "${YELLOW}WARNING:${NC} $1"; }
error() { echo -e "${RED}ERROR:${NC} $1"; exit 1; }

# Prompt for sensitive values
prompt_secret() {
    local var_name=$1
    local prompt=$2
    local value
    read -sp "$prompt: " value
    echo
    eval "$var_name='$value'"
}

echo "=========================================="
echo "  Lobber GCP Infrastructure Provisioning"
echo "=========================================="
echo ""
echo "Project:  ${PROJECT_ID}"
echo "Region:   ${REGION}"
echo "Domain:   ${DOMAIN}"
echo ""

# Confirm
read -p "Continue? (y/n) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 1
fi

# Set project
log "Setting GCP project..."
gcloud config set project ${PROJECT_ID}

# Enable required APIs
log "Enabling required GCP APIs..."
gcloud services enable \
    run.googleapis.com \
    sql-component.googleapis.com \
    sqladmin.googleapis.com \
    secretmanager.googleapis.com \
    compute.googleapis.com \
    cloudbuild.googleapis.com \
    containerregistry.googleapis.com \
    --project=${PROJECT_ID}

# Wait for APIs to propagate
sleep 10

# ============================================
# DATABASE SETUP
# ============================================
log "Setting up Cloud SQL PostgreSQL..."

if gcloud sql instances describe ${DB_INSTANCE} --project=${PROJECT_ID} &>/dev/null; then
    warn "Database instance ${DB_INSTANCE} already exists, skipping creation"
else
    log "Creating Cloud SQL instance (this takes ~5 minutes)..."
    gcloud sql instances create ${DB_INSTANCE} \
        --database-version=POSTGRES_16 \
        --tier=db-f1-micro \
        --region=${REGION} \
        --storage-type=SSD \
        --storage-size=10GB \
        --storage-auto-increase \
        --backup-start-time=04:00 \
        --availability-type=zonal \
        --project=${PROJECT_ID}
fi

# Create database
if gcloud sql databases describe ${DB_NAME} --instance=${DB_INSTANCE} --project=${PROJECT_ID} &>/dev/null; then
    warn "Database ${DB_NAME} already exists"
else
    log "Creating database..."
    gcloud sql databases create ${DB_NAME} \
        --instance=${DB_INSTANCE} \
        --project=${PROJECT_ID}
fi

# Generate random password for DB
DB_PASSWORD=$(openssl rand -base64 24 | tr -dc 'a-zA-Z0-9' | head -c 24)

# Create user
log "Creating database user..."
gcloud sql users create ${DB_USER} \
    --instance=${DB_INSTANCE} \
    --password=${DB_PASSWORD} \
    --project=${PROJECT_ID} 2>/dev/null || warn "User may already exist"

# Get connection name
CONNECTION_NAME=$(gcloud sql instances describe ${DB_INSTANCE} --project=${PROJECT_ID} --format='value(connectionName)')

# Build DATABASE_URL
DATABASE_URL="postgres://${DB_USER}:${DB_PASSWORD}@/${DB_NAME}?host=/cloudsql/${CONNECTION_NAME}"

log "Database setup complete!"
echo "    Instance: ${DB_INSTANCE}"
echo "    Database: ${DB_NAME}"
echo "    Connection: ${CONNECTION_NAME}"

# ============================================
# SECRETS SETUP
# ============================================
log "Setting up secrets..."

# Get project number for IAM
PROJECT_NUMBER=$(gcloud projects describe ${PROJECT_ID} --format='value(projectNumber)')
SERVICE_ACCOUNT="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"

# Function to create or update secret
create_secret() {
    local name=$1
    local value=$2

    if gcloud secrets describe ${name} --project=${PROJECT_ID} &>/dev/null; then
        echo "${value}" | gcloud secrets versions add ${name} --data-file=- --project=${PROJECT_ID}
    else
        echo "${value}" | gcloud secrets create ${name} --data-file=- --project=${PROJECT_ID}
    fi

    # Grant access to Cloud Run service account
    gcloud secrets add-iam-policy-binding ${name} \
        --member="serviceAccount:${SERVICE_ACCOUNT}" \
        --role="roles/secretmanager.secretAccessor" \
        --project=${PROJECT_ID} &>/dev/null
}

# Database URL secret
log "Creating database URL secret..."
create_secret "lobber-database-url" "${DATABASE_URL}"

# Stripe secrets
echo ""
echo "Stripe API credentials (leave blank to skip):"
read -p "Stripe API Key (sk_live_... or sk_test_...): " STRIPE_KEY
if [ -n "$STRIPE_KEY" ]; then
    create_secret "lobber-stripe-key" "${STRIPE_KEY}"
else
    create_secret "lobber-stripe-key" "placeholder"
    warn "Stripe key set to placeholder - update later"
fi

read -p "Stripe Webhook Secret (whsec_...): " STRIPE_WEBHOOK
if [ -n "$STRIPE_WEBHOOK" ]; then
    create_secret "lobber-stripe-webhook" "${STRIPE_WEBHOOK}"
else
    create_secret "lobber-stripe-webhook" "placeholder"
    warn "Stripe webhook set to placeholder - update later"
fi

log "Secrets setup complete!"

# ============================================
# CONTAINER BUILD & DEPLOY
# ============================================
log "Building and deploying container..."

# Ensure we're in repo root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "${SCRIPT_DIR}/.."

# Build container
log "Building container image..."
gcloud builds submit \
    --tag gcr.io/${PROJECT_ID}/${SERVICE_NAME} \
    --project=${PROJECT_ID} \
    -q

# Deploy to Cloud Run
log "Deploying to Cloud Run..."
gcloud run deploy ${SERVICE_NAME} \
    --image gcr.io/${PROJECT_ID}/${SERVICE_NAME} \
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
    --add-cloudsql-instances=${CONNECTION_NAME} \
    --set-env-vars "DEV_MODE=true,HTTP_ADDR=:8080,SERVICE_DOMAIN=${DOMAIN}" \
    --set-secrets "DATABASE_URL=lobber-database-url:latest,STRIPE_API_KEY=lobber-stripe-key:latest,STRIPE_WEBHOOK_SECRET=lobber-stripe-webhook:latest" \
    --project=${PROJECT_ID}

# Get service URL
SERVICE_URL=$(gcloud run services describe ${SERVICE_NAME} --region ${REGION} --project=${PROJECT_ID} --format 'value(status.url)')

log "Cloud Run deployment complete!"
echo "    Service URL: ${SERVICE_URL}"

# ============================================
# DOMAIN MAPPING
# ============================================
log "Setting up domain mapping..."

# Map main domain
gcloud run domain-mappings create \
    --service=${SERVICE_NAME} \
    --domain=${DOMAIN} \
    --region=${REGION} \
    --project=${PROJECT_ID} 2>/dev/null || warn "Domain mapping may already exist"

# Get DNS records
echo ""
log "DNS Configuration Required:"
echo "    Add these DNS records to ${DOMAIN}:"
echo ""
gcloud run domain-mappings describe \
    --domain=${DOMAIN} \
    --region=${REGION} \
    --project=${PROJECT_ID} \
    --format='table(resourceRecords.type,resourceRecords.rrdata)' 2>/dev/null || true

# ============================================
# WILDCARD DOMAIN (Load Balancer)
# ============================================
echo ""
log "Setting up Load Balancer for wildcard domain (*.${DOMAIN})..."

# Reserve static IP
log "Reserving static IP..."
gcloud compute addresses create lobber-ip \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || warn "IP may already exist"

STATIC_IP=$(gcloud compute addresses describe lobber-ip --global --project=${PROJECT_ID} --format='value(address)')

# Create serverless NEG
log "Creating serverless network endpoint group..."
gcloud compute network-endpoint-groups create lobber-neg \
    --region=${REGION} \
    --network-endpoint-type=serverless \
    --cloud-run-service=${SERVICE_NAME} \
    --project=${PROJECT_ID} 2>/dev/null || warn "NEG may already exist"

# Create backend service
log "Creating backend service..."
gcloud compute backend-services create lobber-backend \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || warn "Backend may already exist"

gcloud compute backend-services add-backend lobber-backend \
    --global \
    --network-endpoint-group=lobber-neg \
    --network-endpoint-group-region=${REGION} \
    --project=${PROJECT_ID} 2>/dev/null || true

# Create URL map
log "Creating URL map..."
gcloud compute url-maps create lobber-urlmap \
    --default-service=lobber-backend \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || warn "URL map may already exist"

# Create managed SSL certificate
log "Creating managed SSL certificate..."
gcloud compute ssl-certificates create lobber-cert \
    --domains="${DOMAIN},*.${DOMAIN}" \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || warn "SSL cert may already exist"

# Create HTTPS proxy
log "Creating HTTPS proxy..."
gcloud compute target-https-proxies create lobber-https-proxy \
    --ssl-certificates=lobber-cert \
    --url-map=lobber-urlmap \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || warn "HTTPS proxy may already exist"

# Create forwarding rule
log "Creating forwarding rule..."
gcloud compute forwarding-rules create lobber-https-rule \
    --global \
    --target-https-proxy=lobber-https-proxy \
    --address=lobber-ip \
    --ports=443 \
    --project=${PROJECT_ID} 2>/dev/null || warn "Forwarding rule may already exist"

# HTTP redirect
log "Setting up HTTP to HTTPS redirect..."
gcloud compute url-maps import lobber-http-redirect \
    --global \
    --project=${PROJECT_ID} \
    --source=/dev/stdin <<EOF 2>/dev/null || true
name: lobber-http-redirect
defaultUrlRedirect:
  httpsRedirect: true
  redirectResponseCode: MOVED_PERMANENTLY_DEFAULT
EOF

gcloud compute target-http-proxies create lobber-http-proxy \
    --url-map=lobber-http-redirect \
    --global \
    --project=${PROJECT_ID} 2>/dev/null || true

gcloud compute forwarding-rules create lobber-http-rule \
    --global \
    --target-http-proxy=lobber-http-proxy \
    --address=lobber-ip \
    --ports=80 \
    --project=${PROJECT_ID} 2>/dev/null || true

# ============================================
# SUMMARY
# ============================================
echo ""
echo "=========================================="
echo "  Provisioning Complete!"
echo "=========================================="
echo ""
echo "Cloud Run Service: ${SERVICE_URL}"
echo "Static IP: ${STATIC_IP}"
echo ""
echo "DNS RECORDS REQUIRED:"
echo "  ${DOMAIN}       A     ${STATIC_IP}"
echo "  *.${DOMAIN}     A     ${STATIC_IP}"
echo ""
echo "SSL Certificate Status (may take 10-60 mins to provision):"
gcloud compute ssl-certificates describe lobber-cert --global --project=${PROJECT_ID} --format='value(managed.status)' 2>/dev/null || echo "pending"
echo ""
echo "Database Connection:"
echo "  Instance: ${CONNECTION_NAME}"
echo "  Password stored in Secret Manager"
echo ""
echo "Next Steps:"
echo "  1. Update DNS records as shown above"
echo "  2. Wait for SSL certificate to provision"
echo "  3. Update Stripe secrets if using placeholder values"
echo "  4. Run migrations: ./deploy/run-migrations.sh ${PROJECT_ID}"
echo "  5. Test: curl https://${DOMAIN}/health"
echo ""
