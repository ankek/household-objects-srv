package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/items"
	"github.com/ankek/Household-Objects-Dev/server/internal/session"
	"github.com/ankek/Household-Objects-Dev/server/internal/storage"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"time"
)

const (
	benchUsername = "bench-api-latency"
	benchPassword = "bench-api-latency-password"
)

func registerAndLogin(ctx context.Context, baseURL string) (*http.Client, string, error) {
	client := &http.Client{}

	creds, _ := json.Marshal(map[string]string{"username": benchUsername, "password": benchPassword})

	var regResp struct {
		GroupID string `json:"group_id"`
	}
	if err := doJSONPost(ctx, client, baseURL+"/api/v1/auth/register", creds, &regResp); err != nil {
		return nil, "", fmt.Errorf("register: %w", err)
	}

	sessionCookie, err := loginAndCaptureCookie(ctx, client, baseURL+"/api/v1/auth/login", creds)
	if err != nil {
		return nil, "", fmt.Errorf("login: %w", err)
	}

	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = &cookieInjectingTransport{base: base, cookie: sessionCookie}

	return client, regResp.GroupID, nil
}

func loginAndCaptureCookie(ctx context.Context, client *http.Client, url string, body []byte) (*http.Cookie, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s -> %d: %s", url, resp.StatusCode, string(respBody))
	}
	for _, c := range resp.Cookies() {
		if c.Name == session.CookieName {
			return c, nil
		}
	}
	return nil, fmt.Errorf("%s: response carried no %q cookie", url, session.CookieName)
}

type cookieInjectingTransport struct {
	base   http.RoundTripper
	cookie *http.Cookie
}

func (t *cookieInjectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.AddCookie(t.cookie)
	return t.base.RoundTrip(req)
}

func doJSONPost(ctx context.Context, client *http.Client, url string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s -> %d: %s", url, resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response from %s: %w", url, err)
		}
	}
	return nil
}

type corpusStats struct {
	TotalItems    int `json:"total_items"`
	DistinctNames int `json:"distinct_names"`
}

func seedCorpus(ctx context.Context, repo storage.ItemRepository, n int, rngSeed int64) ([]string, corpusStats, error) {
	r := rand.New(rand.NewSource(rngSeed)) //nolint:gosec // reproducibility, not security
	ids := make([]string, 0, n)
	names := make(map[string]struct{}, n)
	for range n {
		name := itemName(r)
		desc := itemDescription(r)
		item, err := items.Create(ctx, repo, items.CreateRequest{Name: name, Description: desc})
		if err != nil {
			return nil, corpusStats{}, fmt.Errorf("items.Create: %w", err)
		}
		ids = append(ids, item.ID)
		names[name] = struct{}{}
	}
	return ids, corpusStats{TotalItems: len(ids), DistinctNames: len(names)}, nil
}

type roundConfig struct {
	pageSize    int
	searchLimit int
	totalItems  int
	warmup      int
	measure     int
	rngSeed     int64
}

func runRound(ctx context.Context, client *http.Client, baseURL string, ids []string, terms []string, cfg roundConfig) (RunResult, error) {
	r := rand.New(rand.NewSource(cfg.rngSeed)) //nolint:gosec // reproducibility, not security
	numPages := cfg.totalItems / cfg.pageSize
	if numPages < 1 {
		numPages = 1
	}

	nextListURL := func() string {
		offset := r.Intn(numPages) * cfg.pageSize
		return fmt.Sprintf("%s/api/v1/items?limit=%d&offset=%d", baseURL, cfg.pageSize, offset)
	}
	nextSearchURL := func() string {
		term := terms[r.Intn(len(terms))]
		return fmt.Sprintf("%s/api/v1/items?q=%s&limit=%d", baseURL, url.QueryEscape(term), cfg.searchLimit)
	}
	nextDetailURL := func() string {
		id := ids[r.Intn(len(ids))]
		return fmt.Sprintf("%s/api/v1/items/%s", baseURL, id)
	}

	for range cfg.warmup {
		if _, _, err := doGet(ctx, client, nextListURL()); err != nil {
			return RunResult{}, fmt.Errorf("warmup list: %w", err)
		}
		if _, _, err := doGet(ctx, client, nextSearchURL()); err != nil {
			return RunResult{}, fmt.Errorf("warmup search: %w", err)
		}
		if _, _, err := doGet(ctx, client, nextDetailURL()); err != nil {
			return RunResult{}, fmt.Errorf("warmup detail: %w", err)
		}
	}

	listSamples := make([]time.Duration, 0, cfg.measure)
	searchSamples := make([]time.Duration, 0, cfg.measure)
	detailSamples := make([]time.Duration, 0, cfg.measure)
	var searchHitsTotal int

	for range cfg.measure {
		d, _, err := doGet(ctx, client, nextListURL())
		if err != nil {
			return RunResult{}, fmt.Errorf("list: %w", err)
		}
		listSamples = append(listSamples, d)

		d, hits, err := doGetCountingItems(ctx, client, nextSearchURL())
		if err != nil {
			return RunResult{}, fmt.Errorf("search: %w", err)
		}
		searchSamples = append(searchSamples, d)
		searchHitsTotal += hits

		d, _, err = doGet(ctx, client, nextDetailURL())
		if err != nil {
			return RunResult{}, fmt.Errorf("detail: %w", err)
		}
		detailSamples = append(detailSamples, d)
	}

	return RunResult{
		List:            summarize(listSamples),
		Search:          summarize(searchSamples),
		Detail:          summarize(detailSamples),
		SearchHitsTotal: searchHitsTotal,
	}, nil
}

func doGet(ctx context.Context, client *http.Client, url string) (time.Duration, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	n, copyErr := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	elapsed := time.Since(started)
	if copyErr != nil {
		return 0, 0, fmt.Errorf("read body: %w", copyErr)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("%s -> %d (%d bytes)", url, resp.StatusCode, n)
	}
	return elapsed, int(n), nil
}

func doGetCountingItems(ctx context.Context, client *http.Client, url string) (time.Duration, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, err
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	elapsed := time.Since(started)
	if readErr != nil {
		return 0, 0, fmt.Errorf("read body: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("%s -> %d: %s", url, resp.StatusCode, string(body))
	}
	var envelope struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return 0, 0, fmt.Errorf("decode %s: %w", url, err)
	}
	return elapsed, len(envelope.Items), nil
}
