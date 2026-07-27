package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/model"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

type fakeLogStatsService struct {
	levelResult   service.LevelStatsResult
	serviceResult service.ServiceStatsResult

	levelErr   error
	serviceErr error

	levelCalls   int
	serviceCalls int

	levelInput   service.StatsInput
	serviceInput service.StatsInput
}

func (f *fakeLogStatsService) GetLevelStats(
	_ context.Context,
	input service.StatsInput,
) (service.LevelStatsResult, error) {
	f.levelCalls++
	f.levelInput = input

	return f.levelResult, f.levelErr
}

func (f *fakeLogStatsService) GetServiceStats(
	_ context.Context,
	input service.StatsInput,
) (service.ServiceStatsResult, error) {
	f.serviceCalls++
	f.serviceInput = input

	return f.serviceResult, f.serviceErr
}

func TestNewStatsHandlerRejectsNilService(t *testing.T) {
	statsHandler, err := NewStatsHandler(nil)
	if err == nil {
		t.Fatal("NewStatsHandler(nil) returned nil error")
	}

	if statsHandler != nil {
		t.Fatalf(
			"handler = %#v, want nil",
			statsHandler,
		)
	}
}

func TestStatsHandlerGetLevelStats(t *testing.T) {
	fakeService := &fakeLogStatsService{
		levelResult: service.LevelStatsResult{
			Stats: []service.LevelStat{
				{
					Level:      "INFO",
					Count:      3,
					Percentage: 75,
				},
				{
					Level:      "ERROR",
					Count:      1,
					Percentage: 25,
				},
			},
			Total: 4,
		},
	}

	router := newTestStatsRouter(t, fakeService)

	query := url.Values{
		"container": {" container-a "},
		"service":   {" api "},
		"level":     {"error"},
		"start":     {"2026-07-27T10:00:00+08:00"},
		"end":       {"2026-07-27T12:00:00+08:00"},
	}

	recorder := performStatsRequest(
		router,
		"/api/v1/stats/levels?"+query.Encode(),
		"stats-request-id",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var response struct {
		Data []struct {
			Level      string  `json:"level"`
			Count      int64   `json:"count"`
			Percentage float64 `json:"percentage"`
		} `json:"data"`
		Total     int64  `json:"total"`
		RequestID string `json:"request_id"`
	}

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf("decode level statistics response: %v", err)
	}

	if len(response.Data) != 2 {
		t.Fatalf(
			"data length = %d, want 2",
			len(response.Data),
		)
	}

	if response.Data[0].Level != "INFO" ||
		response.Data[0].Count != 3 ||
		response.Data[0].Percentage != 75 {
		t.Fatalf(
			"first level statistic = %#v",
			response.Data[0],
		)
	}

	if response.Data[1].Level != "ERROR" ||
		response.Data[1].Count != 1 ||
		response.Data[1].Percentage != 25 {
		t.Fatalf(
			"second level statistic = %#v",
			response.Data[1],
		)
	}

	if response.Total != 4 {
		t.Fatalf(
			"total = %d, want 4",
			response.Total,
		)
	}

	assertResponseRequestID(
		t,
		recorder,
		response.RequestID,
	)

	if response.RequestID != "stats-request-id" {
		t.Fatalf(
			"request ID = %q, want stats-request-id",
			response.RequestID,
		)
	}

	if fakeService.levelCalls != 1 {
		t.Fatalf(
			"service calls = %d, want 1",
			fakeService.levelCalls,
		)
	}

	input := fakeService.levelInput

	// Handler只负责解析，不负责去除普通字符串的空白。
	if input.ContainerName != " container-a " {
		t.Fatalf(
			"container input = %q",
			input.ContainerName,
		)
	}

	if input.Service != " api " {
		t.Fatalf(
			"service input = %q",
			input.Service,
		)
	}

	if input.Level != "error" {
		t.Fatalf(
			"level input = %q",
			input.Level,
		)
	}

	wantStart, _ := time.Parse(
		time.RFC3339,
		"2026-07-27T10:00:00+08:00",
	)
	wantEnd, _ := time.Parse(
		time.RFC3339,
		"2026-07-27T12:00:00+08:00",
	)

	if input.Start == nil ||
		!input.Start.Equal(wantStart) {
		t.Fatalf(
			"start input = %v, want %v",
			input.Start,
			wantStart,
		)
	}

	if input.End == nil ||
		!input.End.Equal(wantEnd) {
		t.Fatalf(
			"end input = %v, want %v",
			input.End,
			wantEnd,
		)
	}
}

