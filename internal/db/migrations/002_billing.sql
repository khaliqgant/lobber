-- 002_billing.sql

-- Add stripe subscription ID to users
ALTER TABLE users ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT;

-- Update plan enum to include 'payg'
-- (plan column already exists with default 'free')

-- Bandwidth usage tracking table (per-request granularity)
CREATE TABLE IF NOT EXISTS bandwidth_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tunnel_session_id UUID REFERENCES tunnel_sessions(id) ON DELETE SET NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    synced_to_stripe BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_user_id ON bandwidth_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_recorded_at ON bandwidth_usage(recorded_at);
CREATE INDEX IF NOT EXISTS idx_bandwidth_usage_synced ON bandwidth_usage(synced_to_stripe) WHERE NOT synced_to_stripe;

-- Billing events table (Stripe webhook audit trail)
CREATE TABLE IF NOT EXISTS billing_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    stripe_event_id TEXT UNIQUE NOT NULL,
    event_type TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_billing_events_user_id ON billing_events(user_id);
CREATE INDEX IF NOT EXISTS idx_billing_events_type ON billing_events(event_type);
CREATE INDEX IF NOT EXISTS idx_billing_events_created_at ON billing_events(created_at);

-- Function to aggregate monthly usage (for billing queries)
CREATE OR REPLACE FUNCTION get_monthly_usage(p_user_id UUID, p_month DATE DEFAULT date_trunc('month', NOW()))
RETURNS BIGINT AS $$
DECLARE
    total_bytes BIGINT;
BEGIN
    SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
    INTO total_bytes
    FROM bandwidth_usage
    WHERE user_id = p_user_id
    AND recorded_at >= p_month
    AND recorded_at < p_month + INTERVAL '1 month';

    RETURN total_bytes;
END;
$$ LANGUAGE plpgsql;

-- View for user billing summary
CREATE OR REPLACE VIEW user_billing_summary AS
SELECT
    u.id as user_id,
    u.email,
    u.plan,
    u.stripe_customer_id,
    u.stripe_subscription_id,
    COALESCE(SUM(bu.bytes_in + bu.bytes_out), 0) as current_month_bytes,
    ROUND(COALESCE(SUM(bu.bytes_in + bu.bytes_out), 0) / (1024.0 * 1024 * 1024), 2) as current_month_gb,
    CASE
        WHEN u.plan = 'free' THEN 5.0  -- 5GB free tier
        ELSE -1  -- unlimited for paid plans
    END as limit_gb,
    CASE
        WHEN u.plan = 'free' AND COALESCE(SUM(bu.bytes_in + bu.bytes_out), 0) > 5368709120 THEN TRUE
        ELSE FALSE
    END as over_limit
FROM users u
LEFT JOIN bandwidth_usage bu ON bu.user_id = u.id
    AND bu.recorded_at >= date_trunc('month', NOW())
GROUP BY u.id, u.email, u.plan, u.stripe_customer_id, u.stripe_subscription_id;
