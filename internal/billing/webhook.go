// internal/billing/webhook.go
package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/webhook"
)

// WebhookHandler handles Stripe webhook events
type WebhookHandler struct {
	db            *sql.DB
	webhookSecret string
	service       *Service
}

// NewWebhookHandler creates a new webhook handler
func NewWebhookHandler(db *sql.DB, webhookSecret string, service *Service) *WebhookHandler {
	return &WebhookHandler{
		db:            db,
		webhookSecret: webhookSecret,
		service:       service,
	}
}

// HandleWebhook processes incoming Stripe webhook events
// IMPORTANT: This handler expects the raw request body for signature verification
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	// Read the raw body for signature verification
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "error reading request body", http.StatusServiceUnavailable)
		return
	}

	// Verify webhook signature
	sigHeader := r.Header.Get("Stripe-Signature")
	event, err := webhook.ConstructEvent(payload, sigHeader, h.webhookSecret)
	if err != nil {
		http.Error(w, "webhook signature verification failed", http.StatusBadRequest)
		return
	}

	// Check idempotency - have we processed this event already?
	ctx := r.Context()
	processed, err := h.isEventProcessed(ctx, event.ID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	if processed {
		// Already processed, return 200 to acknowledge
		w.WriteHeader(http.StatusOK)
		return
	}

	// Store the event for audit trail
	if err := h.storeEvent(ctx, &event); err != nil {
		http.Error(w, "failed to store event", http.StatusInternalServerError)
		return
	}

	// Process the event
	if err := h.processEvent(ctx, &event); err != nil {
		// Log error but return 200 to prevent Stripe retries for handled events
		// In production, you might want to retry or alert on certain errors
		fmt.Printf("webhook processing error: %v\n", err)
	}

	// Mark event as processed
	h.markEventProcessed(ctx, event.ID)

	// Return 200 immediately
	w.WriteHeader(http.StatusOK)
}

// isEventProcessed checks if we've already processed this Stripe event
func (h *WebhookHandler) isEventProcessed(ctx context.Context, eventID string) (bool, error) {
	if h.db == nil {
		return false, nil
	}

	var processed bool
	err := h.db.QueryRowContext(ctx,
		"SELECT processed FROM billing_events WHERE stripe_event_id = $1",
		eventID).Scan(&processed)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check event processed: %w", err)
	}
	return processed, nil
}

