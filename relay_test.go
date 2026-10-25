package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "test-token-with-at-least-thirty-two-characters"

const validRouteJSON = `{
  "type":"minimap_route",
  "version":"1.0.18",
  "available":true,
  "routeId":"route-123",
  "origin":{"latitude":10.0,"longitude":106.0},
  "destination":{"latitude":10.001,"longitude":106.001},
  "mode":"walk",
  "geometry":"??o}@o}@",
  "steps":[{"text":"Continue","fromIndex":0,"toIndex":1,"distance":157}],
  "distance":157,
  "time":120,
  "currentLocation":{"latitude":10.0,"longitude":106.0,"accuracy":5},
  "currentStep":0
}`

func newTestServer() *httptest.Server {
	return httptest.NewServer(newHandler(testToken, newRouteStore(time.Minute)))
}

func TestHealthIsPublic(t *testing.T) {
	server := newTestServer()
	defer server.Close()

	response, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header.Get("Cache-Control"))
	}
}

func TestRouteRoundTripAndAuthorization(t *testing.T) {
	server := newTestServer()
	defer server.Close()

	response, err := http.Get(server.URL + "/route")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET status = %d, want 401", response.StatusCode)
	}

	postJSON(t, server.URL+"/route", validRouteJSON, http.StatusOK)
	request, err := http.NewRequest(http.MethodGet, server.URL+"/route", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", response.StatusCode)
	}
	var received map[string]any
	if err := json.NewDecoder(response.Body).Decode(&received); err != nil {
		t.Fatal(err)
	}
	if received["routeId"] != "route-123" || received["available"] != true {
		t.Fatalf("unexpected route response: %#v", received)
	}
}

func TestAvailableFalseClearsRoute(t *testing.T) {
	server := newTestServer()
	defer server.Close()

	postJSON(t, server.URL+"/route", validRouteJSON, http.StatusOK)
	postJSON(t, server.URL+"/route", `{"type":"minimap_route","available":false}`, http.StatusOK)

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/route", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var received map[string]any
	if err := json.NewDecoder(response.Body).Decode(&received); err != nil {
		t.Fatal(err)
	}
	if received["available"] != false {
		t.Fatalf("route was not cleared: %#v", received)
	}
}

func TestInvalidRouteRejected(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "malformed geometry", body: strings.Replace(validRouteJSON, "??o}@o}@", "??o}", 1)},
		{name: "step outside geometry", body: strings.Replace(validRouteJSON, `"toIndex":1`, `"toIndex":9`, 1)},
		{name: "missing current location", body: strings.Replace(validRouteJSON, `"currentLocation":{"latitude":10.0,"longitude":106.0,"accuracy":5},`, "", 1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer()
			defer server.Close()
			postJSON(t, server.URL+"/route", test.body, http.StatusBadRequest)
		})
	}
}

func TestRouteExpires(t *testing.T) {
	store := newRouteStore(time.Minute)
	store.set([]byte(validRouteJSON), time.Now().Add(-2*time.Minute))
	if got := string(store.get(time.Now())); got != string(unavailableRoute) {
		t.Fatalf("expired route = %s, want %s", got, unavailableRoute)
	}
}

func TestMethodAndBodyLimits(t *testing.T) {
	server := newTestServer()
	defer server.Close()

	request, _ := http.NewRequest(http.MethodPut, server.URL+"/route", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT status = %d, want 405", response.StatusCode)
	}

	body := strings.NewReader(strings.Repeat(" ", maxRouteBodyBytes+1))
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/route", body)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized POST status = %d, want 413", response.StatusCode)
	}
}

