package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Donking-36/container-log-platform/internal/handler"
	"github.com/Donking-36/container-log-platform/internal/middleware"
	"github.com/Donking-36/container-log-platform/internal/repository"
	"github.com/Donking-36/container-log-platform/internal/server"
	"github.com/Donking-36/container-log-platform/internal/service"
	"github.com/gin-gonic/gin"
)

func TestLogIngestionQueryAndStatisticsIntegration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gormDB, err := repository.OpenSQLite(
		filepath.Join(t.TempDir(), "logs.db"),
	)
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("access SQLite connection pool: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close SQLite: %v", err)
		}
	})

	if err := repository.Migrate(gormDB); err != nil {
		t.Fatalf("migrate SQLite: %v", err)
	}

	logRepository, err := repository.NewLogRepository(gormDB)
	if err != nil {
		t.Fatalf("create log repository: %v", err)
	}

	ingestionService, err := service.NewIngestionService(
		logRepository,
		64*1024,
	)
	if err != nil {
		t.Fatalf("create ingestion service: %v", err)
	}

	ingestionHandler, err := handler.NewIngestionHandler(
		ingestionService,
		1024*1024,
		100,
	)
	if err != nil {
		t.Fatalf("create ingestion handler: %v", err)
	}

	queryService, err := service.NewQueryService(
		logRepository,
		20,
		100,
	)
	if err != nil {
		t.Fatalf("create query service: %v", err)
	}

	queryHandler, err := handler.NewQueryHandler(queryService)
	if err != nil {
		t.Fatalf("create query handler: %v", err)
	}
	logStatsService, err := service.NewStatsService(
		logRepository,
	)
	if err != nil {
		t.Fatalf("create statistics service: %v", err)
	}

	logStatsHandler, err := handler.NewStatsHandler(
		logStatsService,
	)
	if err != nil {
		t.Fatalf("create statistics handler: %v", err)
	}
	readiness := server.NewReadiness(sqlDB.PingContext)
	readiness.SetReady(true)

	logger := slog.New(
		slog.NewTextHandler(io.Discard, nil),
	)

	publicRouter, err := server.NewPublicRouter(
		queryHandler,
		logStatsHandler,
		readiness,
		logger,
	)
	if err != nil {
		t.Fatalf("create public router: %v", err)
	}

	internalRouter, err := server.NewInternalRouter(
		ingestionHandler,
		readiness,
		logger,
	)
	if err != nil {
		t.Fatalf("create internal router: %v", err)
	}

	ingestBody := `{
		"source_event_id": "integration-event-001",
		"agent_id": "integration-agent",
		"container_name": "integration-container",
		"container_id": "integration-container-id",
		"service": "integration-service",
		"level": "info",
		"message": "integration query message",
		"source": "stdout",
		"logged_at": "2026-07-24T10:00:00Z",
		"raw_event": {
			"nested": {"ok": true},
			"sequence": 1
		}
	}`
	ingestRecorder := httptest.NewRecorder()
	ingestRequest := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/logs",
		strings.NewReader(ingestBody),
	)
	ingestRequest.Header.Set("Content-Type", "application/json")

	internalRouter.ServeHTTP(ingestRecorder, ingestRequest)

	if ingestRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"ingestion status = %d, want %d; body = %s",
			ingestRecorder.Code,
			http.StatusCreated,
			ingestRecorder.Body.String(),
		)
	}

	bulkBody := `[
		{
			"source_event_id": "integration-event-002",
			"agent_id": "integration-agent",
			"container_name": "integration-container",
			"service": "integration-service",
			"level": "error",
			"message": "integration error message",
			"source": "stderr",
			"logged_at": "2026-07-24T10:01:00Z"
		},
		{
			"source_event_id": "integration-event-003",
			"agent_id": "integration-agent",
			"container_name": "integration-container",
			"service": "worker-service",
			"level": "warn",
			"message": "integration warning message",
			"source": "stdout",
			"logged_at": "2026-07-24T10:02:00Z"
		},
		{
			"source_event_id": "integration-event-004",
			"agent_id": "integration-agent",
			"container_name": "other-container",
			"service": "other-service",
			"level": "error",
			"message": "log excluded by container filter",
			"source": "stderr",
			"logged_at": "2026-07-24T10:03:00Z"
		}
	]`
	bulkRecorder := httptest.NewRecorder()
	bulkRequest := httptest.NewRequest(
		http.MethodPost,
		"/internal/v1/logs/bulk",
		strings.NewReader(bulkBody),
	)
	bulkRequest.Header.Set("Content-Type", "application/json")

	internalRouter.ServeHTTP(bulkRecorder, bulkRequest)

	if bulkRecorder.Code != http.StatusOK {
		t.Fatalf(
			"bulk ingestion status = %d, want %d; body = %s",
			bulkRecorder.Code,
			http.StatusOK,
			bulkRecorder.Body.String(),
		)
	}

	listRecorder := httptest.NewRecorder()
	listRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs?"+
			"container=integration-container&"+
			"level=INFO&page=1&page_size=10",
		nil,
	)

	publicRouter.ServeHTTP(listRecorder, listRequest)

	if listRecorder.Code != http.StatusOK {
		t.Fatalf(
			"list status = %d, want %d; body = %s",
			listRecorder.Code,
			http.StatusOK,
			listRecorder.Body.String(),
		)
	}

	var listResponse struct {
		Data       []map[string]json.RawMessage `json:"data"`
		Pagination struct {
			Total int64 `json:"total"`
		} `json:"pagination"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(
		listRecorder.Body.Bytes(),
		&listResponse,
	); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if listResponse.Pagination.Total != 1 ||
		len(listResponse.Data) != 1 {
		t.Fatalf(
			"list total = %d, data length = %d; want 1 and 1",
			listResponse.Pagination.Total,
			len(listResponse.Data),
		)
	}

	if _, exists := listResponse.Data[0]["raw_event"]; exists {
		t.Fatal("list response exposed raw_event")
	}

	if listResponse.RequestID == "" ||
		listResponse.RequestID != listRecorder.Header().Get(
			middleware.RequestIDHeader,
		) {
		t.Fatal("list response request ID does not match header")
	}

	var logID int64
	if err := json.Unmarshal(
		listResponse.Data[0]["id"],
		&logID,
	); err != nil {
		t.Fatalf("decode list log ID: %v", err)
	}

	detailRecorder := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/logs/"+strconv.FormatInt(logID, 10),
		nil,
	)

	publicRouter.ServeHTTP(detailRecorder, detailRequest)

	if detailRecorder.Code != http.StatusOK {
		t.Fatalf(
			"detail status = %d, want %d; body = %s",
			detailRecorder.Code,
			http.StatusOK,
			detailRecorder.Body.String(),
		)
	}

	var detailResponse struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(
		detailRecorder.Body.Bytes(),
		&detailResponse,
	); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}

	var rawEvent struct {
		Nested struct {
			OK bool `json:"ok"`
		} `json:"nested"`
		Sequence int `json:"sequence"`
	}
	if err := json.Unmarshal(
		detailResponse.Data["raw_event"],
		&rawEvent,
	); err != nil {
		t.Fatalf("decode detail raw_event: %v", err)
	}

	if !rawEvent.Nested.OK || rawEvent.Sequence != 1 {
		t.Fatalf("detail raw_event = %+v", rawEvent)
	}

	levelStatsRecorder := httptest.NewRecorder()
	levelStatsRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/stats/levels?container=integration-container",
		nil,
	)

	publicRouter.ServeHTTP(
		levelStatsRecorder,
		levelStatsRequest,
	)

	if levelStatsRecorder.Code != http.StatusOK {
		t.Fatalf(
			"level statistics status = %d, want %d; body = %s",
			levelStatsRecorder.Code,
			http.StatusOK,
			levelStatsRecorder.Body.String(),
		)
	}

	var levelStatsResponse struct {
		Data []struct {
			Level      string  `json:"level"`
			Count      int64   `json:"count"`
			Percentage float64 `json:"percentage"`
		} `json:"data"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(
		levelStatsRecorder.Body.Bytes(),
		&levelStatsResponse,
	); err != nil {
		t.Fatalf("decode level statistics response: %v", err)
	}

	expectedLevels := []struct {
		level      string
		count      int64
		percentage float64
	}{
		{level: "ERROR", count: 1, percentage: 33.3},
		{level: "INFO", count: 1, percentage: 33.3},
		{level: "WARN", count: 1, percentage: 33.3},
	}

	if levelStatsResponse.Total != 3 ||
		len(levelStatsResponse.Data) != len(expectedLevels) {
		t.Fatalf(
			"level statistics total = %d, data length = %d; want 3 and %d",
			levelStatsResponse.Total,
			len(levelStatsResponse.Data),
			len(expectedLevels),
		)
	}

	for index, expected := range expectedLevels {
		actual := levelStatsResponse.Data[index]
		if actual.Level != expected.level ||
			actual.Count != expected.count ||
			actual.Percentage != expected.percentage {
			t.Fatalf(
				"level statistics data[%d] = %+v; want level=%s count=%d percentage=%.1f",
				index,
				actual,
				expected.level,
				expected.count,
				expected.percentage,
			)
		}
	}

	serviceStatsRecorder := httptest.NewRecorder()
	serviceStatsRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/stats/services?container=integration-container",
		nil,
	)

	publicRouter.ServeHTTP(
		serviceStatsRecorder,
		serviceStatsRequest,
	)

	if serviceStatsRecorder.Code != http.StatusOK {
		t.Fatalf(
			"service statistics status = %d, want %d; body = %s",
			serviceStatsRecorder.Code,
			http.StatusOK,
			serviceStatsRecorder.Body.String(),
		)
	}

	var serviceStatsResponse struct {
		Data []struct {
			Service string `json:"service"`
			Count   int64  `json:"count"`
		} `json:"data"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(
		serviceStatsRecorder.Body.Bytes(),
		&serviceStatsResponse,
	); err != nil {
		t.Fatalf("decode service statistics response: %v", err)
	}

	expectedServices := []struct {
		service string
		count   int64
	}{
		{service: "integration-service", count: 2},
		{service: "worker-service", count: 1},
	}

	if serviceStatsResponse.Total != 3 ||
		len(serviceStatsResponse.Data) != len(expectedServices) {
		t.Fatalf(
			"service statistics total = %d, data length = %d; want 3 and %d",
			serviceStatsResponse.Total,
			len(serviceStatsResponse.Data),
			len(expectedServices),
		)
	}

	for index, expected := range expectedServices {
		actual := serviceStatsResponse.Data[index]
		if actual.Service != expected.service ||
			actual.Count != expected.count {
			t.Fatalf(
				"service statistics data[%d] = %+v; want service=%s count=%d",
				index,
				actual,
				expected.service,
				expected.count,
			)
		}
	}
}
