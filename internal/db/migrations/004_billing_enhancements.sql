-- 004_billing_enhancements.sql
-- Make billing migration idempotent and additive to prior 002_billing.sql

-- Ensure tables exist (no-op if created by earlier migrations)
CREATE TABLE IF NOT EXISTS bandwidth_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tunnel_session_id UUID REFERENCES tunnel_sessions(id) ON DELETE SET NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    billing_period DATE NOT NULL DEFAULT date_trunc('month', NOW())::DATE,
    synced_to_stripe BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS billing_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    stripe_event_id TEXT UNIQUE NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Additive columns (safe if already present)
ALTER TABLE bandwidth_usage
    ADD COLUMN IF NOT EXISTS billing_period DATE NOT NULL DEFAULT date_trunc('month', NOW())::DATE,
    ADD COLUMN IF NOT EXISTS synced_to_stripe BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE billing_events
    ADD COLUMN IF NOT EXISTS processed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS processed_at TIMESTAMPTZ;

ALTER TABLE users ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT;

-- Indexes (IF NOT EXISTS ensures idempotency)
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_user_id ON bandwidth_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_recorded_at ON bandwidth_usage(recorded_at);
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_billing_period ON bandwidth_usage(billing_period);
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_unsynced ON bandwidth_usage(synced_to_stripe) WHERE NOT synced_to_stripe;

CREATE INDEX IF NOT EXISTS idx_billing_events_user_id ON billing_events(user_id);
CREATE INDEX IF NOT EXISTS idx_billing_events_event_type ON billing_events(event_type);
CREATE INDEX IF NOT EXISTS idx_billing_events_created_at ON billing_events(created_at);
CREATE INDEX IF NOT EXISTS idx_billing_events_stripe_event_id ON billing_events(stripe_event_id);
