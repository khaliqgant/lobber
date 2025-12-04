package client

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sync"
	"time"
)

//go:embed static/*
var staticFiles embed.FS

type InspectedRequest struct {
	ID              string              `json:"id"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	StatusCode      int                 `json:"status_code"`
	RequestHeaders  map[string][]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string][]string `json:"response_headers,omitempty"`
	RequestBody     string              `json:"request_body,omitempty"`
	ResponseBody    string              `json:"response_body,omitempty"`
	DurationMs      int64               `json:"duration_ms"`
	Timestamp       time.Time           `json:"timestamp"`
}

type Inspector struct {
	mu       sync.RWMutex
	requests []*InspectedRequest
	maxSize  int
	mux      *http.ServeMux
}

func NewInspector() *Inspector {
	i := &Inspector{
		requests: make([]*InspectedRequest, 0, 100),
		maxSize:  100,
		mux:      http.NewServeMux(),
	}

	// API routes
	i.mux.HandleFunc("/api/requests", i.handleListRequests)
	i.mux.HandleFunc("/api/requests/", i.handleGetRequest)
	i.mux.HandleFunc("/api/replay/", i.handleReplay)

	// Static files
	staticFS, _ := fs.Sub(staticFiles, "static")
	i.mux.Handle("/", http.FileServer(http.FS(staticFS)))

	return i
}

func (i *Inspector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.mux.ServeHTTP(w, r)
}

func (i *Inspector) AddRequest(req *InspectedRequest) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if req.Timestamp.IsZero() {
		req.Timestamp = time.Now()
	}

	i.requests = append([]*InspectedRequest{req}, i.requests...)

	if len(i.requests) > i.maxSize {
		i.requests = i.requests[:i.maxSize]
	}
}

func (i *Inspector) handleListRequests(w http.ResponseWriter, r *http.Request) {
	i.mu.RLock()
	requests := make([]*InspectedRequest, len(i.requests))
	copy(requests, i.requests)
	i.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(requests)
}

func (i *Inspector) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	// TODO: Get single request by ID
	http.Error(w, "not implemented", http.StatusNotImplemented)
}

func (i *Inspector) handleReplay(w http.ResponseWriter, r *http.Request) {
	// TODO: Replay request
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
