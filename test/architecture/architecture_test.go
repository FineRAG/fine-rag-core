package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modulePath = "enterprise-go-rag"

var dependencyRules = map[string][]string{
	"internal/contracts":           {},
	"internal/services/ingestion":  {"internal/contracts"},
	"internal/services/retrieval":  {"internal/contracts", "internal/logging"},
	"internal/services/governance": {"internal/contracts"},
}

func TestArchitectureDependencyDirection(t *testing.T) {
	for pkg := range dependencyRules {
		imports, err := listPackageImports(pkg)
		if err != nil {
			t.Fatalf("list imports for %s: %v", pkg, err)
		}
		for _, imp := range imports {
			if !strings.HasPrefix(imp, modulePath+"/") {
				continue
			}
			target := strings.TrimPrefix(imp, modulePath+"/")
			if strings.HasPrefix(target, "internal/services/") && pkg == "internal/contracts" {
				t.Fatalf("forbidden dependency: %s -> %s", pkg, target)
			}
			if !isAllowed(pkg, target) {
				t.Fatalf("dependency not allowed by E1-T1 map: %s -> %s", pkg, target)
			}
		}
	}
}

func isAllowed(pkg string, imported string) bool {
	if pkg == imported {
		return true
	}
	allowed := dependencyRules[pkg]
	for _, candidate := range allowed {
		if imported == candidate {
			return true
		}
	}
	return false
}

func listPackageImports(pkg string) ([]string, error) {
	repoRoot, err := repositoryRoot()
	if err != nil {
		return nil, err
	}

	pkgPath := filepath.Join(repoRoot, pkg)
	entries := make([]string, 0)
	fset := token.NewFileSet()

	err = filepath.Walk(pkgPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range file.Imports {
			entries = append(entries, strings.Trim(imp.Path.Value, "\""))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return dedupe(entries), nil
}

func dedupe(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func TestArchitectureRulesDocumented(t *testing.T) {
	repoRoot, err := repositoryRoot()
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	path := filepath.Join(repoRoot, "docs", "architecture", "dependency-map.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected architecture dependency map at %s: %v", path, err)
	}
}

func TestArchitectureNoCrossServiceImports(t *testing.T) {
	servicePkgs := []string{
		"internal/services/ingestion",
		"internal/services/retrieval",
		"internal/services/governance",
	}

	for _, pkg := range servicePkgs {
		imports, err := listPackageImports(pkg)
		if err != nil {
			t.Fatalf("list imports for %s: %v", pkg, err)
		}
		for _, imp := range imports {
			if !strings.HasPrefix(imp, modulePath+"/internal/services/") {
				continue
			}
			t.Fatalf("cross-service dependency forbidden: %s imports %s", pkg, strings.TrimPrefix(imp, modulePath+"/"))
		}
	}
}

func repositoryRoot() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..")), nil
}
