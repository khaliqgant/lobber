// internal/billing/service.go
package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Plan represents a billing plan
type Plan string

const (
	PlanFree Plan = "free"
	PlanPAYG Plan = "payg"
	PlanPro  Plan = "pro"
)

// FreeTierBytes is the free tier bandwidth limit (5GB)
const FreeTierBytes int64 = 5 * 1024 * 1024 * 1024

// UsageRecord represents bandwidth usage for a tunnel session
type UsageRecord struct {
	ID              int64
	UserID          int64
	TunnelSessionID int64
	BytesIn         int64
	BytesOut        int64
	RecordedAt      time.Time
	SyncedToStripe  bool
}

// UserBilling represents a user's billing information
type UserBilling struct {
	UserID             int64
	StripeCustomerID   string
	StripeSubscription string
	Plan               Plan
	CurrentUsageBytes  int64
	BillingPeriodStart time.Time
}

// Service handles billing operations
type Service struct {
	db     *sql.DB
	stripe *StripeClient
}

// NewService creates a new billing service
func NewService(db *sql.DB, stripeKey string) *Service {
	var stripeClient *StripeClient
	if stripeKey != "" {
		stripeClient = NewStripeClient(stripeKey)
	}
	return &Service{
		db:     db,
		stripe: stripeClient,
	}
}

// RecordBandwidth records bandwidth usage for a user/tunnel
func (s *Service) RecordBandwidth(ctx context.Context, userID, tunnelSessionID, bytesIn, bytesOut int64) error {
	if s.db == nil {
		return nil // No-op if no database
	}

	query := `
		INSERT INTO bandwidth_usage (user_id, tunnel_session_id, bytes_in, bytes_out, recorded_at)
		VALUES ($1, $2, $3, $4, NOW())
	`
	_, err := s.db.ExecContext(ctx, query, userID, tunnelSessionID, bytesIn, bytesOut)
	if err != nil {
		return fmt.Errorf("record bandwidth: %w", err)
	}
	return nil
}

// GetUserUsage returns total usage for a user in the current billing period
func (s *Service) GetUserUsage(ctx context.Context, userID int64) (int64, error) {
	if s.db == nil {
		return 0, nil
	}

	// Get usage for current month
	query := `
		SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM bandwidth_usage
		WHERE user_id = $1
		AND recorded_at >= date_trunc('month', NOW())
	`
	var totalBytes int64
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&totalBytes)
	if err != nil {
		return 0, fmt.Errorf("get user usage: %w", err)
	}
	return totalBytes, nil
}

// CheckQuota checks if user is within their quota
// Returns (withinQuota, usedBytes, limitBytes, error)
func (s *Service) CheckQuota(ctx context.Context, userID int64) (bool, int64, int64, error) {
	if s.db == nil {
		return true, 0, FreeTierBytes, nil
	}

	// Get user's plan
	var plan string
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(plan, 'free') FROM users WHERE id = $1", userID).Scan(&plan)
	if err != nil {
		return false, 0, 0, fmt.Errorf("get user plan: %w", err)
	}

	// Get current usage
	usedBytes, err := s.GetUserUsage(ctx, userID)
	if err != nil {
		return false, 0, 0, err
	}

	// Determine limit based on plan
	var limitBytes int64
	switch Plan(plan) {
	case PlanFree:
		limitBytes = FreeTierBytes
	case PlanPAYG:
		limitBytes = -1 // No limit, just pay for usage
	case PlanPro:
		limitBytes = -1 // No limit for pro
	default:
		limitBytes = FreeTierBytes
	}

	if limitBytes == -1 {
		return true, usedBytes, limitBytes, nil
	}

	return usedBytes < limitBytes, usedBytes, limitBytes, nil
}

// CreateCustomerForUser creates a Stripe customer for a user
func (s *Service) CreateCustomerForUser(ctx context.Context, userID int64, email, name string) (string, error) {
	if s.stripe == nil {
		return "", fmt.Errorf("stripe not configured")
	}

	// Create Stripe customer
	customerID, err := s.stripe.CreateCustomer(email, name)
	if err != nil {
		return "", err
	}

	// Save to database
	if s.db != nil {
		_, err = s.db.ExecContext(ctx,
			"UPDATE users SET stripe_customer_id = $1 WHERE id = $2",
			customerID, userID)
		if err != nil {
			return customerID, fmt.Errorf("save customer id: %w", err)
		}
	}

	return customerID, nil
}

