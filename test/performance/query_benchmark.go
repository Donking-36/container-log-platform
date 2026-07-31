package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	defaultInternalURL = "http://127.0.0.1:18081/internal/v1/logs/bulk"
	defaultPublicURL   = "http://127.0.0.1:18080"
	defaultRecords     = 10_000
	defaultBatchSize   = 500
	defaultConcurrency = 10
	defaultDuration    = 30 * time.Second
)

var fixtureStart = time.Date(
	2026,
	time.January,
	1,
	0,
	0,
	0,
	0,
	time.UTC,
)

type config struct {
	InternalURL      string
	PublicURL        string
	Records          int
	BatchSize        int
	Concurrency      int
	Duration         time.Duration
	AllowNonBaseline bool
}

type eventInput struct {
	SourceEventID string         `json:"source_event_id"`
	AgentID       string         `json:"agent_id"`
	ContainerName string         `json:"container_name"`
	ContainerID   string         `json:"container_id"`
	Service       string         `json:"service"`
	Level         string         `json:"level"`
	Message       string         `json:"message"`
	Source        string         `json:"source"`
	LoggedAt      string         `json:"logged_at"`
	RawEvent      map[string]any `json:"raw_event"`
}

type ingestionResponse struct {
	Data struct {
		Received   int `json:"received"`
		Inserted   int `json:"inserted"`
		Duplicated int `json:"duplicated"`
		Rejected   int `json:"rejected"`
	} `json:"data"`
}

type listResponse struct {
	Data []struct {
		ID            int64  `json:"id"`
		ContainerName string `json:"container_name"`
		Level         string `json:"level"`
		LoggedAt      string `json:"logged_at"`
	} `json:"data"`
	Pagination struct {
		Total int `json:"total"`
	} `json:"pagination"`
}

type seedResult struct {
	Records    int `json:"records"`
	Batches    int `json:"batches"`
	Inserted   int `json:"inserted"`
	Duplicated int `json:"duplicated"`
	Rejected   int `json:"rejected"`
}

