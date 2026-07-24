package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id": RequestIDFromContext(c),
		})
	})

	requestIDs := make([]string, 0, 2)

	for range 2 {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(
			http.MethodGet,
			"/test",
			nil,
		)

		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf(
				"status code = %d, want %d",
				recorder.Code,
				http.StatusOK,
			)
		}

		headerRequestID := recorder.Header().Get(
			RequestIDHeader,
		)
		if headerRequestID == "" {
			t.Fatal(
				"response header request ID must not be empty",
			)
		}

		if _, err := uuid.Parse(headerRequestID); err != nil {
			t.Fatalf(
				"response header request ID = %q, "+
					"want valid UUID: %v",
				headerRequestID,
				err,
			)
		}

		var response struct {
			RequestID string `json:"request_id"`
		}

		if err := json.Unmarshal(
			recorder.Body.Bytes(),
			&response,
		); err != nil {
			t.Fatalf("decode response body: %v", err)
		}

		if response.RequestID != headerRequestID {
			t.Fatalf(
				"body request ID = %q, "+
					"header request ID = %q",
				response.RequestID,
				headerRequestID,
			)
		}

		requestIDs = append(requestIDs, headerRequestID)
	}

	if requestIDs[0] == requestIDs[1] {
		t.Fatalf(
			"consecutive requests used the same ID %q",
			requestIDs[0],
		)
	}
}

func TestRequestIDPreservesValidUpstreamID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id": RequestIDFromContext(c),
		})
	})

	const upstreamRequestID = "logstash-request-123"

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/test",
		nil,
	)
	request.Header.Set(
		RequestIDHeader,
		upstreamRequestID,
	)

	router.ServeHTTP(recorder, request)

	if got := recorder.Header().Get(RequestIDHeader); got !=
		upstreamRequestID {
		t.Fatalf(
			"response request ID = %q, want %q",
			got,
			upstreamRequestID,
		)
	}
}

func TestRequestIDFromContextWithoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	context, _ := gin.CreateTestContext(
		httptest.NewRecorder(),
	)

	if got := RequestIDFromContext(context); got != "" {
		t.Fatalf(
			"request ID = %q, want empty string",
			got,
		)
	}
}
