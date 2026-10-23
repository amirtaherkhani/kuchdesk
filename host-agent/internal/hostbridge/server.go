package hostbridge

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/amirtaherkhani/kuchdesk/host-agent/internal/doctor"
)

type Diagnostician interface {
	Run(context.Context, doctor.Config) (doctor.Report, error)
}

// Server handles one authenticated host command surface. It is independent of
// listener setup, so the network service can remain disabled by default.
type Server struct {
	Doctor        Diagnostician
	Token         string
	ServiceAccess func(context.Context) (map[string]any, error)
	slots         chan struct{}
}

func New(diagnostics Diagnostician, token string) (*Server, error) {
	if len(token) < 32 || diagnostics == nil {
		return nil, errors.New("host bridge requires Doctor and a private bearer token")
	}
	return &Server{Doctor: diagnostics, Token: token, slots: make(chan struct{}, 8)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/doctor", s.doctor)
	mux.HandleFunc("GET /v1/service-access", s.serviceAccess)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path != "/healthz" {
			provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			want := sha256.Sum256([]byte(s.Token))
			got := sha256.Sum256([]byte(provided))
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "capacity_reached"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) serviceAccess(w http.ResponseWriter, r *http.Request) {
	if s.ServiceAccess == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "service_access_unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	report, err := s.ServiceAccess(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "service_access_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) doctor(w http.ResponseWriter, r *http.Request) {
	var cfg doctor.Config
	if !decode(w, r, &cfg) {
		return
	}
	if cfg.Validate() != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_doctor_config"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	report, err := s.Doctor.Run(ctx, cfg)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "doctor_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func decode(w http.ResponseWriter, r *http.Request, output any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