type queryResult struct {
	Requests       int     `json:"requests"`
	Successful     int     `json:"successful"`
	Errors         int     `json:"errors"`
	ErrorRate      float64 `json:"error_rate_percent"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	RequestsPerSec float64 `json:"requests_per_second"`
	P50MS          float64 `json:"p50_ms"`
	P95MS          float64 `json:"p95_ms"`
	P99MS          float64 `json:"p99_ms"`
	MinMS          float64 `json:"min_ms"`
	MaxMS          float64 `json:"max_ms"`
}

type report struct {
	GeneratedAt string      `json:"generated_at"`
	Records     int         `json:"records"`
	Concurrency int         `json:"concurrency"`
	Duration    string      `json:"duration"`
	Query       string      `json:"query"`
	Seed        seedResult  `json:"seed"`
	Result      queryResult `json:"result"`
	Thresholds  struct {
		P95MaxMS        float64 `json:"p95_max_ms"`
		ErrorRateMaxPct float64 `json:"error_rate_max_percent"`
	} `json:"thresholds"`
	BaselineParameters bool `json:"baseline_parameters"`
	ThresholdsPassed   bool `json:"thresholds_passed"`
	Passed             bool `json:"passed"`
}

type workerResult struct {
	latencies []float64
	requests  int
	errors    int
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result, err := run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode benchmark report: %v\n", err)
		os.Exit(1)
	}

	if !result.Passed {
		os.Exit(1)
	}
}

func parseConfig() (config, error) {
	cfg := config{}

	flag.StringVar(
		&cfg.InternalURL,
		"internal-url",
		defaultInternalURL,
		"internal bulk ingestion endpoint",
	)
	flag.StringVar(
		&cfg.PublicURL,
		"public-url",
		defaultPublicURL,
		"public API base URL",
	)
	flag.IntVar(
		&cfg.Records,
		"records",
		defaultRecords,
		"number of deterministic records to seed",
	)
	flag.IntVar(
		&cfg.BatchSize,
		"batch-size",
		defaultBatchSize,
		"events per ingestion request",
	)
	flag.IntVar(
		&cfg.Concurrency,
		"concurrency",
		defaultConcurrency,
		"number of concurrent query workers",
	)
	flag.DurationVar(
		&cfg.Duration,
		"duration",
		defaultDuration,
		"benchmark duration",
	)
	flag.BoolVar(
		&cfg.AllowNonBaseline,
		"allow-non-baseline",
		false,
		"run exploratory parameters without marking acceptance as passed",
	)
	flag.Parse()

	switch {
	case cfg.Records <= 0:
		return config{}, errors.New("records must be greater than zero")
	case cfg.BatchSize <= 0 || cfg.BatchSize > 1000:
		return config{}, errors.New(
			"batch-size must be between 1 and 1000",
		)
	case cfg.Concurrency <= 0:
		return config{}, errors.New(
			"concurrency must be greater than zero",
		)
	case cfg.Duration <= 0:
		return config{}, errors.New(
			"duration must be greater than zero",
		)
	}

	if !usesBaselineParameters(cfg) && !cfg.AllowNonBaseline {
		return config{}, fmt.Errorf(
			"acceptance requires records=%d concurrency=%d duration=%s; "+
				"use -allow-non-baseline only for exploratory runs",
			defaultRecords,
			defaultConcurrency,
			defaultDuration,
		)
	}

	if _, err := url.ParseRequestURI(cfg.InternalURL); err != nil {
		return config{}, fmt.Errorf("invalid internal-url: %w", err)
	}

	if _, err := url.ParseRequestURI(cfg.PublicURL); err != nil {
		return config{}, fmt.Errorf("invalid public-url: %w", err)
	}

	return cfg, nil
}

func run(cfg config) (report, error) {
	client := newHTTPClient(10 * time.Second)

	fmt.Fprintf(
		os.Stderr,
		"seeding %d deterministic records in batches of %d\n",
		cfg.Records,
		cfg.BatchSize,
	)

	seed, err := seedRecords(client, cfg)
	if err != nil {
		return report{}, err
	}

	queryURL, err := buildQueryURL(cfg.PublicURL, cfg.Records)
	if err != nil {
		return report{}, err
	}

	if err := validateQuery(client, queryURL, cfg.Records); err != nil {
		return report{}, err
	}

	fmt.Fprintf(
		os.Stderr,
		"running %d query workers for %s\n",
		cfg.Concurrency,
		cfg.Duration,
	)

	result := benchmarkQueries(
		newHTTPClient(2*time.Second),
		queryURL,
		cfg.Concurrency,
		cfg.Duration,
	)

	output := report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Records:     cfg.Records,
		Concurrency: cfg.Concurrency,
		Duration:    cfg.Duration.String(),
		Query:       queryURL,
		Seed:        seed,
		Result:      result,
	}
	output.Thresholds.P95MaxMS = 500
	output.Thresholds.ErrorRateMaxPct = 0
	output.BaselineParameters = usesBaselineParameters(cfg)
	output.ThresholdsPassed = result.Requests > 0 &&
		result.Errors == 0 &&
		result.P95MS <= output.Thresholds.P95MaxMS
	output.Passed = output.BaselineParameters &&
		output.ThresholdsPassed

	return output, nil
}

func usesBaselineParameters(cfg config) bool {
	return cfg.Records == defaultRecords &&
		cfg.Concurrency == defaultConcurrency &&
		cfg.Duration == defaultDuration
}

func newHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// 本地验收流量不能经过 Windows 代理，否则延迟和错误率不再代表 API 本身。
	transport.Proxy = nil
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}

func seedRecords(
	client *http.Client,
	cfg config,
) (seedResult, error) {
	result := seedResult{Records: cfg.Records}

	for offset := 0; offset < cfg.Records; offset += cfg.BatchSize {
		end := min(offset+cfg.BatchSize, cfg.Records)
		batch := make([]eventInput, 0, end-offset)

		for index := offset; index < end; index++ {
			batch = append(batch, fixtureEvent(index))
		}

		response, err := postBatch(client, cfg.InternalURL, batch)
		if err != nil {
			return seedResult{}, fmt.Errorf(
				"seed batch beginning at record %d: %w",
				offset+1,
				err,
			)
		}

		result.Batches++
		result.Inserted += response.Data.Inserted
		result.Duplicated += response.Data.Duplicated
		result.Rejected += response.Data.Rejected
	}

	if result.Inserted != cfg.Records ||
		result.Duplicated != 0 ||
		result.Rejected != 0 {
		return seedResult{}, fmt.Errorf(
			"unexpected seed result: inserted=%d duplicated=%d rejected=%d",
			result.Inserted,
			result.Duplicated,
			result.Rejected,
		)
	}

	return result, nil
}

// fixtureEvent 以 10 个容器、4 个级别的确定性分布造数，
// 为复合过滤查询提供每次运行都相同的选择性。
func fixtureEvent(index int) eventInput {
	sequence := index + 1
	containerNumber := index % 10
	levelNumber := (index / 10) % 4
	levels := [...]string{"INFO", "WARN", "ERROR", "DEBUG"}
	loggedAt := fixtureStart.Add(time.Duration(index) * time.Second)

	return eventInput{
		SourceEventID: fmt.Sprintf("perf-seed-%06d", sequence),
		AgentID:       "performance-agent-01",
		ContainerName: fmt.Sprintf(
			"perf-container-%02d",
			containerNumber,
		),
		ContainerID: fmt.Sprintf(
			"performance-container-id-%02d",
			containerNumber,
		),
		Service:  "performance-fixture",
		Level:    levels[levelNumber],
		Message:  fmt.Sprintf("performance event %06d", sequence),
		Source:   "stdout",
		LoggedAt: loggedAt.Format(time.RFC3339Nano),
		RawEvent: map[string]any{
			"fixture":  "query-performance",
			"sequence": sequence,
		},
	}
}

func postBatch(
	client *http.Client,
	endpoint string,
	batch []eventInput,
) (ingestionResponse, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return ingestionResponse{}, fmt.Errorf(
			"encode ingestion batch: %w",
			err,
		)
	}

	request, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return ingestionResponse{}, fmt.Errorf(
			"create ingestion request: %w",
			err,
		)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return ingestionResponse{}, fmt.Errorf(
			"send ingestion request: %w",
			err,
		)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(
		io.LimitReader(response.Body, 1<<20),
	)
	if err != nil {
		return ingestionResponse{}, fmt.Errorf(
			"read ingestion response: %w",
			err,
		)
	}

	if response.StatusCode != http.StatusOK {
		return ingestionResponse{}, fmt.Errorf(
			"ingestion returned HTTP %d: %s",
			response.StatusCode,
			responseBody,
		)
	}

	var decoded ingestionResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return ingestionResponse{}, fmt.Errorf(
			"decode ingestion response: %w",
			err,
		)
	}

	if decoded.Data.Received != len(batch) {
		return ingestionResponse{}, fmt.Errorf(
			"ingestion reported %d received events for batch of %d",
			decoded.Data.Received,
			len(batch),
		)
	}

	return decoded, nil
}

func buildQueryURL(baseURL string, records int) (string, error) {
	endpoint, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse public URL: %w", err)
	}

	endpoint.Path = "/api/v1/logs"
	values := endpoint.Query()
	values.Set("container", "perf-container-01")
	values.Set("level", "INFO")
	values.Set("start", fixtureStart.Format(time.RFC3339))
	values.Set(
		"end",
		fixtureStart.Add(
			time.Duration(records)*time.Second,
		).Format(time.RFC3339),
	)
	values.Set("page", "1")
	values.Set("page_size", "20")
	endpoint.RawQuery = values.Encode()

	return endpoint.String(), nil
}

func validateQuery(
	client *http.Client,
	endpoint string,
	records int,
) error {
	response, err := client.Get(endpoint)
	if err != nil {
		return fmt.Errorf("validate performance query: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))

		return fmt.Errorf(
			"performance query returned HTTP %d: %s",
			response.StatusCode,
			body,
		)
	}

	var decoded listResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return fmt.Errorf(
			"decode performance query response: %w",
			err,
		)
	}

	if len(decoded.Data) == 0 {
		return errors.New(
			"performance query returned no fixture records",
		)
	}

	expectedTotal := 0
	for index := range records {
		event := fixtureEvent(index)
		if event.ContainerName == "perf-container-01" &&
			event.Level == "INFO" {
			expectedTotal++
		}
	}

	if decoded.Pagination.Total != expectedTotal {
		return fmt.Errorf(
			"performance query returned total=%d, want %d",
			decoded.Pagination.Total,
			expectedTotal,
		)
	}

	queryEnd := fixtureStart.Add(
		time.Duration(records) * time.Second,
	)
	for _, item := range decoded.Data {
		loggedAt, err := time.Parse(time.RFC3339Nano, item.LoggedAt)
		if err != nil {
			return fmt.Errorf(
				"parse query result logged_at: %w",
				err,
			)
		}

		if item.ContainerName != "perf-container-01" ||
			item.Level != "INFO" ||
			loggedAt.Before(fixtureStart) ||
			loggedAt.After(queryEnd) {
			return fmt.Errorf(
				"query returned a record outside the requested filters: id=%d",
				item.ID,
			)
		}
	}

	return nil
}

// benchmarkQueries 并发运行固定数量的查询 worker 并汇总延迟。
// results 的容量必须等于 worker 数：主 goroutine 在 Wait 后才消费结果，
// 足够的缓冲可以避免 worker 发送结果时阻塞并与 Wait 形成死锁。
func benchmarkQueries(
	client *http.Client,
	endpoint string,
	concurrency int,
	duration time.Duration,
) queryResult {
	startedAt := time.Now()
	deadline := startedAt.Add(duration)
	results := make(chan workerResult, concurrency)

	var workers sync.WaitGroup
	workers.Add(concurrency)

	for range concurrency {
		go func() {
			defer workers.Done()
			results <- runWorker(client, endpoint, deadline)
		}()
	}

	workers.Wait()
	elapsed := time.Since(startedAt)
	close(results)

	latencies := make([]float64, 0)
	requests := 0
	requestErrors := 0

	for result := range results {
		latencies = append(latencies, result.latencies...)
		requests += result.requests
		requestErrors += result.errors
	}

	sort.Float64s(latencies)

	output := queryResult{
		Requests:   requests,
		Successful: requests - requestErrors,
		Errors:     requestErrors,
	}

	if requests > 0 {
		output.ErrorRate = float64(requestErrors) /
			float64(requests) * 100
		output.ElapsedSeconds = elapsed.Seconds()
		output.RequestsPerSec = float64(requests) /
			elapsed.Seconds()
	}

	if len(latencies) > 0 {
		output.P50MS = percentile(latencies, 0.50)
		output.P95MS = percentile(latencies, 0.95)
		output.P99MS = percentile(latencies, 0.99)
		output.MinMS = latencies[0]
		output.MaxMS = latencies[len(latencies)-1]
	}

	return output
}

// runWorker 将完整响应体读取并关闭后才记录一次延迟，这既把传输耗时纳入
// 指标，也允许 HTTP keep-alive 连接被后续请求复用。
func runWorker(
	client *http.Client,
	endpoint string,
	deadline time.Time,
) workerResult {
	result := workerResult{}

	for time.Now().Before(deadline) {
		startedAt := time.Now()
		response, err := client.Get(endpoint)

		result.requests++

		if err != nil {
			elapsedMS := float64(
				time.Since(startedAt).Microseconds(),
			) / 1000
			result.latencies = append(result.latencies, elapsedMS)
			result.errors++
			continue
		}

		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		elapsedMS := float64(
			time.Since(startedAt).Microseconds(),
		) / 1000
		result.latencies = append(result.latencies, elapsedMS)

		if response.StatusCode != http.StatusOK ||
			readErr != nil ||
			closeErr != nil {
			result.errors++
		}
	}

	return result
}

// percentile 使用 nearest-rank（向上取整）算法计算百分位，不做插值；
// sortedValues 必须已经按升序排列。
func percentile(sortedValues []float64, quantile float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}

	index := int(math.Ceil(
		quantile*float64(len(sortedValues)),
	)) - 1
	index = max(index, 0)

	return sortedValues[index]
}
