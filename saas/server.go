package saas

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ServerConfig specifies startup parameters for the SaaS server.
type ServerConfig struct {
	Addr         string
	DatacenterID uint64
	MachineID    uint64
	PodName      string
	WebDir       string
	CustomConfig CustomBitConfig
}

// Server encapsulates the complete distributed ID SaaS backend.
type Server struct {
	cfg      ServerConfig
	engine   *MultiEngine
	security *SecurityManager
	metrics  *MetricsCollector
	mux      *http.ServeMux
}

// NewServer initializes SaaS API Server components.
func NewServer(cfg ServerConfig) (*Server, error) {
	if cfg.PodName == "" {
		if host, err := os.Hostname(); err == nil && host != "" {
			cfg.PodName = host
		} else {
			cfg.PodName = "snowflake-worker-0"
		}
	}
	if cfg.WebDir == "" {
		cfg.WebDir = "web"
	}

	engine, err := NewMultiEngine(cfg.DatacenterID, cfg.MachineID, cfg.CustomConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine: %w", err)
	}

	sec := NewSecurityManager()
	met := NewMetricsCollector(cfg.PodName, cfg.DatacenterID, cfg.MachineID)

	s := &Server{
		cfg:      cfg,
		engine:   engine,
		security: sec,
		metrics:  met,
		mux:      http.NewServeMux(),
	}

	s.routes()
	return s, nil
}

// SecurityManager returns the underlying security controller.
func (s *Server) SecurityManager() *SecurityManager { return s.security }

// Metrics returns the underlying metrics collector.
func (s *Server) Metrics() *MetricsCollector { return s.metrics }

// Engine returns the underlying multi-format ID engine.
func (s *Server) Engine() *MultiEngine { return s.engine }

func (s *Server) routes() {
	// Health & Liveness
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/readyz", s.handleReady)

	// Developer API
	s.mux.HandleFunc("/api/v1/ids/generate", s.withSecurity(s.handleGenerateID, "ids:read"))
	s.mux.HandleFunc("/api/v1/ids/batch", s.withSecurity(s.handleBatchGenerate, "ids:batch"))
	s.mux.HandleFunc("/api/v1/ids/decode", s.handleDecodeID)
	s.mux.HandleFunc("/api/v1/auth/keys", s.handleCreateKey)

	// Admin Dashboard API
	s.mux.HandleFunc("/api/v1/admin/metrics", s.handleAdminMetrics)
	s.mux.HandleFunc("/api/v1/admin/nodes", s.handleAdminNodes)
	s.mux.HandleFunc("/api/v1/admin/rebalance", s.handleAdminRebalance)
	s.mux.HandleFunc("/api/v1/admin/security/audit", s.handleAdminAuditLogs)
	s.mux.HandleFunc("/api/v1/admin/security/blocklist", s.handleAdminBlocklist)
	s.mux.HandleFunc("/api/v1/admin/security/block", s.handleAdminBlockIP)

	// Static Web Frontend
	fs := http.FileServer(http.Dir(s.cfg.WebDir))
	s.mux.Handle("/", fs)
}

// ListenAndServe launches the HTTP SaaS server.
func (s *Server) ListenAndServe() error {
	server := &http.Server{
		Addr:         s.cfg.Addr,
		Handler:      s.securityHeadersMiddleware(s.mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	fmt.Printf("🚀 Enterprise Distributed ID SaaS serving on http://%s  (Pod: %s, Machine: %d, DC: %d)\n",
		s.cfg.Addr, s.cfg.PodName, s.cfg.MachineID, s.cfg.DatacenterID)
	return server.ListenAndServe()
}

// ServeHTTP implements http.Handler for testing.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.securityHeadersMiddleware(s.mux).ServeHTTP(w, r)
}

// --- Middleware ---

func (s *Server) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (s *Server) withSecurity(handler http.HandlerFunc, requiredPermission string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := getClientIP(r)

		// 1. IP Blocklist check
		if s.security.IsIPBlocked(clientIP) {
			s.writeError(w, http.StatusForbidden, "IP_BLOCKED", "Access denied: IP address is blacklisted due to security policy")
			return
		}

		// 2. Extract API Key (Header "X-API-Key" or "Authorization: Bearer <key>")
		rawKey := r.Header.Get("X-API-Key")
		if rawKey == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				rawKey = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		// Allow unauthenticated demo requests with strict lower rate limit
		var limitRPS = 50
		var identifier = "ip:" + clientIP
		var tenantID = "anonymous"

		if rawKey != "" {
			meta, valid := s.security.ValidateKey(rawKey)
			if !valid {
				s.security.AddAuditLog("AUTH_FAILED", clientIP, "", "Invalid API key attempt")
				s.writeError(w, http.StatusUnauthorized, "INVALID_API_KEY", "Provided API Key is invalid or revoked")
				return
			}
			limitRPS = meta.RateLimit
			identifier = "key:" + meta.KeyHash[:16]
			tenantID = meta.TenantID
		}

		// 3. Token Bucket Rate Limit check
		allowed, remaining, resetIn := s.security.AllowRequest(identifier, clientIP, limitRPS)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limitRPS))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if !allowed {
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(resetIn).Unix(), 10))
			w.Header().Set("Retry-After", strconv.Itoa(int(resetIn.Seconds())+1))
			s.writeError(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED",
				fmt.Sprintf("Rate limit exceeded. Maximum %d requests/sec allowed for tier.", limitRPS))
			return
		}

		_ = tenantID
		handler(w, r)
	}
}

