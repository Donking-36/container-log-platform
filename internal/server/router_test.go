package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicRouterHealthEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := NewPublicRouter()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "process is alive",
			path:       "/healthz",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "database is not ready",
			path:       "/readyz",
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"status":"not_ready"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodGet,
				tt.path,
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
