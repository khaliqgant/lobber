#!/bin/bash
# Run database migrations against Cloud SQL
# Usage: ./run-migrations.sh <project-id> [region]

set -e

PROJECT_ID=${1:?Usage: ./run-migrations.sh <project-id> [region]}
REGION=${2:-us-central1}
DB_INSTANCE="lobber-db"
DB_NAME="lobber"

echo "==> Running migrations for ${PROJECT_ID}"

# Get connection name
CONNECTION_NAME=$(gcloud sql instances describe ${DB_INSTANCE} --project=${PROJECT_ID} --format='value(connectionName)')

# Start Cloud SQL Proxy in background
echo "==> Starting Cloud SQL Proxy..."
cloud_sql_proxy -instances=${CONNECTION_NAME}=tcp:5433 &
PROXY_PID=$!
sleep 3

# Get password from secret
DB_PASSWORD=$(gcloud secrets versions access latest --secret=lobber-database-url --project=${PROJECT_ID} | grep -oP '://[^:]+:\K[^@]+')

# Run migrations
echo "==> Applying migrations..."
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MIGRATIONS_DIR="${SCRIPT_DIR}/../internal/db/migrations"

for migration in $(ls ${MIGRATIONS_DIR}/*.sql | sort); do
    echo "    Applying $(basename ${migration})..."
    PGPASSWORD=${DB_PASSWORD} psql \
        -h 127.0.0.1 \
        -p 5433 \
        -U lobber \
        -d ${DB_NAME} \
        -f ${migration}
done

# Cleanup
echo "==> Stopping Cloud SQL Proxy..."
kill ${PROXY_PID} 2>/dev/null || true

echo "==> Migrations complete!"
