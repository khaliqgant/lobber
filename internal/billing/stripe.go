// internal/billing/stripe.go
package billing

import (
	"fmt"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/usagerecord"
)

// StripeClient wraps Stripe API operations
type StripeClient struct {
	apiKey string
}

// NewStripeClient creates a new Stripe client
func NewStripeClient(apiKey string) *StripeClient {
	stripe.Key = apiKey
	return &StripeClient{apiKey: apiKey}
}

// CreateCustomer creates a new Stripe customer
func (c *StripeClient) CreateCustomer(email, name string) (string, error) {
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
		Name:  stripe.String(name),
	}

	cust, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}

	return cust.ID, nil
}

// GetCustomer retrieves a Stripe customer by ID
func (c *StripeClient) GetCustomer(customerID string) (*stripe.Customer, error) {
	cust, err := customer.Get(customerID, nil)
	if err != nil {
		return nil, fmt.Errorf("get customer: %w", err)
	}
	return cust, nil
}

// UpdateCustomer updates a Stripe customer's information
func (c *StripeClient) UpdateCustomer(customerID, email, name string) error {
	params := &stripe.CustomerParams{}
	if email != "" {
		params.Email = stripe.String(email)
	}
	if name != "" {
		params.Name = stripe.String(name)
	}

	_, err := customer.Update(customerID, params)
	if err != nil {
		return fmt.Errorf("update customer: %w", err)
	}
	return nil
}

// CreateMeteredSubscription creates a subscription with metered billing
func (c *StripeClient) CreateMeteredSubscription(customerID, priceID string) (*stripe.Subscription, error) {
	params := &stripe.SubscriptionParams{
		Customer: stripe.String(customerID),
		Items: []*stripe.SubscriptionItemsParams{
			{
				Price: stripe.String(priceID),
			},
		},
	}

	sub, err := subscription.New(params)
	if err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}

	return sub, nil
}

// GetSubscription retrieves a subscription by ID
func (c *StripeClient) GetSubscription(subscriptionID string) (*stripe.Subscription, error) {
	sub, err := subscription.Get(subscriptionID, nil)
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return sub, nil
}

// CancelSubscription cancels a subscription
func (c *StripeClient) CancelSubscription(subscriptionID string) error {
	_, err := subscription.Cancel(subscriptionID, nil)
	if err != nil {
		return fmt.Errorf("cancel subscription: %w", err)
	}
	return nil
}

// ReportUsage reports bandwidth usage to Stripe for metered billing
// bytes is the amount of data transferred
func (c *StripeClient) ReportUsage(subscriptionItemID string, bytes int64) error {
	// Convert bytes to MB for billing (minimum 1 MB)
	mbUsed := bytes / (1024 * 1024)
	if mbUsed == 0 && bytes > 0 {
		mbUsed = 1 // Minimum 1 MB if any data transferred
	}

	if mbUsed == 0 {
		return nil // No usage to report
	}

	params := &stripe.UsageRecordParams{
		SubscriptionItem: stripe.String(subscriptionItemID),
		Quantity:         stripe.Int64(mbUsed),
		Action:           stripe.String(string(stripe.UsageRecordActionIncrement)),
	}

	_, err := usagerecord.New(params)
	if err != nil {
		return fmt.Errorf("report usage: %w", err)
	}

	return nil
}

// BytesToGB converts bytes to gigabytes (for display)
func BytesToGB(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024 * 1024)
}

// GBToBytes converts gigabytes to bytes
func GBToBytes(gb float64) int64 {
	return int64(gb * 1024 * 1024 * 1024)
}