// storeEvent stores the webhook event for audit trail
func (h *WebhookHandler) storeEvent(ctx context.Context, event *stripe.Event) error {
	if h.db == nil {
		return nil
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = h.db.ExecContext(ctx, `
		INSERT INTO billing_events (stripe_event_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (stripe_event_id) DO NOTHING
	`, event.ID, event.Type, payload, time.Now())
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

// markEventProcessed marks an event as successfully processed
func (h *WebhookHandler) markEventProcessed(ctx context.Context, eventID string) error {
	if h.db == nil {
		return nil
	}

	_, err := h.db.ExecContext(ctx, `
		UPDATE billing_events
		SET processed = TRUE, processed_at = NOW()
		WHERE stripe_event_id = $1
	`, eventID)
	return err
}

// processEvent handles the specific event type
func (h *WebhookHandler) processEvent(ctx context.Context, event *stripe.Event) error {
	switch event.Type {
	case "customer.subscription.created":
		return h.handleSubscriptionCreated(ctx, event)
	case "customer.subscription.updated":
		return h.handleSubscriptionUpdated(ctx, event)
	case "customer.subscription.deleted":
		return h.handleSubscriptionDeleted(ctx, event)
	case "invoice.paid":
		return h.handleInvoicePaid(ctx, event)
	case "invoice.payment_failed":
		return h.handleInvoicePaymentFailed(ctx, event)
	case "customer.created":
		// No action needed - we create customers ourselves
		return nil
	default:
		// Unknown event type - log but don't error
		fmt.Printf("unhandled webhook event type: %s\n", event.Type)
		return nil
	}
}

// handleSubscriptionCreated handles new subscription creation
func (h *WebhookHandler) handleSubscriptionCreated(ctx context.Context, event *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	if h.db == nil {
		return nil
	}

	// Find user by Stripe customer ID and update subscription
	_, err := h.db.ExecContext(ctx, `
		UPDATE users
		SET stripe_subscription_id = $1, plan = $2, updated_at = NOW()
		WHERE stripe_customer_id = $3
	`, sub.ID, determinePlan(&sub), sub.Customer.ID)
	if err != nil {
		return fmt.Errorf("update user subscription: %w", err)
	}

	return nil
}

// handleSubscriptionUpdated handles subscription updates
func (h *WebhookHandler) handleSubscriptionUpdated(ctx context.Context, event *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	if h.db == nil {
		return nil
	}

	// Update user's plan based on subscription status
	plan := determinePlan(&sub)
	if sub.Status == stripe.SubscriptionStatusCanceled ||
		sub.Status == stripe.SubscriptionStatusUnpaid {
		plan = string(PlanFree)
	}

	_, err := h.db.ExecContext(ctx, `
		UPDATE users
		SET plan = $1, updated_at = NOW()
		WHERE stripe_subscription_id = $2
	`, plan, sub.ID)
	if err != nil {
		return fmt.Errorf("update user plan: %w", err)
	}

	return nil
}

// handleSubscriptionDeleted handles subscription cancellation
func (h *WebhookHandler) handleSubscriptionDeleted(ctx context.Context, event *stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return fmt.Errorf("unmarshal subscription: %w", err)
	}

	if h.db == nil {
		return nil
	}

	// Downgrade user to free plan
	_, err := h.db.ExecContext(ctx, `
		UPDATE users
		SET plan = 'free', stripe_subscription_id = NULL, updated_at = NOW()
		WHERE stripe_subscription_id = $1
	`, sub.ID)
	if err != nil {
		return fmt.Errorf("downgrade user: %w", err)
	}

	return nil
}

// handleInvoicePaid handles successful payment
func (h *WebhookHandler) handleInvoicePaid(ctx context.Context, event *stripe.Event) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	// Reset bandwidth counter on successful payment for the billing period
	if h.db == nil {
		return nil
	}

	// Find user and reset their monthly bandwidth
	_, err := h.db.ExecContext(ctx, `
		UPDATE users
		SET bandwidth_used_bytes = 0, bandwidth_reset_at = NOW(), updated_at = NOW()
		WHERE stripe_customer_id = $1
	`, invoice.Customer.ID)
	if err != nil {
		return fmt.Errorf("reset bandwidth: %w", err)
	}

	return nil
}

// handleInvoicePaymentFailed handles failed payment
func (h *WebhookHandler) handleInvoicePaymentFailed(ctx context.Context, event *stripe.Event) error {
	var invoice stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return fmt.Errorf("unmarshal invoice: %w", err)
	}

	// Log failed payment - in production, you'd send an email notification
	fmt.Printf("payment failed for customer: %s, invoice: %s\n",
		invoice.Customer.ID, invoice.ID)

	return nil
}

// determinePlan determines the plan type from a subscription
func determinePlan(sub *stripe.Subscription) string {
	if sub.Status != stripe.SubscriptionStatusActive &&
		sub.Status != stripe.SubscriptionStatusTrialing {
		return string(PlanFree)
	}

	// Check subscription items for plan type
	// This is a simplified version - in production, you'd check price IDs
	if len(sub.Items.Data) > 0 {
		item := sub.Items.Data[0]
		if item.Price != nil && item.Price.Recurring != nil {
			// If it's a metered price, it's PAYG
			if item.Price.Recurring.UsageType == stripe.PriceRecurringUsageTypeMetered {
				return string(PlanPAYG)
			}
			// Otherwise it's Pro (monthly fixed price)
			return string(PlanPro)
		}
	}

	return string(PlanPAYG)
}