func TestStatsHandlerGetServiceStats(t *testing.T) {
	fakeService := &fakeLogStatsService{
		serviceResult: service.ServiceStatsResult{
			Stats: []model.LogServiceCount{
				{
					Service: "api",
					Count:   3,
				},
				{
					Service: "worker",
					Count:   1,
				},
			},
			Total: 4,
		},
	}

	router := newTestStatsRouter(t, fakeService)

	recorder := performStatsRequest(
		router,
		"/api/v1/stats/services",
		"",
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status code = %d, want %d; body = %s",
			recorder.Code,
			http.StatusOK,
			recorder.Body.String(),
		)
	}

	var response struct {
		Data []struct {
			Service string `json:"service"`
			Count   int64  `json:"count"`
		} `json:"data"`
		Total int64 `json:"total"`
	}

	if err := json.Unmarshal(
		recorder.Body.Bytes(),
		&response,
	); err != nil {
		t.Fatalf(
			"decode service statistics response: %v",
			err,
		)
	}

	if len(response.Data) != 2 {
		t.Fatalf(
			"data length = %d, want 2",
			len(response.Data),
		)
	}

	if response.Data[0].Service != "api" ||
		response.Data[0].Count != 3 {
		t.Fatalf(
			"first service statistic = %#v",
			response.Data[0],
		)
	}

	if response.Data[1].Service != "worker" ||
		response.Data[1].Count != 1 {
		t.Fatalf(
			"second service statistic = %#v",
			response.Data[1],
		)
	}

	if response.Total != 4 {
		t.Fatalf(
			"total = %d, want 4",
			response.Total,
		)
	}

	if fakeService.serviceCalls != 1 {
		t.Fatalf(
			"service calls = %d, want 1",
			fakeService.serviceCalls,
		)
	}
}

func TestStatsHandlerReturnsEmptyArrays(t *testing.T) {
	fakeService := &fakeLogStatsService{}

	router := newTestStatsRouter(t, fakeService)

	paths := []string{
		"/api/v1/stats/levels",
		"/api/v1/stats/services",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			recorder := performStatsRequest(
				router,
				path,
				"",
			)

			if recorder.Code != http.StatusOK {
				t.Fatalf(
					"status code = %d, want %d",
					recorder.Code,
					http.StatusOK,
				)
			}

			var response struct {
				Data []json.RawMessage `json:"data"`
			}

			if err := json.Unmarshal(
				recorder.Body.Bytes(),
				&response,
			); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if response.Data == nil ||
				len(response.Data) != 0 {
				t.Fatalf(
					"data = %#v, want non-nil empty array",
					response.Data,
				)
			}
		})
	}
}

func TestStatsHandlerRejectsInvalidParameters(
	t *testing.T,
) {
	tests := []string{
		"/api/v1/stats/levels?page=1",
		"/api/v1/stats/levels?level=INFO&level=ERROR",
		"/api/v1/stats/levels?start=not-a-time",
		"/api/v1/stats/levels?container=a;bad=b",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			fakeService := &fakeLogStatsService{}
			router := newTestStatsRouter(
				t,
				fakeService,
			)

			recorder := performStatsRequest(
				router,
				path,
				"",
			)

			assertAPIError(
				t,
				recorder,
				http.StatusBadRequest,
				"INVALID_ARGUMENT",
			)

			if fakeService.levelCalls != 0 ||
				fakeService.serviceCalls != 0 {
				t.Fatal(
					"service was called for invalid parameters",
				)
			}
		})
	}
}

func TestStatsHandlerMapsServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "invalid time range",
			err: errors.Join(
				errors.New("wrapped error"),
				service.ErrInvalidTimeRange,
			),
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_TIME_RANGE",
		},
		{
			name: "temporary database error",
			err: errors.Join(
				errors.New("database busy"),
				service.ErrTemporarilyUnavailable,
			),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "SERVICE_UNAVAILABLE",
		},
		{
			name:       "internal error",
			err:        errors.New("database failed"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeService := &fakeLogStatsService{
				levelErr: tt.err,
			}
			router := newTestStatsRouter(
				t,
				fakeService,
			)

			recorder := performStatsRequest(
				router,
				"/api/v1/stats/levels",
				"",
			)

			assertAPIError(
				t,
				recorder,
				tt.wantStatus,
				tt.wantCode,
			)
		})
	}
}

func newTestStatsRouter(
	t *testing.T,
	statsService LogStatsService,
) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	statsHandler, err := NewStatsHandler(
		statsService,
	)
	if err != nil {
		t.Fatalf("create stats handler: %v", err)
	}

	router := gin.New()
	router.Use(middleware.RequestID())

	router.GET(
		"/api/v1/stats/levels",
		statsHandler.GetLevelStats,
	)
	router.GET(
		"/api/v1/stats/services",
		statsHandler.GetServiceStats,
	)

	return router
}

func performStatsRequest(
	router http.Handler,
	path string,
	requestID string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(
		http.MethodGet,
		path,
		nil,
	)

	if requestID != "" {
		request.Header.Set(
			middleware.RequestIDHeader,
			requestID,
		)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	return recorder
}