func TestWatchPairingKeepsMasterTokenOnPhoneAndGrantsReadOnlyCredential(t *testing.T) {
	server := newTestServer()
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/pair/start", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		Code      string `json:"code"`
		Session   string `json:"session"`
		ExpiresIn int    `json:"expiresIn"`
	}
	if err := json.NewDecoder(response.Body).Decode(&pairing); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(pairing.Code) != 6 || !digitsOnly(pairing.Code) ||
		!validPairNonce(pairing.Session) || pairing.ExpiresIn != int(pairCodeTTL.Seconds()) {
		t.Fatalf("invalid watch pairing response: status=%d code=%q sessionValid=%v expiresIn=%d",
			response.StatusCode, pairing.Code, validPairNonce(pairing.Session), pairing.ExpiresIn)
	}

	statusRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/pair/start", nil)
	statusRequest.Header.Set("X-MiniMap-Pair-Session", pairing.Session)
	response, err = http.DefaultClient.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("pending status = %d, want 202", response.StatusCode)
	}

	claimBody := fmt.Sprintf(`{"code":%q}`, pairing.Code)
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/pair/claim", strings.NewReader(claimBody))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated claim status = %d, want 401", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/pair/claim", strings.NewReader(claimBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d, want 200", response.StatusCode)
	}

	request, _ = http.NewRequest(http.MethodPost, server.URL+"/pair/claim", strings.NewReader(claimBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second claim status = %d, want 409", response.StatusCode)
	}

	statusRequest, _ = http.NewRequest(http.MethodGet, server.URL+"/pair/start", nil)
	statusRequest.Header.Set("X-MiniMap-Pair-Session", pairing.Session)
	response, err = http.DefaultClient.Do(statusRequest)
	if err != nil {
		t.Fatal(err)
	}
	var paired struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&paired); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || paired.Status != "paired" || paired.Token == testToken || !validWatchToken(paired.Token, testToken) {
		t.Fatalf("invalid paired response: status=%d state=%q tokenValid=%v", response.StatusCode, paired.Status,
			validWatchToken(paired.Token, testToken))
	}

	postJSON(t, server.URL+"/route", validRouteJSON, http.StatusOK)
	watchRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/route", nil)
	watchRequest.Header.Set("Authorization", "Bearer "+paired.Token)
	response, err = http.DefaultClient.Do(watchRequest)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("paired watch GET status = %d, want 200", response.StatusCode)
	}

	watchRequest, _ = http.NewRequest(http.MethodPost, server.URL+"/route", strings.NewReader(validRouteJSON))
	watchRequest.Header.Set("Authorization", "Bearer "+paired.Token)
	watchRequest.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(watchRequest)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("paired watch POST status = %d, want 401", response.StatusCode)
	}
}

func TestPairingCodeCannotBeClaimedAfterExpiry(t *testing.T) {
	manager := newPairingManager()
	now := time.Now()
	session, err := manager.start(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.claim(session.code, now.Add(pairCodeTTL)); !errors.Is(err, errPairNotFound) {
		t.Fatalf("claim after expiry error = %v, want pairing_not_found", err)
	}
	if _, exists := manager.status(session.nonce, now.Add(pairCodeTTL)); exists {
		t.Fatal("expired pairing session is still available")
	}
}

func TestPairingRateLimitsStartsAndClaims(t *testing.T) {
	manager := newPairingManager()
	now := time.Now()
	for index := 0; index < pairStartLimit; index++ {
		if _, err := manager.start(now); err != nil {
			t.Fatalf("start %d: %v", index+1, err)
		}
	}
	if _, err := manager.start(now); !errors.Is(err, errPairRateLimited) {
		t.Fatalf("extra start error = %v, want rate limited", err)
	}
	for index := 0; index < pairClaimLimit; index++ {
		_, _ = manager.claim("000000", now)
	}
	if _, err := manager.claim("000000", now); !errors.Is(err, errPairRateLimited) {
		t.Fatalf("extra claim error = %v, want rate limited", err)
	}
}

func postJSON(t *testing.T, url, body string, wantStatus int) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != wantStatus {
		t.Fatalf("POST status = %d, want %d", response.StatusCode, wantStatus)
	}
}