// --- Route Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"pod":       s.cfg.PodName,
		"machineId": s.cfg.MachineID,
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]bool{"ready": true})
}

func (s *Server) handleGenerateID(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	format := IDFormat(q.Get("format"))
	prefix := q.Get("prefix")
	countStr := q.Get("count")

	count := 1
	if countStr != "" {
		if c, err := strconv.Atoi(countStr); err == nil && c > 0 {
			count = c
		}
	}
	if count > 100000 {
		s.writeError(w, http.StatusBadRequest, "INVALID_COUNT", "Maximum batch count is 100,000")
		return
	}

	if count == 1 {
		id, err := s.engine.Generate(format, prefix)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "GENERATION_ERROR", err.Error())
			return
		}
		s.metrics.RecordGeneration(string(format), 1, time.Since(start))
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":        id,
			"format":    format,
			"pod":       s.cfg.PodName,
			"machineId": s.cfg.MachineID,
		})
		return
	}

	ids, err := s.engine.GenerateBatch(format, prefix, count)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GENERATION_ERROR", err.Error())
		return
	}
	s.metrics.RecordGeneration(string(format), uint64(count), time.Since(start))
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ids":       ids,
		"count":     len(ids),
		"format":    format,
		"pod":       s.cfg.PodName,
		"machineId": s.cfg.MachineID,
	})
}

type batchRequest struct {
	Format IDFormat `json:"format"`
	Prefix string   `json:"prefix"`
	Count  int      `json:"count"`
}

func (s *Server) handleBatchGenerate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use POST for batch generation")
		return
	}

	var req struct {
		Format string `json:"format"`
		Prefix string `json:"prefix"`
		Count  int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100000 {
		s.writeError(w, http.StatusBadRequest, "INVALID_COUNT", "Maximum batch count is 100,000")
		return
	}

	ids, err := s.engine.GenerateBatch(IDFormat(req.Format), req.Prefix, req.Count)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GENERATION_ERROR", err.Error())
		return
	}

	s.metrics.RecordGeneration(req.Format, uint64(req.Count), time.Since(start))
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"ids":       ids,
		"count":     len(ids),
		"format":    req.Format,
		"pod":       s.cfg.PodName,
		"machineId": s.cfg.MachineID,
	})
}

func (s *Server) handleDecodeID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_ID", "Query parameter 'id' is required")
		return
	}

	decoded, err := s.engine.DecodeID(id)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "DECODE_ERROR", err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, decoded)
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Use POST")
		return
	}

	var req struct {
		TenantID string `json:"tenantId"`
		Tier     string `json:"tier"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.TenantID == "" {
		req.TenantID = "tenant_" + strconv.FormatInt(time.Now().UnixNano()%10000, 10)
	}

	tier := TenantTier(req.Tier)
	if tier != TierPro && tier != TierEnterprise {
		tier = TierFree
	}

	rawKey, meta := s.security.CreateAPIKey(req.TenantID, tier, []string{"ids:read", "ids:batch"})
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"apiKey":    rawKey,
		"tenantId":  meta.TenantID,
		"tier":      meta.Tier,
		"rateLimit": meta.RateLimit,
		"createdAt": meta.CreatedAt,
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	blockedCount := len(s.security.ListBlockedIPs())
	snapshot := s.metrics.GetSnapshot(blockedCount)
	s.writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleAdminNodes(w http.ResponseWriter, r *http.Request) {
	blockedCount := len(s.security.ListBlockedIPs())
	snap := s.metrics.GetSnapshot(blockedCount)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": snap.ClusterNodes,
		"count": len(snap.ClusterNodes),
	})
}

func (s *Server) handleAdminRebalance(w http.ResponseWriter, r *http.Request) {
	nodes := s.metrics.RebalanceCluster()
	s.security.AddAuditLog("CLUSTER_REBALANCED", getClientIP(r), "admin", "Triggered cluster worker ID rebalancing")
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "rebalancing_initiated",
		"message": "Dynamic worker ID assignment rebalancing initiated across pods",
		"nodes":   nodes,
	})
}

func (s *Server) handleAdminAuditLogs(w http.ResponseWriter, r *http.Request) {
	logs := s.security.GetAuditLogs(50)
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":  logs,
		"count": len(logs),
	})
}

func (s *Server) handleAdminBlocklist(w http.ResponseWriter, r *http.Request) {
	ips := s.security.ListBlockedIPs()
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"blockedIPs": ips,
		"count":      len(ips),
	})
}

func (s *Server) handleAdminBlockIP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IP     string `json:"ip"`
		Action string `json:"action"` // "block" or "unblock"
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "Invalid JSON payload")
		return
	}

	if req.IP == "" {
		s.writeError(w, http.StatusBadRequest, "MISSING_IP", "IP field required")
		return
	}

	if req.Action == "unblock" {
		s.security.UnblockIP(req.IP)
		s.writeJSON(w, http.StatusOK, map[string]string{"message": "IP unblocked successfully"})
		return
	}

	s.security.BlockIP(req.IP, 1*time.Hour, req.Reason)
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "IP blocked successfully for 1 hour"})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, msg string) {
	s.writeJSON(w, status, map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": msg,
		},
	})
}
