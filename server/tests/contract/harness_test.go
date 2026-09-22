package contract

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

var (
	binOnce  sync.Once
	binPath  string
	binSkip  string
	binFatal string
)

func buildHHOBinary(t *testing.T) string {
	t.Helper()
	binOnce.Do(func() {
		goBin, err := exec.LookPath("go")
		if err != nil {
			binSkip = fmt.Sprintf("no go toolchain on PATH, so the real hho binary cannot be built for these contract tests: %v", err)
			return
		}

		moduleRoot, err := findServerModuleRoot()
		if err != nil {
			binFatal = err.Error()
			return
		}

		dir, err := os.MkdirTemp("", "hho-contract-bin-*")
		if err != nil {
			binFatal = fmt.Sprintf("create temp dir for hho binary: %v", err)
			return
		}

		out := filepath.Join(dir, "hho")
		if runtime.GOOS == "windows" {
			out += ".exe"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, goBin, "build", "-o", out, "./cmd/hho")
		cmd.Dir = moduleRoot
		cmdOut, err := cmd.CombinedOutput()
		if err != nil {
			binFatal = fmt.Sprintf("go build ./cmd/hho: %v\n%s", err, cmdOut)
			return
		}
		binPath = out
	})

	if binSkip != "" {
		t.Skip(binSkip)
	}
	if binFatal != "" {
		t.Fatal(binFatal)
	}
	return binPath
}

func findServerModuleRoot() (string, error) {
	dir, err := filepath.Abs(".")
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod above %s", dir)
		}
		dir = parent
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) writeLine(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.WriteString(line)
	b.buf.WriteByte('\n')
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func drainLines(r io.Reader, dst *syncBuffer, onLine func(line string)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		dst.writeLine(line)
		if onLine != nil {
			onLine(line)
		}
	}
}

type contractServer struct {
	baseURL string
	client  *http.Client

	cmd    *exec.Cmd
	stdout *syncBuffer
	stderr *syncBuffer
}

type serveLogLine struct {
	Msg  string `json:"msg"`
	Addr string `json:"addr"`
}

func startContractServer(t *testing.T) *contractServer {
	t.Helper()
	bin := buildHHOBinary(t)
	dataDir := t.TempDir()

	cmd := exec.Command(bin, "serve", "--addr", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "HHO_DATA_DIR="+dataDir)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("hho serve: obtain stderr pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("hho serve: obtain stdout pipe: %v", err)
	}

	srv := &contractServer{
		client: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cmd:    cmd,
		stdout: &syncBuffer{},
		stderr: &syncBuffer{},
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start hho serve: %v", err)
	}

	t.Cleanup(func() {
		if cmd.Process == nil {
			return
		}
		_ = cmd.Process.Kill()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})

	addrCh := make(chan string, 1)
	var addrOnce sync.Once
	go drainLines(stderrPipe, srv.stderr, func(line string) {
		var rec serveLogLine
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Msg == "hho serve" && rec.Addr != "" {
			addrOnce.Do(func() { addrCh <- rec.Addr })
		}
	})
	go drainLines(stdoutPipe, srv.stdout, nil)

	var addr string
	select {
	case addr = <-addrCh:
	case <-time.After(15 * time.Second):
		t.Fatalf("hho serve did not report its listen address within 15s\n--- stdout ---\n%s--- stderr ---\n%s",
			srv.stdout.String(), srv.stderr.String())
	}

	srv.baseURL = "http://" + addr

	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for {
		resp, err := srv.client.Get(srv.baseURL + "/api/v1/status")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
			lastErr = fmt.Errorf("GET /api/v1/status: status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			t.Fatalf("GET %s/status never answered 200 before timeout: %v\n--- stderr ---\n%s",
				srv.baseURL, lastErr, srv.stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	return srv
}

func (srv *contractServer) do(t *testing.T, method, path string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.client.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func (srv *contractServer) diagnostics() string {
	return "--- stderr ---\n" + srv.stderr.String() + "--- stdout ---\n" + srv.stdout.String()
}
