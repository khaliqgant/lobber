// internal/billing/billing_test.go
package billing

import (
	"testing"
)

func TestBytesToGB(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected float64
	}{
		{"zero bytes", 0, 0},
		{"1 GB", 1073741824, 1.0},
		{"5 GB", 5368709120, 5.0},
		{"1.5 GB", 1610612736, 1.5},
		{"500 MB", 524288000, 0.48828125},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BytesToGB(tt.bytes)
			if got != tt.expected {
				t.Errorf("BytesToGB(%d) = %v, want %v", tt.bytes, got, tt.expected)
			}
		})
	}
}

func TestGBToBytes(t *testing.T) {
	tests := []struct {
		name     string
		gb       float64
		expected int64
	}{
		{"zero", 0, 0},
		{"1 GB", 1.0, 1073741824},
		{"5 GB", 5.0, 5368709120},
		{"0.5 GB", 0.5, 536870912},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GBToBytes(tt.gb)
			if got != tt.expected {
				t.Errorf("GBToBytes(%v) = %v, want %v", tt.gb, got, tt.expected)
			}
		})
	}
}

func TestFreeTierConstant(t *testing.T) {
	// Verify free tier is 5GB
	expectedBytes := int64(5 * 1024 * 1024 * 1024)
	if FreeTierBytes != expectedBytes {
		t.Errorf("FreeTierBytes = %d, want %d (5GB)", FreeTierBytes, expectedBytes)
	}
}

func TestPlanConstants(t *testing.T) {
	if PlanFree != "free" {
		t.Errorf("PlanFree = %q, want %q", PlanFree, "free")
	}
	if PlanPAYG != "payg" {
		t.Errorf("PlanPAYG = %q, want %q", PlanPAYG, "payg")
	}
	if PlanPro != "pro" {
		t.Errorf("PlanPro = %q, want %q", PlanPro, "pro")
	}
}

func TestNewServiceWithoutStripe(t *testing.T) {
	// Service should work without Stripe configured
	svc := NewService(nil, "")
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.stripe != nil {
		t.Error("stripe client should be nil when no key provided")
	}
}

func TestNewServiceWithStripe(t *testing.T) {
	// Service should initialize Stripe client when key provided
	svc := NewService(nil, "sk_test_fake_key")
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.stripe == nil {
		t.Error("stripe client should not be nil when key provided")
	}
}

func TestServiceRecordBandwidthNoDB(t *testing.T) {
	// Should be a no-op without database
	svc := NewService(nil, "")
	err := svc.RecordBandwidth(nil, 1, 1, 100, 200)
	if err != nil {
		t.Errorf("RecordBandwidth without DB should not error, got: %v", err)
	}
}

func TestServiceGetUserUsageNoDB(t *testing.T) {
	// Should return 0 without database
	svc := NewService(nil, "")
	usage, err := svc.GetUserUsage(nil, 1)
	if err != nil {
		t.Errorf("GetUserUsage without DB should not error, got: %v", err)
	}
	if usage != 0 {
		t.Errorf("GetUserUsage without DB should return 0, got: %d", usage)
	}
}

func TestServiceCheckQuotaNoDB(t *testing.T) {
	// Should return within quota without database
	svc := NewService(nil, "")
	withinQuota, used, limit, err := svc.CheckQuota(nil, 1)
	if err != nil {
		t.Errorf("CheckQuota without DB should not error, got: %v", err)
	}
	if !withinQuota {
		t.Error("CheckQuota without DB should return withinQuota=true")
	}
	if used != 0 {
		t.Errorf("CheckQuota without DB should return used=0, got: %d", used)
	}
	if limit != FreeTierBytes {
		t.Errorf("CheckQuota without DB should return limit=FreeTierBytes, got: %d", limit)
	}
}

func TestServiceCreateCustomerNoStripe(t *testing.T) {
	// Should error without Stripe configured
	svc := NewService(nil, "")
	_, err := svc.CreateCustomerForUser(nil, 1, "test@example.com", "Test User")
	if err == nil {
		t.Error("CreateCustomerForUser without Stripe should error")
	}
}

func TestUsageSummaryFields(t *testing.T) {
	// Test that UsageSummary struct has expected fields
	summary := &UsageSummary{
		UserID:      1,
		Plan:        PlanFree,
		UsedBytes:   1073741824, // 1GB
		LimitBytes:  FreeTierBytes,
		UsedGB:      1.0,
		LimitGB:     5.0,
		PercentUsed: 20.0,
		OverLimit:   false,
	}

	if summary.UserID != 1 {
		t.Errorf("UserID = %d, want 1", summary.UserID)
	}
	if summary.Plan != PlanFree {
		t.Errorf("Plan = %v, want %v", summary.Plan, PlanFree)
	}
	if summary.PercentUsed != 20.0 {
		t.Errorf("PercentUsed = %v, want 20.0", summary.PercentUsed)
	}
}
