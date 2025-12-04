-- 001_initial.sql

-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT UNIQUE NOT NULL,
    name TEXT,
    stripe_customer_id TEXT,
    plan TEXT NOT NULL DEFAULT 'free', -- 'free', 'pro'
    bandwidth_used_bytes BIGINT NOT NULL DEFAULT 0,
    bandwidth_reset_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Domains table
CREATE TABLE domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hostname TEXT UNIQUE NOT NULL, -- e.g., 'app.mysite.com'
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_domains_hostname ON domains(hostname);
CREATE INDEX idx_domains_user_id ON domains(user_id);

-- API tokens table
CREATE TABLE api_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT UNIQUE NOT NULL, -- bcrypt hash of token
    name TEXT NOT NULL,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Request logs table
CREATE TABLE request_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status_code INTEGER NOT NULL,
    request_headers JSONB,
    response_headers JSONB,
    request_body_preview TEXT, -- First 10KB
    response_body_preview TEXT, -- First 10KB
    request_size_bytes INTEGER NOT NULL,
    response_size_bytes INTEGER NOT NULL,
    duration_ms INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_request_logs_domain_id ON request_logs(domain_id);
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at);

-- Tunnel sessions table (for uptime monitoring)
CREATE TABLE tunnel_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain_id UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    disconnect_reason TEXT
);

CREATE INDEX idx_tunnel_sessions_domain_id ON tunnel_sessions(domain_id);
