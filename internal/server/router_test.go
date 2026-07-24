package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/gin-gonic/gin"
)

type fakeInternalLogHandler struct {
	oneCalls   int
	batchCalls int
}

func (h *fakeInternalLogHandler) IngestOne(c *gin.Context) {
	h.oneCalls++
	c.Status(http.StatusNoContent)
}

func (h *fakeInternalLogHandler) IngestBatch(c *gin.Context) {
	h.batchCalls++
	c.Status(http.StatusNoContent)
}

func TestPublicRouterHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	readiness := NewReadiness(
		func(context.Context) error {
			return errors.New("database unavailable")
		},
	)

	router := NewPublicRouter(
		readiness,
		newTestLogger(),
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/healthz",
		nil,
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	if recorder.Body.String() != `{"status":"ok"}` {
		t.Fatalf(
			"body = %q, want %q",
			recorder.Body.String(),
			`{"status":"ok"}`,
		)
	}
}

func TestPublicRouterReadinessEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		accepting     bool
		dependencyErr error
		wantStatus    int
		wantBody      string
	}{
		{
			name:       "traffic gate is closed",
			accepting:  false,
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"not_ready"}`,
		},
		{
			name:       "traffic gate and dependency are ready",
			accepting:  true,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ready"}`,
		},
		{
			name:          "database dependency is unavailable",
			accepting:     true,
			dependencyErr: errors.New("database unavailable"),
			wantStatus:    http.StatusServiceUnavailable,
			wantBody:      `{"status":"not_ready"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readiness := NewReadiness(
				func(context.Context) error {
					return tt.dependencyErr
				},
			)
			readiness.SetReady(tt.accepting)

			router := NewPublicRouter(
				readiness,
				newTestLogger(),
			)

			request := httptest.NewRequest(
				http.MethodGet,
				"/readyz",
				nil,
			)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf(
					"status code = %d, want %d",
					recorder.Code,
					tt.wantStatus,
				)
			}

			if recorder.Body.String() != tt.wantBody {
				t.Fatalf(
					"body = %q, want %q",
					recorder.Body.String(),
					tt.wantBody,
				)
			}
		})
	}
}

func TestInternalRouterDoesNotExposePublicRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, err := NewInternalRouter(
		&fakeInternalLogHandler{},
		newReadyReadiness(),
		newTestLogger(),
	)
	if err != nil {
		t.Fatalf("create internal router: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/healthz",
		nil,
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}
}

func TestInternalRouterRegistersLogIngestionRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logHandler := &fakeInternalLogHandler{}
	router, err := NewInternalRouter(
		logHandler,
		newReadyReadiness(),
		newTestLogger(),
	)
	if err != nil {
		t.Fatalf("create internal router: %v", err)
	}

	tests := []struct {
		name      string
		path      string
		wantCalls func() int
	}{
		{
			name: "single ingestion route",
			path: "/internal/v1/logs",
			wantCalls: func() int {
				return logHandler.oneCalls
			},
		},
		{
			name: "batch ingestion route",
			path: "/internal/v1/logs/bulk",
			wantCalls: func() int {
				return logHandler.batchCalls
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.wantCalls()

			request := httptest.NewRequest(
				http.MethodPost,
				tt.path,
				nil,
			)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf(
					"status code = %d, want %d",
					recorder.Code,
					http.StatusNoContent,
				)
			}

			if got := tt.wantCalls(); got != before+1 {
				t.Fatalf(
					"handler calls = %d, want %d",
					got,
					before+1,
				)
			}
		})
	}
}

func TestNewInternalRouterRejectsInvalidDependencies(
	t *testing.T,
) {
	tests := []struct {
		name       string
		logHandler InternalLogHandler
		readiness  *Readiness
	}{
		{
			name:      "nil handler",
			readiness: newReadyReadiness(),
		},
		{
			name:       "nil readiness",
			logHandler: &fakeInternalLogHandler{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, err := NewInternalRouter(
				tt.logHandler,
				tt.readiness,
				newTestLogger(),
			)
			if err == nil {
				t.Fatal("expected constructor error")
			}

			if router != nil {
				t.Fatal(
					"router must be nil when construction fails",
				)
			}
		})
	}
}

func TestPublicRouterDoesNotExposeInternalRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	readiness := NewReadiness(
		func(context.Context) error {
			return nil
		},
	)
	router := NewPublicRouter(
		readiness,
		newTestLogger(),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/logs",
		nil,
	)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"status code = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}
}

func TestInternalRouterRejectsRequestsWhenNotReady(
	t *testing.T,
) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		accepting     bool
		dependencyErr error
	}{
		{
			name:      "traffic gate is closed",
			accepting: false,
		},
		{
			name:          "database is unavailable",
			accepting:     true,
			dependencyErr: errors.New("database unavailable"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readiness := NewReadiness(
				func(context.Context) error {
					return tt.dependencyErr
				},
			)
			readiness.SetReady(tt.accepting)

			logHandler := &fakeInternalLogHandler{}
			router, err := NewInternalRouter(
				logHandler,
				readiness,
				newTestLogger(),
			)
			if err != nil {
				t.Fatalf("create internal router: %v", err)
			}

			request := httptest.NewRequest(
				http.MethodPost,
				"/internal/v1/logs",
				nil,
			)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf(
					"status code = %d, want %d",
					recorder.Code,
					http.StatusServiceUnavailable,
				)
			}

			var response middleware.ErrorResponse

			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatalf("decode error response: %v", err)
			}

			if response.Error.Code != "SERVICE_UNAVAILABLE" {
				t.Fatalf(
					"error code = %q, want %q",
					response.Error.Code,
					"SERVICE_UNAVAILABLE",
				)
			}

			headerRequestID := recorder.Header().Get(
				middleware.RequestIDHeader,
			)
			if response.RequestID == "" ||
				response.RequestID != headerRequestID {
				t.Fatalf(
					"body request ID = %q, header request ID = %q",
					response.RequestID,
					headerRequestID,
				)
			}

			if logHandler.oneCalls != 0 {
				t.Fatalf(
					"handler calls = %d, want 0",
					logHandler.oneCalls,
				)
			}
		})
	}
}

func newReadyReadiness() *Readiness {
	readiness := NewReadiness(
		func(context.Context) error {
			return nil
		},
	)
	readiness.SetReady(true)

	return readiness
}

func newTestLogger() *slog.Logger {
	return slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)
}
