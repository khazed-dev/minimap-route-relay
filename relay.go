package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxRouteBodyBytes = 64 * 1024
	maxGeometryBytes  = 48 * 1024
	maxRouteSteps     = 300
	maxRouteIndex     = 12000
)

var unavailableRoute = []byte(`{"type":"minimap_route","available":false}`)

type coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type routeStep struct {
	Text      string `json:"text"`
	FromIndex int    `json:"fromIndex"`
	ToIndex   int    `json:"toIndex"`
}

type routePayload struct {
	Type            string       `json:"type"`
	Available       *bool        `json:"available"`
	RouteID         string       `json:"routeId"`
	Mode            string       `json:"mode"`
	Geometry        string       `json:"geometry"`
	Origin          *coordinate  `json:"origin"`
	Destination     *coordinate  `json:"destination"`
	CurrentLocation *coordinate  `json:"currentLocation"`
	CurrentStep     int          `json:"currentStep"`
	Steps           []routeStep  `json:"steps"`
}

type routeStore struct {
	mu        sync.RWMutex
	route     []byte
	updatedAt time.Time
	ttl       time.Duration
}

func newRouteStore(ttl time.Duration) *routeStore {
	return &routeStore{ttl: ttl}
}

func (s *routeStore) set(route []byte, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.route = append(s.route[:0], route...)
	s.updatedAt = now
}

func (s *routeStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.route = nil
	s.updatedAt = time.Time{}
}

func (s *routeStore) get(now time.Time) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.route) == 0 || now.Sub(s.updatedAt) > s.ttl {
		s.route = nil
		s.updatedAt = time.Time{}
		return unavailableRoute
	}
	return append([]byte(nil), s.route...)
}

type relayHandler struct {
	token string
	store *routeStore
}

func newHandler(token string, store *routeStore) http.Handler {
	r := &relayHandler{token: token, store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", r.health)
	mux.HandleFunc("/route", r.route)
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, req)
	})
}

func (r *relayHandler) health(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "MINIMAP_ROUTE_RELAY_OK\n")
}

func (r *relayHandler) route(w http.ResponseWriter, req *http.Request) {
	if !r.authorized(req) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	switch req.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(r.store.get(time.Now()))
	case http.MethodPost:
		r.publish(w, req)
	case http.MethodDelete:
		r.store.clear()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	}
}

func (r *relayHandler) authorized(req *http.Request) bool {
	header := strings.TrimSpace(req.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return subtle.ConstantTimeCompare([]byte(provided), []byte(r.token)) == 1
}

func (r *relayHandler) publish(w http.ResponseWriter, req *http.Request) {
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "content_type_must_be_json")
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxRouteBodyBytes)
	body, err := io.ReadAll(req.Body)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "route_too_large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if !json.Valid(body) {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := validateRoute(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var payload routePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_route")
		return
	}
	if payload.Available != nil && !*payload.Available {
		r.store.clear()
	} else {
		r.store.set(body, time.Now())
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"ok":true}`)
}

func validateRoute(body []byte) error {
	var payload routePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return errors.New("invalid_route")
	}
	if payload.Type != "minimap_route" || payload.Available == nil {
		return errors.New("invalid_route_envelope")
	}
	if !*payload.Available {
		return nil
	}
	if payload.RouteID == "" || len(payload.RouteID) > 128 {
		return errors.New("invalid_route_id")
	}
	if payload.Mode != "" && payload.Mode != "walk" {
		return errors.New("unsupported_mode")
	}
	if len(payload.Geometry) == 0 || len(payload.Geometry) > maxGeometryBytes {
		return errors.New("invalid_geometry")
	}
	pointCount, err := polylinePointCount(payload.Geometry)
	if err != nil || pointCount < 2 || pointCount > maxRouteIndex {
		return errors.New("invalid_geometry")
	}
	if !validCoordinate(payload.Origin) || !validCoordinate(payload.Destination) || !validCoordinate(payload.CurrentLocation) {
		return errors.New("invalid_coordinate")
	}
	if len(payload.Steps) == 0 || len(payload.Steps) > maxRouteSteps {
		return errors.New("invalid_steps")
	}
	if payload.CurrentStep < 0 || payload.CurrentStep >= len(payload.Steps) {
		return errors.New("invalid_current_step")
	}
	for _, step := range payload.Steps {
		if strings.TrimSpace(step.Text) == "" || len(step.Text) > 240 ||
			step.FromIndex < 0 || step.ToIndex < step.FromIndex || step.ToIndex >= pointCount {
			return errors.New("invalid_step")
		}
	}
	return nil
}

func polylinePointCount(encoded string) (int, error) {
	count, offset := 0, 0
	var latitude, longitude int64
	for offset < len(encoded) {
		latDelta, err := readPolylineDelta(encoded, &offset)
		if err != nil {
			return 0, err
		}
		if latDelta < -180_000_000 || latDelta > 180_000_000 {
			return 0, errors.New("latitude_delta_out_of_range")
		}
		lonDelta, err := readPolylineDelta(encoded, &offset)
		if err != nil {
			return 0, err
		}
		if lonDelta < -360_000_000 || lonDelta > 360_000_000 {
			return 0, errors.New("longitude_delta_out_of_range")
		}
		latitude += latDelta
		longitude += lonDelta
		if latitude < -90_000_000 || latitude > 90_000_000 ||
			longitude < -180_000_000 || longitude > 180_000_000 {
			return 0, errors.New("coordinate_out_of_range")
		}
		count++
		if count > maxRouteIndex {
			return 0, errors.New("too_many_points")
		}
	}
	return count, nil
}

func readPolylineDelta(encoded string, offset *int) (int64, error) {
	var value uint64
	var shift uint
	for {
		if *offset >= len(encoded) {
			return 0, errors.New("truncated_polyline")
		}
		chunk := int(encoded[*offset]) - 63
		(*offset)++
		if chunk < 0 || chunk > 63 {
			return 0, errors.New("invalid_polyline_character")
		}
		part := uint64(chunk & 0x1f)
		if shift > 60 || (shift == 60 && part > 0x0f) {
			return 0, errors.New("polyline_value_overflow")
		}
		value |= part << shift
		if chunk&0x20 == 0 {
			break
		}
		shift += 5
		if shift > 60 {
			return 0, errors.New("polyline_value_overflow")
		}
	}
	if value > math.MaxInt64 {
		return 0, errors.New("polyline_value_overflow")
	}
	delta := int64(value >> 1)
	if value&1 != 0 {
		delta = ^delta
	}
	return delta, nil
}

func validCoordinate(point *coordinate) bool {
	if point == nil || math.IsNaN(point.Latitude) || math.IsInf(point.Latitude, 0) ||
		math.IsNaN(point.Longitude) || math.IsInf(point.Longitude, 0) {
		return false
	}
	return point.Latitude >= -90 && point.Latitude <= 90 &&
		point.Longitude >= -180 && point.Longitude <= 180
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: code})
}
