package contract

import (
	"fmt"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"patch": true, "head": true, "options": true, "trace": true,
}

type operation struct {
	Method       string
	PathTemplate string
	Public       bool
	Responses    map[string]struct{}
}

func (o operation) key() string { return o.Method + " " + o.PathTemplate }

var pathParamPattern = regexp.MustCompile(`\{[^{}]+\}`)

func (o operation) probePath() string {
	return pathParamPattern.ReplaceAllStringFunc(o.PathTemplate, func(string) string {
		return uuid.New().String()
	})
}

func loadOperations(t *testing.T) []operation {
	t.Helper()

	root, err := findServerModuleRoot()
	if err != nil {
		t.Fatalf("locate server module root: %v", err)
	}
	specPath := filepath.Join(root, "api", "openapi.yaml")
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	ops, err := parseOperations(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", specPath, err)
	}
	return ops
}

func parseOperations(raw []byte) ([]operation, error) {
	var doc struct {
		Servers []struct {
			URL string `yaml:"url"`
		} `yaml:"servers"`
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	if len(doc.Servers) == 0 || doc.Servers[0].URL == "" {
		return nil, fmt.Errorf("document declares no servers[0].url to anchor operation paths against")
	}
	base := strings.TrimRight(doc.Servers[0].URL, "/")

	var ops []operation
	for pathKey, pathItem := range doc.Paths {
		pathBase, err := pathServerBase(pathItem, base)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", pathKey, err)
		}

		for methodKey, rawOp := range pathItem {
			method := strings.ToLower(methodKey)
			if !httpMethods[method] {
				continue
			}
			opMap, ok := rawOp.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s %s: operation is %T, not a mapping", methodKey, pathKey, rawOp)
			}

			public, err := operationIsPublic(opMap)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", methodKey, pathKey, err)
			}
			responses, err := operationResponses(opMap)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", methodKey, pathKey, err)
			}

			ops = append(ops, operation{
				Method:       strings.ToUpper(methodKey),
				PathTemplate: pathBase + pathKey,
				Public:       public,
				Responses:    responses,
			})
		}
	}

	sort.Slice(ops, func(i, j int) bool {
		if ops[i].PathTemplate != ops[j].PathTemplate {
			return ops[i].PathTemplate < ops[j].PathTemplate
		}
		return ops[i].Method < ops[j].Method
	})
	return ops, nil
}

func pathServerBase(pathItem map[string]any, docBase string) (string, error) {
	raw, ok := pathItem["servers"]
	if !ok {
		return docBase, nil
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return "", fmt.Errorf("path-level `servers` override must be a non-empty list, got %T", raw)
	}
	first, ok := list[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("path-level servers[0] is %T, not a mapping", list[0])
	}
	url, ok := first["url"].(string)
	if !ok {
		return "", fmt.Errorf("path-level servers[0].url is %T, not a string", first["url"])
	}
	return strings.TrimRight(url, "/"), nil
}

func operationIsPublic(op map[string]any) (bool, error) {
	raw, ok := op["security"]
	if !ok {
		return true, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return false, fmt.Errorf("security field is %T, not a list", raw)
	}
	return len(list) == 0, nil
}

func operationResponses(op map[string]any) (map[string]struct{}, error) {
	raw, ok := op["responses"]
	if !ok {
		return nil, fmt.Errorf("operation declares no responses map at all")
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("responses field is %T, not a mapping", raw)
	}
	out := make(map[string]struct{}, len(m))
	for status := range m {
		out[status] = struct{}{}
	}
	return out, nil
}
