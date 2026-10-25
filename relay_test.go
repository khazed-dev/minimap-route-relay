package main

import (
	"encoding/json"
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
