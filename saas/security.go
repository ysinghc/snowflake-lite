package saas

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// TenantTier represents the service level agreement tier.
type TenantTier string

const (
	TierFree       TenantTier = "free"       // 100 req/sec
	TierPro        TenantTier = "pro"        // 5,000 req/sec
	TierEnterprise TenantTier = "enterprise" // 100,000 req/sec
)

// APIKeyMetadata holds tenant metadata and security scopes.
type APIKeyMetadata struct {
	KeyHash     string     `json:"keyHash"`
	TenantID    string     `json:"tenantId"`
	Tier        TenantTier `json:"tier"`
	RateLimit   int        `json:"rateLimit"` // Requests per second
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// AuditLogEntry records security and compliance events.
type AuditLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"eventType"`
	ClientIP  string    `json:"clientIp"`
	TenantID  string    `json:"tenantId,omitempty"`
	Detail    string    `json:"detail"`
}

// TokenBucket manages atomic rate limiting state per client key or IP.
type TokenBucket struct {
	capacity   float64
	tokens     float64
	refillRate float64 // tokens per second
	lastRefill time.Time
}

// SecurityManager coordinates authentication, rate limiting, and threat detection.
type SecurityManager struct {
	mu           sync.RWMutex
	apiKeys      map[string]*APIKeyMetadata // keyHash -> metadata
	blockedIPs   map[string]time.Time       // ip -> block expiry
	rateLimiters map[string]*TokenBucket    // identifier -> bucket
	ipVelocity   map[string][]time.Time     // ip -> timestamps of recent requests
	auditLogs    []AuditLogEntry
	maxAuditLogs int
}

// NewSecurityManager initializes the security control system.
func NewSecurityManager() *SecurityManager {
	sm := &SecurityManager{
		apiKeys:      make(map[string]*APIKeyMetadata),
		blockedIPs:   make(map[string]time.Time),
		rateLimiters: make(map[string]*TokenBucket),
		ipVelocity:   make(map[string][]time.Time),
		auditLogs:    make([]AuditLogEntry, 0, 1000),
		maxAuditLogs: 1000,
	}

	// Seed default demo API keys
	sm.CreateAPIKey("dev_default", TierFree, []string{"ids:read", "ids:batch"})
	sm.CreateAPIKey("enterprise_demo", TierEnterprise, []string{"ids:read", "ids:batch", "admin:all"})

	return sm
}

// GenerateRawKey produces a new secure key string (sk_live_...).
func GenerateRawKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return fmt.Sprintf("sk_live_%s", hex.EncodeToString(b))
}

// HashKey computes SHA-256 checksum of raw API key.
func HashKey(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

// CreateAPIKey generates and registers a new API Key for a tenant.
func (sm *SecurityManager) CreateAPIKey(tenantID string, tier TenantTier, permissions []string) (string, *APIKeyMetadata) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	rawKey := fmt.Sprintf("sk_live_%s_%s", tier, GenerateRawKey()[8:24])
	hash := HashKey(rawKey)

	limit := 100
	switch tier {
	case TierPro:
		limit = 5000
	case TierEnterprise:
		limit = 100000
	}

	meta := &APIKeyMetadata{
		KeyHash:     hash,
		TenantID:    tenantID,
		Tier:        tier,
		RateLimit:   limit,
		Permissions: permissions,
		CreatedAt:   time.Now().UTC(),
	}

	sm.apiKeys[hash] = meta
	sm.addAuditLogLocked("API_KEY_CREATED", "system", tenantID, fmt.Sprintf("Created %s key with limit %d req/s", tier, limit))
	return rawKey, meta
}

// ValidateKey checks an incoming raw API Key.
func (sm *SecurityManager) ValidateKey(rawKey string) (*APIKeyMetadata, bool) {
	if rawKey == "" {
		return nil, false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	targetHash := HashKey(rawKey)
	meta, exists := sm.apiKeys[targetHash]
	if !exists {
		return nil, false
	}

	// Constant time comparison
	if subtle.ConstantTimeCompare([]byte(meta.KeyHash), []byte(targetHash)) == 1 {
		return meta, true
	}
	return nil, false
}

// IsIPBlocked returns true if an IP is currently blacklisted.
func (sm *SecurityManager) IsIPBlocked(ip string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	expiry, blocked := sm.blockedIPs[ip]
	if !blocked {
		return false
	}
	if time.Now().After(expiry) {
		delete(sm.blockedIPs, ip)
		return false
	}
	return true
}

// BlockIP manually or automatically adds an IP to the blocklist.
func (sm *SecurityManager) BlockIP(ip string, duration time.Duration, reason string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	expiry := time.Now().Add(duration)
	sm.blockedIPs[ip] = expiry
	sm.addAuditLogLocked("IP_BLOCKED", ip, "", fmt.Sprintf("Blocked until %s. Reason: %s", expiry.Format(time.RFC3339), reason))
}

// UnblockIP removes an IP from the blocklist.
func (sm *SecurityManager) UnblockIP(ip string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.blockedIPs, ip)
	sm.addAuditLogLocked("IP_UNBLOCKED", ip, "", "Manually unblocked by admin")
}

// ListBlockedIPs returns active blocklist entries.
func (sm *SecurityManager) ListBlockedIPs() map[string]time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	res := make(map[string]time.Time)
	now := time.Now()
	for ip, exp := range sm.blockedIPs {
		if exp.After(now) {
			res[ip] = exp
		}
	}
	return res
}

// AllowRequest enforces token bucket rate limiting and IP velocity checking.
func (sm *SecurityManager) AllowRequest(identifier string, clientIP string, limitRPS int) (bool, int, time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()

	// 1. IP Velocity check for abuse protection
	if clientIP != "" {
		stamps := sm.ipVelocity[clientIP]
		// Retain timestamps within last 10 seconds
		cutoff := now.Add(-10 * time.Second)
		validStamps := make([]time.Time, 0, len(stamps)+1)
		for _, t := range stamps {
			if t.After(cutoff) {
				validStamps = append(validStamps, t)
			}
		}
		validStamps = append(validStamps, now)
		sm.ipVelocity[clientIP] = validStamps

		// If IP makes > 500 requests in 10s without elevated key, trigger auto-block
		if len(validStamps) > 500 && limitRPS <= 100 {
			sm.blockedIPs[clientIP] = now.Add(15 * time.Minute)
			sm.addAuditLogLocked("ABUSE_AUTO_BLOCK", clientIP, "", "IP velocity exceeded 500 req / 10s. Auto-blocked for 15m.")
			return false, 0, 15 * time.Minute
		}
	}

	// 2. Token Bucket Rate Limiter
	tb, exists := sm.rateLimiters[identifier]
	if !exists {
		tb = &TokenBucket{
			capacity:   float64(limitRPS),
			tokens:     float64(limitRPS),
			refillRate: float64(limitRPS),
			lastRefill: now,
		}
		sm.rateLimiters[identifier] = tb
	}

	// Calculate refilled tokens
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}
	tb.lastRefill = now

	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		remaining := int(tb.tokens)
		return true, remaining, 0
	}

	// Rate limit hit
	resetIn := time.Duration((1.0 - tb.tokens) / tb.refillRate * float64(time.Second))
	sm.addAuditLogLocked("RATE_LIMIT_EXCEEDED", clientIP, identifier, fmt.Sprintf("Limit %d RPS hit. Retry in %v", limitRPS, resetIn))
	return false, 0, resetIn
}

// AddAuditLog appends an audit record safely.
func (sm *SecurityManager) AddAuditLog(eventType, clientIP, tenantID, detail string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.addAuditLogLocked(eventType, clientIP, tenantID, detail)
}

func (sm *SecurityManager) addAuditLogLocked(eventType, clientIP, tenantID, detail string) {
	entry := AuditLogEntry{
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		ClientIP:  clientIP,
		TenantID:  tenantID,
		Detail:    detail,
	}

	if len(sm.auditLogs) >= sm.maxAuditLogs {
		sm.auditLogs = sm.auditLogs[1:]
	}
	sm.auditLogs = append(sm.auditLogs, entry)
}

// GetAuditLogs returns latest security audit logs.
func (sm *SecurityManager) GetAuditLogs(limit int) []AuditLogEntry {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if limit <= 0 || limit > len(sm.auditLogs) {
		limit = len(sm.auditLogs)
	}

	start := len(sm.auditLogs) - limit
	out := make([]AuditLogEntry, limit)
	copy(out, sm.auditLogs[start:])
	return out
}
