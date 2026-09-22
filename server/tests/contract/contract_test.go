package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestOpenAPIOperationsMatchMountedRoutes(t *testing.T) {
	ops := loadOperations(t)
	docSet := make(map[string]struct{}, len(ops))
	for _, op := range ops {
		docSet[op.key()] = struct{}{}
	}

	bin := buildHHOBinary(t)
	out, err := exec.Command(bin, "routes").Output()
	if err != nil {
		var stderr []byte
		var exitErr *exec.ExitError
		if asExitError(err, &exitErr) {
			stderr = exitErr.Stderr
		}
		t.Fatalf("hho routes: %v\n%s", err, stderr)
	}

	srvSet := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		srvSet[line] = struct{}{}
	}
	if len(srvSet) == 0 {
		t.Fatal("hho routes printed no routes at all; cannot compare against the document")
	}

	var documentedNotMounted, mountedNotDocumented []string
	for k := range docSet {
		if _, ok := srvSet[k]; !ok {
			documentedNotMounted = append(documentedNotMounted, k)
		}
	}
	for k := range srvSet {
		if _, ok := docSet[k]; !ok {
			mountedNotDocumented = append(mountedNotDocumented, k)
		}
	}
	sort.Strings(documentedNotMounted)
	sort.Strings(mountedNotDocumented)

	if len(documentedNotMounted) == 0 && len(mountedNotDocumented) == 0 {
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "server/api/openapi.yaml and `hho routes` disagree on the operation set (constitution P-4), %d document-only and %d server-only:\n",
		len(documentedNotMounted), len(mountedNotDocumented))
	if len(documentedNotMounted) > 0 {
		b.WriteString("documented but NOT mounted:\n")
		for _, l := range documentedNotMounted {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	if len(mountedNotDocumented) > 0 {
		b.WriteString("mounted but NOT documented:\n")
		for _, l := range mountedNotDocumented {
			fmt.Fprintf(&b, "  - %s\n", l)
		}
	}
	t.Fatal(b.String())
}

func asExitError(err error, out **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*out = ee
	}
	return ok
}

func TestUnauthenticatedProbeMatchesSecurityAndResponses(t *testing.T) {
	ops := loadOperations(t)
	srv := startContractServer(t)

	for _, op := range ops {
		t.Run(op.key(), func(t *testing.T) {
			resp := srv.do(t, op.Method, op.probePath(), nil)
			respBody, err := io.ReadAll(resp.Body)
			closeErr := resp.Body.Close()
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			if closeErr != nil {
				t.Fatalf("close response body: %v", closeErr)
			}

			diag := fmt.Sprintf("request: %s %s\nresponse: %d\nbody: %s\n%s",
				op.Method, op.probePath(), resp.StatusCode, respBody, srv.diagnostics())

			t.Run("security", func(t *testing.T) {
				assertSecurityMatchesServer(t, op, resp.StatusCode, diag)
			})
			t.Run("documented", func(t *testing.T) {
				assertResponseIsDocumented(t, op, resp.StatusCode, resp.Header, respBody, diag)
			})
		})
	}
}

func assertSecurityMatchesServer(t *testing.T, op operation, status int, diag string) {
	t.Helper()

	if op.Public {
		if status == http.StatusUnauthorized {
			t.Errorf("%s is documented `security: []` (public) but answered 401 to an unauthenticated request -- a public operation must never require a credential\n%s",
				op.key(), diag)
		}
		return
	}

	if status == http.StatusUnauthorized {
		return
	}

	reason := ""
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		reason = " -- an unauthenticated caller received a successful response from an operation documented as requiring authentication"
	case http.StatusForbidden:
		reason = " -- 403 confirms the route/resource exists to an unauthenticated caller; this API refuses with 401 before any authorization decision is made (P-3/NFR-010)"
	case http.StatusNotFound:
		reason = " -- a foreign or unauthenticated caller must never learn whether a resource exists (P-3/NFR-010); authentication must be refused with 401 before any lookup runs"
	case http.StatusNotImplemented:
		reason = " -- a 501 stub (e.g. /sync/pull, /sync/push, T093) must still check authentication FIRST; answering its stub status to an unauthenticated caller means the stub runs ahead of auth"
	}
	t.Errorf("%s is documented as requiring authentication but answered %d, not 401, to an unauthenticated request%s\n%s",
		op.key(), status, reason, diag)
}

type problemBody map[string]any

func assertResponseIsDocumented(t *testing.T, op operation, status int, header http.Header, body []byte, diag string) {
	t.Helper()

	statusKey := strconv.Itoa(status)
	_, explicit := op.Responses[statusKey]
	_, hasDefault := op.Responses["default"]
	if !explicit && !hasDefault {
		t.Errorf("%s answered %d, which server/api/openapi.yaml documents neither as an explicit %q response nor under `default` -- the server answers a status its own contract does not list\n%s",
			op.key(), status, statusKey, diag)
	}

	if status < 400 {
		return
	}

	const wantMediaType = "application/problem+json"
	ct := header.Get("Content-Type")
	if !strings.HasPrefix(ct, wantMediaType) {
		t.Errorf("%s answered %d with Content-Type %q, want a %q prefix\n%s", op.key(), status, ct, wantMediaType, diag)
		return
	}

	var raw problemBody
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Errorf("%s answered %d with a body that does not parse as JSON: %v\n%s", op.key(), status, err, diag)
		return
	}

	for _, field := range []string{"type", "title", "status"} {
		if _, present := raw[field]; !present {
			t.Errorf("%s answered %d whose body is missing required Problem field %q (Problem's schema: required: [type, title, status])\n%s",
				op.key(), status, field, diag)
		}
	}

	if bodyStatus, ok := raw["status"].(float64); ok {
		if int(bodyStatus) != status {
			t.Errorf("%s answered HTTP %d but the Problem body's own `status` field says %v -- they must match\n%s",
				op.key(), status, raw["status"], diag)
		}
	}
}
