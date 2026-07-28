package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const pageSize = 100

type config struct {
	BaseURL        string
	Prefix         string
	Start          time.Time
	End            time.Time
	Expected       int
	ExpectedStdout int
	ExpectedStderr int
	ExpectedFile   int
	Timeout        time.Duration
}

type logItem struct {
	ID            int64  `json:"id"`
	EventID       string `json:"event_id"`
	SourceEventID string `json:"source_event_id"`
	ContainerName string `json:"container_name"`
	Service       string `json:"service"`
	Level         string `json:"level"`
	Source        string `json:"source"`
	LoggedAt      string `json:"logged_at"`
}

type listResponse struct {
	Data       []logItem `json:"data"`
	Pagination struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Total    int `json:"total"`
	} `json:"pagination"`
}

type verificationResult struct {
	VerifiedAt     string         `json:"verified_at"`
	Prefix         string         `json:"prefix"`
	Expected       int            `json:"expected"`
	Actual         int            `json:"actual"`
	UniqueEventIDs int            `json:"unique_event_ids"`
	SourceCounts   map[string]int `json:"source_counts"`
	Passed         bool           `json:"passed"`
}

func main() {
	cfg, err := parseConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	result, err := waitForEvents(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "encode verification result: %v\n", err)
		os.Exit(1)
	}
}

func parseConfig() (config, error) {
	var (
		cfg       config
		startText string
		endText   string
	)

	flag.StringVar(
		&cfg.BaseURL,
		"base-url",
		"http://127.0.0.1:18083",
		"public API base URL",
	)
	flag.StringVar(
		&cfg.Prefix,
		"prefix",
		"",
		"required source_event_id prefix",
	)
	flag.StringVar(&startText, "start", "", "RFC3339 query start")
	flag.StringVar(&endText, "end", "", "RFC3339 query end")
	flag.IntVar(&cfg.Expected, "expected", 0, "expected event count")
	flag.IntVar(
		&cfg.ExpectedStdout,
		"stdout",
		0,
		"expected stdout count",
	)
	flag.IntVar(
		&cfg.ExpectedStderr,
		"stderr",
		0,
		"expected stderr count",
	)
	flag.IntVar(
		&cfg.ExpectedFile,
		"file",
		0,
		"expected file count",
	)
	flag.DurationVar(
		&cfg.Timeout,
		"timeout",
		3*time.Minute,
		"maximum wait for ingestion",
	)
	flag.Parse()

	if strings.TrimSpace(cfg.Prefix) == "" {
		return config{}, errors.New("prefix must not be empty")
	}

	if cfg.Expected <= 0 {
		return config{}, errors.New(
			"expected must be greater than zero",
		)
	}

	if cfg.ExpectedStdout+
		cfg.ExpectedStderr+
		cfg.ExpectedFile != cfg.Expected {
		return config{}, errors.New(
			"stdout, stderr and file counts must sum to expected",
		)
	}

	if cfg.Timeout <= 0 {
		return config{}, errors.New(
			"timeout must be greater than zero",
		)
	}

	start, err := time.Parse(time.RFC3339, startText)
	if err != nil {
		return config{}, fmt.Errorf("parse start: %w", err)
	}

	end, err := time.Parse(time.RFC3339, endText)
	if err != nil {
		return config{}, fmt.Errorf("parse end: %w", err)
	}

	if start.After(end) {
		return config{}, errors.New("start must not be after end")
	}

	cfg.Start = start.UTC()
	cfg.End = end.UTC()

	return cfg, nil
}