// UpgradeToPAYG upgrades a user to pay-as-you-go billing
func (s *Service) UpgradeToPAYG(ctx context.Context, userID int64, priceID string) error {
	if s.db == nil || s.stripe == nil {
		return fmt.Errorf("billing not configured")
	}

	// Get user's Stripe customer ID
	var customerID string
	err := s.db.QueryRowContext(ctx,
		"SELECT stripe_customer_id FROM users WHERE id = $1", userID).Scan(&customerID)
	if err != nil {
		return fmt.Errorf("get customer id: %w", err)
	}

	if customerID == "" {
		return fmt.Errorf("user has no stripe customer")
	}

	// Create metered subscription
	sub, err := s.stripe.CreateMeteredSubscription(customerID, priceID)
	if err != nil {
		return err
	}

	// Update user's plan
	_, err = s.db.ExecContext(ctx,
		"UPDATE users SET plan = $1, stripe_subscription_id = $2 WHERE id = $3",
		PlanPAYG, sub.ID, userID)
	if err != nil {
		return fmt.Errorf("update user plan: %w", err)
	}

	return nil
}

// SyncUsageToStripe syncs unsynced usage records to Stripe
func (s *Service) SyncUsageToStripe(ctx context.Context) error {
	if s.db == nil || s.stripe == nil {
		return nil
	}

	// Get users with unsynced usage and active subscriptions
	query := `
		SELECT u.id, u.stripe_subscription_id, SUM(bu.bytes_in + bu.bytes_out) as total_bytes
		FROM users u
		JOIN bandwidth_usage bu ON bu.user_id = u.id
		WHERE bu.synced_to_stripe = FALSE
		AND u.stripe_subscription_id IS NOT NULL
		AND u.plan IN ('payg', 'pro')
		GROUP BY u.id, u.stripe_subscription_id
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query unsynced usage: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var userID int64
		var subscriptionID string
		var totalBytes int64

		if err := rows.Scan(&userID, &subscriptionID, &totalBytes); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}

		// Get subscription to find the subscription item ID
		sub, err := s.stripe.GetSubscription(subscriptionID)
		if err != nil {
			return fmt.Errorf("get subscription: %w", err)
		}

		if len(sub.Items.Data) == 0 {
			continue
		}

		// Report usage
		err = s.stripe.ReportUsage(sub.Items.Data[0].ID, totalBytes)
		if err != nil {
			return fmt.Errorf("report usage for user %d: %w", userID, err)
		}

		// Mark as synced
		_, err = s.db.ExecContext(ctx,
			"UPDATE bandwidth_usage SET synced_to_stripe = TRUE WHERE user_id = $1 AND synced_to_stripe = FALSE",
			userID)
		if err != nil {
			return fmt.Errorf("mark synced: %w", err)
		}
	}

	return nil
}

// GetUsageSummary returns a usage summary for a user
type UsageSummary struct {
	UserID       int64
	Plan         Plan
	UsedBytes    int64
	LimitBytes   int64
	UsedGB       float64
	LimitGB      float64
	PercentUsed  float64
	OverLimit    bool
	PeriodStart  time.Time
	PeriodEnd    time.Time
}

func (s *Service) GetUsageSummary(ctx context.Context, userID int64) (*UsageSummary, error) {
	withinQuota, usedBytes, limitBytes, err := s.CheckQuota(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get user's plan
	var plan string
	if s.db != nil {
		s.db.QueryRowContext(ctx, "SELECT COALESCE(plan, 'free') FROM users WHERE id = $1", userID).Scan(&plan)
	}
	if plan == "" {
		plan = string(PlanFree)
	}

	now := time.Now()
	periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Second)

	summary := &UsageSummary{
		UserID:      userID,
		Plan:        Plan(plan),
		UsedBytes:   usedBytes,
		LimitBytes:  limitBytes,
		UsedGB:      BytesToGB(usedBytes),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		OverLimit:   !withinQuota,
	}

	if limitBytes > 0 {
		summary.LimitGB = BytesToGB(limitBytes)
		summary.PercentUsed = float64(usedBytes) / float64(limitBytes) * 100
	}

	return summary, nil
}
