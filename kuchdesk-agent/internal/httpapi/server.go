package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/dnsconfig"
	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/model"
	"github.com/amirtaherkhani/kuchdesk/kuchdesk-agent/internal/stream"
)

type SnapshotSource interface {
	Snapshot() model.Snapshot
	Ready() bool
}

type Server struct {
	baseProject map[string]string
	source      SnapshotSource
	broker      *stream.Broker
	dns         *dnsconfig.Manager
	bearerToken string
	heartbeat   time.Duration
	logger      *slog.Logger
	management  http.Handler
	hostBridge  http.Handler
}

// SetManagementHandler enables the separately configured management API.
func (s *Server) SetManagementHandler(handler http.Handler) { s.management = handler }

// SetHostBridgeHandler exposes the separately configured, agent-authenticated
// path to the host control plane. It is absent when the bridge is disabled.
func (s *Server) SetHostBridgeHandler(handler http.Handler) { s.hostBridge = handler }

func New(
	source SnapshotSource,
	broker *stream.Broker,
	dns *dnsconfig.Manager,
	bearerToken string,
	heartbeat time.Duration,
	logger *slog.Logger,
) *Server {
	return &Server{
		source:      source,
		broker:      broker,
		dns:         dns,
		bearerToken: bearerToken,
		heartbeat:   heartbeat,
		logger:      logger,
	}
}

func (s *Server) SetBaseProject(name, id, slug string) {
	s.baseProject = map[string]string{"name": name, "projectId": id, "slug": slug}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/platform", s.authenticate(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.baseProject)
	})))
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.Handle("GET /v1/snapshot", s.authenticate(http.HandlerFunc(s.snapshot)))
	mux.Handle("GET /v1/events", s.authenticate(http.HandlerFunc(s.events)))
	mux.Handle("GET /v1/dns/config", s.authenticate(http.HandlerFunc(s.dnsConfig)))
	mux.Handle("PUT /v1/dns/config", s.authenticate(http.HandlerFunc(s.replaceDNSConfig)))
	if s.management != nil {
		mux.Handle("/v1/manage/", s.authenticate(s.management))
	}
	if s.hostBridge != nil {
		mux.Handle("/v1/host/", s.authenticate(s.hostBridge))
	}
	return s.recover(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, _ *http.Request) {
	if !s.source.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "syncing"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) snapshot(w http.ResponseWriter, _ *http.Request) {
	if !s.source.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Kubernetes informer caches are still syncing",
		})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.source.Snapshot())
}

func (s *Server) dnsConfig(w http.ResponseWriter, r *http.Request) {
	state, err := s.dns.Read(r.Context())
	if err != nil {
		s.writeDNSError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) replaceDNSConfig(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var request dnsconfig.ReplaceRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request: " + err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must contain one JSON object"})
		return
	}

	state, err := s.dns.Replace(r.Context(), request)
	if err != nil {
		s.writeDNSError(w, err)
		return
	}
	if !request.DryRun {
		if _, err := s.broker.Publish("dns.config.changed", state); err != nil {
			s.logger.Error("publish DNS configuration event", "error", err)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) writeDNSError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "CoreDNS configuration request failed"
	switch {
	case errors.Is(err, dnsconfig.ErrDisabled):
		status = http.StatusForbidden
		message = err.Error()
	case errors.Is(err, dnsconfig.ErrUnavailable):
		status = http.StatusServiceUnavailable
		message = err.Error()
	case errors.Is(err, dnsconfig.ErrConflict), errors.Is(err, dnsconfig.ErrUnmanaged):
		status = http.StatusConflict
		message = err.Error()
	case errors.Is(err, dnsconfig.ErrInvalid):
		status = http.StatusBadRequest
		message = err.Error()
	default:
		s.logger.Error("CoreDNS configuration request failed", "error", err)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if !s.source.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Kubernetes informer caches are still syncing",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)

	afterID := parseLastEventID(r)
	replay, live, historyGap, latestID, unsubscribe := s.broker.Subscribe(afterID)
	defer unsubscribe()

	if _, err := fmt.Fprint(w, "retry: 3000\n\n"); err != nil {
		return
	}
	if err := controller.Flush(); err != nil {
		return
	}

	if afterID == 0 || historyGap || afterID > latestID {
		event := stream.Event{
			ID:        latestID,
			Name:      "snapshot",
			ClusterID: s.source.Snapshot().Cluster.ID,
			SentAt:    time.Now().UTC(),
		}
		event.Data, _ = json.Marshal(s.source.Snapshot())
		if err := writeSSE(w, event); err != nil {
			return
		}
	}
	if afterID != 0 && !historyGap && afterID <= latestID {
		for _, event := range replay {
			if err := writeSSE(w, event); err != nil {
				return
			}
		}
	}
	if err := controller.Flush(); err != nil {
		return
	}

	heartbeat := time.NewTicker(s.heartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-live:
			if !ok {
				return
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		case now := <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat %s\n\n", now.UTC().Format(time.RFC3339)); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		authorization := r.Header.Get("Authorization")
		if s.bearerToken == "" || !strings.HasPrefix(authorization, prefix) ||
			!constantTimeEqual(strings.TrimPrefix(authorization, prefix), s.bearerToken) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("HTTP handler panic", "error", recovered, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeSSE(w http.ResponseWriter, event stream.Event) error {
	envelope, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: ", event.ID, event.Name); err != nil {
		return err
	}
	if _, err = w.Write(envelope); err != nil {
		return err
	}
	_, err = fmt.Fprint(w, "\n\n")
	return err
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func parseLastEventID(r *http.Request) uint64 {
	value := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if value == "" {
		value = strings.TrimSpace(r.URL.Query().Get("lastEventId"))
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func IsServerClosed(err error) bool {
	return errors.Is(err, http.ErrServerClosed)
}