func waitForEvents(
	cfg config,
) (verificationResult, error) {
	client := newHTTPClient()
	deadline := time.Now().Add(cfg.Timeout)
	lastCount := 0

	for {
		items, err := fetchWindow(client, cfg)
		if err != nil {
			if time.Now().After(deadline) {
				return verificationResult{}, err
			}

			time.Sleep(time.Second)
			continue
		}

		result, complete, err := verifyItems(cfg, items)
		if err != nil {
			return verificationResult{}, err
		}

		lastCount = result.Actual
		if complete {
			return result, nil
		}

		if time.Now().After(deadline) {
			return verificationResult{}, fmt.Errorf(
				"timed out waiting for prefix %q: got %d of %d events",
				cfg.Prefix,
				lastCount,
				cfg.Expected,
			)
		}

		time.Sleep(time.Second)
	}
}

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}

func fetchWindow(
	client *http.Client,
	cfg config,
) ([]logItem, error) {
	items := make([]logItem, 0)
	seenIDs := make(map[int64]struct{})
	page := 1
	expectedTotal := -1
	var previous *logItem

	for {
		endpoint, err := buildListURL(cfg, page)
		if err != nil {
			return nil, err
		}

		response, err := client.Get(endpoint)
		if err != nil {
			return nil, fmt.Errorf("query logs page %d: %w", page, err)
		}

		body, readErr := io.ReadAll(
			io.LimitReader(response.Body, 4<<20),
		)
		closeErr := response.Body.Close()

		if readErr != nil {
			return nil, fmt.Errorf(
				"read logs page %d: %w",
				page,
				readErr,
			)
		}
		if closeErr != nil {
			return nil, fmt.Errorf(
				"close logs page %d: %w",
				page,
				closeErr,
			)
		}

		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf(
				"query logs page %d returned HTTP %d: %s",
				page,
				response.StatusCode,
				body,
			)
		}

		var decoded listResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return nil, fmt.Errorf(
				"decode logs page %d: %w",
				page,
				err,
			)
		}

		if decoded.Pagination.Page != page ||
			decoded.Pagination.PageSize != pageSize {
			return nil, fmt.Errorf(
				"unexpected pagination metadata on page %d: page=%d page_size=%d",
				page,
				decoded.Pagination.Page,
				decoded.Pagination.PageSize,
			)
		}

		if expectedTotal == -1 {
			expectedTotal = decoded.Pagination.Total
		} else if decoded.Pagination.Total != expectedTotal {
			return nil, fmt.Errorf(
				"pagination total changed from %d to %d while reading pages",
				expectedTotal,
				decoded.Pagination.Total,
			)
		}

		for _, item := range decoded.Data {
			if _, exists := seenIDs[item.ID]; exists {
				return nil, fmt.Errorf(
					"log ID %d appeared on more than one page",
					item.ID,
				)
			}

			if previous != nil {
				if err := verifyOrder(*previous, item); err != nil {
					return nil, err
				}
			}

			seenIDs[item.ID] = struct{}{}
			items = append(items, item)
			previous = &items[len(items)-1]
		}

		if len(decoded.Data) < pageSize ||
			page*pageSize >= expectedTotal {
			break
		}

		page++
		if page > 1000 {
			return nil, errors.New(
				"query window exceeded 1000 pages",
			)
		}
	}

	if len(items) != expectedTotal {
		return nil, fmt.Errorf(
			"pagination returned %d records, total reports %d",
			len(items),
			expectedTotal,
		)
	}

	filtered := make([]logItem, 0, len(items))
	for _, item := range items {
		if strings.HasPrefix(item.SourceEventID, cfg.Prefix) {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

func verifyOrder(previous logItem, current logItem) error {
	previousTime, err := time.Parse(
		time.RFC3339Nano,
		previous.LoggedAt,
	)
	if err != nil {
		return fmt.Errorf(
			"parse event %d logged_at while checking order: %w",
			previous.ID,
			err,
		)
	}

	currentTime, err := time.Parse(
		time.RFC3339Nano,
		current.LoggedAt,
	)
	if err != nil {
		return fmt.Errorf(
			"parse event %d logged_at while checking order: %w",
			current.ID,
			err,
		)
	}

	if previousTime.Before(currentTime) ||
		(previousTime.Equal(currentTime) &&
			previous.ID <= current.ID) {
		return fmt.Errorf(
			"logs are not ordered by logged_at DESC, id DESC: %d before %d",
			previous.ID,
			current.ID,
		)
	}

	return nil
}

func buildListURL(cfg config, page int) (string, error) {
	endpoint, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}

	endpoint.Path = "/api/v1/logs"
	values := endpoint.Query()
	values.Set("start", cfg.Start.Format(time.RFC3339))
	values.Set("end", cfg.End.Format(time.RFC3339))
	values.Set("page", fmt.Sprintf("%d", page))
	values.Set("page_size", fmt.Sprintf("%d", pageSize))
	endpoint.RawQuery = values.Encode()

	return endpoint.String(), nil
}

func verifyItems(
	cfg config,
	items []logItem,
) (verificationResult, bool, error) {
	result := verificationResult{
		VerifiedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		Prefix:       cfg.Prefix,
		Expected:     cfg.Expected,
		Actual:       len(items),
		SourceCounts: map[string]int{},
	}

	if len(items) > cfg.Expected {
		return verificationResult{}, false, fmt.Errorf(
			"prefix %q produced %d events, want %d",
			cfg.Prefix,
			len(items),
			cfg.Expected,
		)
	}

	eventIDs := make(map[string]struct{}, len(items))
	sourceEventIDs := make(map[string]struct{}, len(items))

	for _, item := range items {
		if item.EventID == "" ||
			item.SourceEventID == "" ||
			item.ContainerName == "" ||
			item.Service != "log-producer" {
			return verificationResult{}, false, fmt.Errorf(
				"event %d is missing required metadata",
				item.ID,
			)
		}

		loggedAt, err := time.Parse(time.RFC3339Nano, item.LoggedAt)
		if err != nil {
			return verificationResult{}, false, fmt.Errorf(
				"parse event %d logged_at: %w",
				item.ID,
				err,
			)
		}

		if loggedAt.Before(cfg.Start) || loggedAt.After(cfg.End) {
			return verificationResult{}, false, fmt.Errorf(
				"event %d is outside the requested time window",
				item.ID,
			)
		}

		if err := verifySourceAndLevel(item); err != nil {
			return verificationResult{}, false, err
		}

		result.SourceCounts[item.Source]++
		eventIDs[item.EventID] = struct{}{}
		sourceEventIDs[item.SourceEventID] = struct{}{}
	}

	result.UniqueEventIDs = len(eventIDs)

	if len(items) < cfg.Expected {
		return result, false, nil
	}

	if len(eventIDs) != cfg.Expected ||
		len(sourceEventIDs) != cfg.Expected {
		return verificationResult{}, false, fmt.Errorf(
			"expected %d unique event IDs and source event IDs, got %d and %d",
			cfg.Expected,
			len(eventIDs),
			len(sourceEventIDs),
		)
	}

	if result.SourceCounts["stdout"] != cfg.ExpectedStdout ||
		result.SourceCounts["stderr"] != cfg.ExpectedStderr ||
		result.SourceCounts["file"] != cfg.ExpectedFile {
		return verificationResult{}, false, fmt.Errorf(
			"unexpected source counts: stdout=%d stderr=%d file=%d",
			result.SourceCounts["stdout"],
			result.SourceCounts["stderr"],
			result.SourceCounts["file"],
		)
	}

	result.Passed = true

	return result, true, nil
}

func verifySourceAndLevel(item logItem) error {
	expectedLevels := map[string]string{
		"stdout": "INFO",
		"stderr": "ERROR",
		"file":   "WARN",
	}

	expectedLevel, exists := expectedLevels[item.Source]
	if !exists {
		return fmt.Errorf(
			"event %d has unexpected source %q",
			item.ID,
			item.Source,
		)
	}

	if item.Level != expectedLevel {
		return fmt.Errorf(
			"event %d has level %q for source %q, want %q",
			item.ID,
			item.Level,
			item.Source,
			expectedLevel,
		)
	}

	return nil
}
