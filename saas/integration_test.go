package saas

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestIntegration_SaaSPlatform(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Addr:         ":0",
		DatacenterID: 0,
		MachineID:    5,
		PodName:      "snowflake-worker-5",
		WebDir:       "../web",
		CustomConfig: DefaultCustomBitConfig(),
	})
	if err != nil {
		t.Fatalf("failed to initialize SaaS server: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 1. Create API Key for Enterprise Tenant
	keyResp, err := http.Post(ts.URL+"/api/v1/auth/keys", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/v1/auth/keys failed: %v", err)
	}
	if keyResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created, got %d", keyResp.StatusCode)
	}

	var keyData struct {
		APIKey    string `json:"apiKey"`
		RateLimit int    `json:"rateLimit"`
	}
	_ = json.NewDecoder(keyResp.Body).Decode(&keyData)
	keyResp.Body.Close()

	if keyData.APIKey == "" {
		t.Fatal("expected valid API key string")
	}

	// 2. Generate Single ID with API Key
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/ids/generate?format=snowflake&prefix=usr_", nil)
	req.Header.Set("X-API-Key", keyData.APIKey)
	genResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/ids/generate failed: %v", err)
	}
	if genResp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200 OK, got %d", genResp.StatusCode)
	}

	var genData struct {
		ID     string `json:"id"`
		Format string `json:"format"`
	}
	_ = json.NewDecoder(genResp.Body).Decode(&genData)
	genResp.Body.Close()

	if genData.ID == "" || genData.Format != "snowflake" {
		t.Fatalf("invalid generation response: %+v", genData)
	}

	// 3. Decode Generated ID
	decResp, err := http.Get(ts.URL + "/api/v1/ids/decode?id=" + genData.ID)
	if err != nil || decResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/ids/decode failed: %v", err)
	}
	var decData DecodedID
	_ = json.NewDecoder(decResp.Body).Decode(&decData)
	decResp.Body.Close()

	if decData.MachineID != 5 {
		t.Errorf("expected machine ID 5, got %d", decData.MachineID)
	}

	// 4. Admin Metrics Verification
	metResp, err := http.Get(ts.URL + "/api/v1/admin/metrics")
	if err != nil || metResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/admin/metrics failed: %v", err)
	}
	var snap SystemMetricsSnapshot
	_ = json.NewDecoder(metResp.Body).Decode(&snap)
	metResp.Body.Close()

	if snap.TotalIDsGenerated == 0 {
		t.Error("expected non-zero total IDs generated in metrics snapshot")
	}
}

func TestIntegration_HighConcurrencyZeroCollisions(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Addr:         ":0",
		DatacenterID: 0,
		MachineID:    10,
		PodName:      "snowflake-worker-10",
		WebDir:       "../web",
		CustomConfig: DefaultCustomBitConfig(),
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Register Enterprise key with 100,000 req/sec quota
	entKey, _ := srv.SecurityManager().CreateAPIKey("high_concurrency_tenant", TierEnterprise, []string{"ids:read", "ids:batch"})

	// High concurrency: 50 goroutines generating 200 IDs each = 10,000 parallel IDs
	const numGoroutines = 50
	const idsPerRoutine = 200

	var wg sync.WaitGroup
	idChan := make(chan string, numGoroutines*idsPerRoutine)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			client := &http.Client{}
			for i := 0; i < idsPerRoutine; i++ {
				url := fmt.Sprintf("%s/api/v1/ids/generate?format=snowflake", ts.URL)
				req, _ := http.NewRequest("GET", url, nil)
				req.Header.Set("X-API-Key", entKey)
				resp, err := client.Do(req)
				if err != nil {
					t.Errorf("routine %d request error: %v", routineID, err)
					return
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				var res struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(body, &res); err == nil && res.ID != "" {
					idChan <- res.ID
				}
			}
		}(g)
	}

	wg.Wait()
	close(idChan)

	// Verify complete set uniqueness
	seen := make(map[string]bool)
	totalCollected := 0
	for id := range idChan {
		totalCollected++
		if seen[id] {
			t.Fatalf("CRITICAL: Collision detected for ID %s!", id)
		}
		seen[id] = true
	}

	expectedTotal := numGoroutines * idsPerRoutine
	if totalCollected != expectedTotal {
		t.Fatalf("expected %d IDs collected, got %d", expectedTotal, totalCollected)
	}
}

func TestIntegration_RateLimitEnforcement(t *testing.T) {
	srv, err := NewServer(ServerConfig{
		Addr:         ":0",
		DatacenterID: 0,
		MachineID:    2,
		PodName:      "snowflake-worker-2",
		WebDir:       "../web",
		CustomConfig: DefaultCustomBitConfig(),
	})
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// Free key has limit 100 req/sec
	freeKey, _ := srv.SecurityManager().CreateAPIKey("free_tenant", TierFree, []string{"ids:read"})

	client := &http.Client{}
	hitLimit := false

	// Make 150 requests rapidly
	for i := 0; i < 150; i++ {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/ids/generate?format=snowflake", nil)
		req.Header.Set("X-API-Key", freeKey)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			hitLimit = true
			if resp.Header.Get("Retry-After") == "" {
				t.Error("expected Retry-After header on 429 response")
			}
			break
		}
	}

	if !hitLimit {
		t.Fatal("expected 429 Too Many Requests status code when exceeding Free tier quota")
	}
}
