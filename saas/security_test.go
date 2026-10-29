package saas

import (
	"testing"
	"time"
)

func TestSecurityManager_APIKey(t *testing.T) {
	sm := NewSecurityManager()

	rawKey, meta := sm.CreateAPIKey("tenant_test", TierPro, []string{"ids:read"})
	if rawKey == "" {
		t.Fatal("expected non-empty raw key")
	}

	metaValid, ok := sm.ValidateKey(rawKey)
	if !ok {
		t.Fatal("expected valid API key lookup")
	}
	if metaValid.TenantID != "tenant_test" {
		t.Errorf("expected tenant 'tenant_test', got %s", metaValid.TenantID)
	}
	if meta.RateLimit != 5000 {
		t.Errorf("expected rate limit 5000 for Pro tier, got %d", meta.RateLimit)
	}

	_, invalid := sm.ValidateKey("sk_live_bogus_key_1234")
	if invalid {
		t.Fatal("expected invalid lookup for bogus key")
	}
}

func TestSecurityManager_RateLimiter(t *testing.T) {
	sm := NewSecurityManager()
	ip := "192.168.1.50"

	// Allow first 5 requests with limit 5
	for i := 0; i < 5; i++ {
		allowed, remaining, _ := sm.AllowRequest("test_limiter", ip, 5)
		if !allowed {
			t.Fatalf("request %d should be allowed, remaining: %d", i+1, remaining)
		}
	}

	// 6th request should fail
	allowed, _, resetIn := sm.AllowRequest("test_limiter", ip, 5)
	if allowed {
		t.Fatal("expected rate limit to be triggered on 6th request")
	}
	if resetIn <= 0 {
		t.Errorf("expected positive reset duration, got %v", resetIn)
	}
}

func TestSecurityManager_Blocklist(t *testing.T) {
	sm := NewSecurityManager()
	testIP := "10.0.0.99"

	if sm.IsIPBlocked(testIP) {
		t.Fatal("IP should not be blocked initially")
	}

	sm.BlockIP(testIP, 1*time.Hour, "Security Test")
	if !sm.IsIPBlocked(testIP) {
		t.Fatal("IP should be blocked after BlockIP call")
	}

	sm.UnblockIP(testIP)
	if sm.IsIPBlocked(testIP) {
		t.Fatal("IP should be unblocked after UnblockIP call")
	}
}
