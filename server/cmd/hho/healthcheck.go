package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/ankek/Household-Objects-Dev/server/internal/config"
	"io"
	"net"
	"net/http"
	"time"
)

const healthcheckUsage = `usage: hho healthcheck [--addr host:port]

Probes GET /api/v1/status and exits 0 when the server answers 200.

  --addr   address to probe (default from HHO_ADDR, else :7745)

Intended for a container HEALTHCHECK.
`

const healthcheckTimeout = 3 * time.Second

func runHealthcheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { _, _ = fmt.Fprint(stderr, healthcheckUsage) }
	addrFlag := fs.String("addr", "", "address to probe")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		_, _ = fmt.Fprintf(stderr, "hho healthcheck: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	addr, err := healthcheckAddr(*addrFlag)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "hho healthcheck: %v\n", err)
		return 1
	}

	if err := probe(addr); err != nil {
		_, _ = fmt.Fprintf(stderr, "hho healthcheck: %v\n", err)
		return 1
	}
	_, _ = fmt.Fprintln(stdout, "ok")
	return 0
}

func healthcheckAddr(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.Addr, nil
}

func probe(addr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
	defer cancel()

	url := "http://" + normalizeProbeHost(addr) + "/api/v1/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe %s: status %d", url, resp.StatusCode)
	}
	return nil
}

func normalizeProbeHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}
