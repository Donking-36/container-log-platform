package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicRouterHealthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	readiness := NewReadiness(
		func(context.Context) error {
			return errors.New("database unavailable")
		},
	)

	router := NewPublicRouter(readiness)

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

			router := NewPublicRouter(readiness)

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

	router := NewInternalRouter()
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
