// web/dashboard/handler.go
package dashboard

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/*.html
var content embed.FS

// User represents the logged-in user for templates
type User struct {
	ID        string
	Email     string
	Name      string
	Plan      string
	AvatarURL string
}

// Domain represents a user's registered domain
type Domain struct {
	ID        string
	Name      string
	Verified  bool
	CreatedAt time.Time
}

// UsageSummary holds bandwidth usage info
type UsageSummary struct {
	UsedBytes   int64
	LimitBytes  int64
	UsedGB      float64
	LimitGB     float64
	PercentUsed float64
	OverLimit   bool
}

// RequestLog represents a logged request
type RequestLog struct {
	ID         string
	Method     string
	Path       string
	StatusCode int
	Duration   time.Duration
	Domain     string
	CreatedAt  time.Time
}

// Handler serves the web dashboard
type Handler struct {
	db        *sql.DB
	templates *template.Template
	mux       *http.ServeMux
}

// NewHandler creates a new dashboard handler
func NewHandler(db *sql.DB) (*Handler, error) {
	// Parse templates
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatBytes":    formatBytes,
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
		"lower":          strings.ToLower,
	}).ParseFS(content, "templates/*.html")
	if err != nil {
		return nil, err
	}

	h := &Handler{
		db:        db,
		templates: tmpl,
		mux:       http.NewServeMux(),
	}

	// Routes
	h.mux.HandleFunc("/dashboard", h.requireAuth(h.handleDashboard))
	h.mux.HandleFunc("/dashboard/account", h.requireAuth(h.handleAccount))
	h.mux.HandleFunc("/dashboard/domains", h.requireAuth(h.handleDomains))
	h.mux.HandleFunc("/dashboard/logs", h.requireAuth(h.handleLogs))
	h.mux.HandleFunc("/dashboard/logout", h.handleLogout)

	return h, nil
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// requireAuth middleware checks for valid session
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := h.getUserFromSession(r)
		if user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		// Add user to context
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

type contextKey string

const userContextKey contextKey = "user"

// getUserFromSession retrieves user from session cookie
func (h *Handler) getUserFromSession(r *http.Request) *User {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}

	if h.db == nil {
		return nil
	}

	// Look up session in database
	var user User
	hashed := hashToken(cookie.Value)

	err = h.db.QueryRowContext(r.Context(), `
		SELECT u.id, u.email, COALESCE(u.name, ''), COALESCE(u.plan, 'free'), COALESCE(u.avatar_url, '')
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token_hash = $1 AND s.expires_at > NOW()
	`, hashed).Scan(&user.ID, &user.Email, &user.Name, &user.Plan, &user.AvatarURL)
	if err != nil {
		return nil
	}

	return &user
}

// handleDashboard renders the main dashboard page
func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*User)

	usage := h.getUserUsage(r.Context(), user.ID)
	domains := h.getUserDomains(r.Context(), user.ID)
	recentLogs := h.getRecentLogs(r.Context(), user.ID, 10)

	data := map[string]interface{}{
		"User":       user,
		"Usage":      usage,
		"Domains":    domains,
		"RecentLogs": recentLogs,
		"Page":       "dashboard",
	}

	h.render(w, "dashboard.html", data)
}

// handleAccount renders the account settings page
func (h *Handler) handleAccount(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*User)
	usage := h.getUserUsage(r.Context(), user.ID)

	data := map[string]interface{}{
		"User":  user,
		"Usage": usage,
		"Page":  "account",
	}

	h.render(w, "account.html", data)
}

// handleDomains renders the domain management page
func (h *Handler) handleDomains(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*User)
	domains := h.getUserDomains(r.Context(), user.ID)

	data := map[string]interface{}{
		"User":    user,
		"Domains": domains,
		"Page":    "domains",
	}

	// Handle HTMX partial requests
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "domains-list.html", data)
		return
	}

	h.render(w, "domains.html", data)
}

// handleLogs renders the request logs page
func (h *Handler) handleLogs(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userContextKey).(*User)
	logs := h.getRecentLogs(r.Context(), user.ID, 100)

	data := map[string]interface{}{
		"User": user,
		"Logs": logs,
		"Page": "logs",
	}

	// Handle HTMX partial requests
	if r.Header.Get("HX-Request") == "true" {
		h.render(w, "logs-list.html", data)
		return
	}

	h.render(w, "logs.html", data)
}

// handleLogout clears the session and redirects
func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// getUserUsage retrieves bandwidth usage for a user
func (h *Handler) getUserUsage(ctx context.Context, userID string) *UsageSummary {
	if h.db == nil {
		return &UsageSummary{}
	}

	var usedBytes int64
	err := h.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(bytes_in + bytes_out), 0)
		FROM bandwidth_usage
		WHERE user_id = $1
		AND recorded_at >= date_trunc('month', NOW())
	`, userID).Scan(&usedBytes)
	if err != nil {
		return &UsageSummary{}
	}

	// Get user plan for limit
	var plan string
	h.db.QueryRowContext(ctx, `SELECT COALESCE(plan, 'free') FROM users WHERE id = $1`, userID).Scan(&plan)

	var limitBytes int64 = 5 * 1024 * 1024 * 1024 // 5GB free tier
	if plan == "pro" || plan == "payg" {
		limitBytes = -1 // Unlimited
	}

	summary := &UsageSummary{
		UsedBytes:  usedBytes,
		LimitBytes: limitBytes,
		UsedGB:     float64(usedBytes) / (1024 * 1024 * 1024),
	}

	if limitBytes > 0 {
		summary.LimitGB = float64(limitBytes) / (1024 * 1024 * 1024)
		summary.PercentUsed = float64(usedBytes) / float64(limitBytes) * 100
		summary.OverLimit = usedBytes >= limitBytes
	}

	return summary
}

// getUserDomains retrieves domains for a user
func (h *Handler) getUserDomains(ctx context.Context, userID string) []Domain {
	if h.db == nil {
		return nil
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT id, hostname AS domain, verified, created_at
		FROM domains
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var domains []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Name, &d.Verified, &d.CreatedAt); err != nil {
			continue
		}
		domains = append(domains, d)
	}
	return domains
}

// getRecentLogs retrieves recent request logs for a user
func (h *Handler) getRecentLogs(ctx context.Context, userID string, limit int) []RequestLog {
	if h.db == nil {
		return nil
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT r.id, r.method, r.path, r.status_code, r.duration_ms, d.hostname AS domain, r.created_at
		FROM request_logs r
		JOIN domains d ON r.domain_id = d.id
		WHERE d.user_id = $1
		ORDER BY r.created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var logs []RequestLog
	for rows.Next() {
		var l RequestLog
		var durationMs int64
		if err := rows.Scan(&l.ID, &l.Method, &l.Path, &l.StatusCode, &durationMs, &l.Domain, &l.CreatedAt); err != nil {
			continue
		}
		l.Duration = time.Duration(durationMs) * time.Millisecond
		logs = append(logs, l)
	}
	return logs
}

// render executes a template
func (h *Handler) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// Template helper functions
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatTime(t time.Time) string {
	return t.Format("Jan 2, 2006 3:04 PM")
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return "<1ms"
	}
	return d.Truncate(time.Millisecond).String()
}

// hashToken returns a hex SHA256 hash for session token comparison
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}
