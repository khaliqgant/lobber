// web/dashboard/handler_test.go
package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewHandler(t *testing.T) {
	// Should work without a database
	h, err := NewHandler(nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestRequireAuthRedirect(t *testing.T) {
	h, err := NewHandler(nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	// Request without session should redirect
	req := httptest.NewRequest("GET", "/dashboard", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	h, err := NewHandler(nil)
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/dashboard/logout", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Should redirect
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", rec.Code)
	}

	// Check cookie is cleared
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session" {
			found = true
			if c.MaxAge != -1 {
				t.Errorf("Expected MaxAge -1 to delete cookie, got %d", c.MaxAge)
			}
			if c.Value != "" {
				t.Errorf("Expected empty cookie value, got %s", c.Value)
			}
		}
	}
	if !found {
		t.Error("Expected session cookie to be set (for deletion)")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{5368709120, "5.00 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%d) = %s, want %s", tt.bytes, result, tt.expected)
		}
	}
}

func TestFormatTime(t *testing.T) {
	testTime := time.Date(2025, 12, 5, 15, 30, 0, 0, time.UTC)
	result := formatTime(testTime)
	expected := "Dec 5, 2025 3:30 PM"
	if result != expected {
		t.Errorf("formatTime() = %s, want %s", result, expected)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{500 * time.Microsecond, "<1ms"},
		{1 * time.Millisecond, "1ms"},
		{50 * time.Millisecond, "50ms"},
		{1500 * time.Millisecond, "1.5s"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.duration)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %s, want %s", tt.duration, result, tt.expected)
		}
	}
}

func TestUserStruct(t *testing.T) {
	u := User{
		ID:        "user-123",
		Email:     "test@example.com",
		Name:      "Test User",
		Plan:      "pro",
		AvatarURL: "https://example.com/avatar.png",
	}

	if u.ID != "user-123" {
		t.Errorf("User.ID = %s, want user-123", u.ID)
	}
	if u.Email != "test@example.com" {
		t.Errorf("User.Email = %s, want test@example.com", u.Email)
	}
	if u.Plan != "pro" {
		t.Errorf("User.Plan = %s, want pro", u.Plan)
	}
}

func TestUsageSummaryStruct(t *testing.T) {
	summary := UsageSummary{
		UsedBytes:   1073741824, // 1 GB
		LimitBytes:  5368709120, // 5 GB
		UsedGB:      1.0,
		LimitGB:     5.0,
		PercentUsed: 20.0,
		OverLimit:   false,
	}

	if summary.PercentUsed != 20.0 {
		t.Errorf("UsageSummary.PercentUsed = %f, want 20.0", summary.PercentUsed)
	}
	if summary.OverLimit {
		t.Error("UsageSummary.OverLimit should be false")
	}
}

func TestDomainStruct(t *testing.T) {
	d := Domain{
		ID:        "domain-123",
		Name:      "test.example.com",
		Verified:  true,
		CreatedAt: time.Now(),
	}

	if d.Name != "test.example.com" {
		t.Errorf("Domain.Name = %s, want test.example.com", d.Name)
	}
	if !d.Verified {
		t.Error("Domain.Verified should be true")
	}
}

func TestRequestLogStruct(t *testing.T) {
	log := RequestLog{
		ID:         "log-123",
		Method:     "GET",
		Path:       "/api/users",
		StatusCode: 200,
		Duration:   50 * time.Millisecond,
		Domain:     "test.example.com",
		CreatedAt:  time.Now(),
	}

	if log.Method != "GET" {
		t.Errorf("RequestLog.Method = %s, want GET", log.Method)
	}
	if log.StatusCode != 200 {
		t.Errorf("RequestLog.StatusCode = %d, want 200", log.StatusCode)
	}
}
