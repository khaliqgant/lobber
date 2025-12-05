# Deploying Lobber to GCP Cloud Run

## Prerequisites

1. GCP project with billing enabled
2. `gcloud` CLI installed and authenticated
3. Docker installed locally
4. Cloud SQL PostgreSQL instance (or external DB)

## Quick Deploy

```bash
# One-command deploy
./deploy.sh YOUR_PROJECT_ID us-central1
```

## Manual Setup

### 1. Create Cloud SQL Instance

```bash
# Create PostgreSQL instance
gcloud sql instances create lobber-db \
    --database-version=POSTGRES_16 \
    --tier=db-f1-micro \
    --region=us-central1 \
    --project=YOUR_PROJECT_ID

# Create database
gcloud sql databases create lobber \
    --instance=lobber-db \
    --project=YOUR_PROJECT_ID

# Create user
gcloud sql users create lobber \
    --instance=lobber-db \
    --password=YOUR_SECURE_PASSWORD \
    --project=YOUR_PROJECT_ID
```

### 2. Set Up Secrets

```bash
# Database URL (use Cloud SQL Proxy connection)
echo "postgres://lobber:PASSWORD@/lobber?host=/cloudsql/PROJECT:REGION:lobber-db" | \
    gcloud secrets create lobber-database-url --data-file=-

# Stripe keys
echo "sk_live_YOUR_KEY" | gcloud secrets create lobber-stripe-key --data-file=-
echo "whsec_YOUR_SECRET" | gcloud secrets create lobber-stripe-webhook --data-file=-

# Grant access to Cloud Run service account
gcloud secrets add-iam-policy-binding lobber-database-url \
    --member="serviceAccount:PROJECT_NUMBER-compute@developer.gserviceaccount.com" \
    --role="roles/secretmanager.secretAccessor"
```

### 3. Deploy Service

```bash
# Build and push image
docker build -t gcr.io/YOUR_PROJECT/lobber-relay -f docker/Dockerfile.relay .
docker push gcr.io/YOUR_PROJECT/lobber-relay

# Deploy
gcloud run deploy lobber-relay \
    --image gcr.io/YOUR_PROJECT/lobber-relay \
    --region us-central1 \
    --allow-unauthenticated \
    --add-cloudsql-instances=YOUR_PROJECT:us-central1:lobber-db \
    --set-env-vars "DEV_MODE=true,HTTP_ADDR=:8080,SERVICE_DOMAIN=lobber.dev" \
    --set-secrets "DATABASE_URL=lobber-database-url:latest"
```

### 4. Custom Domain Setup

For `lobber.dev`:

1. Go to Cloud Run console → lobber-relay → Domain mappings
2. Add `lobber.dev` domain
3. Update DNS with provided records

For wildcard `*.lobber.dev` (tunnel subdomains):

Cloud Run doesn't support wildcard domains directly. Options:

**Option A: Cloud Load Balancer (recommended)**
```bash
# Create serverless NEG
gcloud compute network-endpoint-groups create lobber-neg \
    --region=us-central1 \
    --network-endpoint-type=serverless \
    --cloud-run-service=lobber-relay

# Create backend service, URL map, and HTTPS proxy
# Then add SSL cert for *.lobber.dev
```

**Option B: Cloudflare Proxy**
- Point `*.lobber.dev` to Cloud Run URL via Cloudflare
- Cloudflare handles wildcard SSL

## Architecture Notes

- Cloud Run handles TLS termination (DEV_MODE=true disables internal TLS)
- Request timeout set to 3600s (1 hour) for tunnel connections
- Min instances = 1 to keep tunnels alive
- Concurrency = 250 for multiple simultaneous tunnels

## Monitoring

```bash
# View logs
gcloud run services logs read lobber-relay --region us-central1

# View metrics in console
open "https://console.cloud.google.com/run/detail/us-central1/lobber-relay/metrics"
```
