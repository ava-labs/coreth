// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

const repo = "github.com/ava-labs/coreth"

// getDependencies takes a fully qualified package name and returns a map of all
// its package imports (including itself) in the same format.
// If recursive is true, returns all transitive dependencies.
// If recursive is false, returns only direct dependencies.
func getDependencies(packageName string, recursive bool) (map[string]struct{}, error) {
	// Configure the load mode to include dependencies
	cfg := &packages.Config{Mode: packages.NeedImports | packages.NeedName}
	pkgs, err := packages.Load(cfg, packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to load package: %w", err)
	}

	if len(pkgs) == 0 || pkgs[0].Errors != nil {
		return nil, fmt.Errorf("failed to load package %s", packageName)
	}

	deps := make(map[string]struct{})

	if recursive {
		// Recursive collection (original behavior)
		var collectDeps func(pkg *packages.Package)
		collectDeps = func(pkg *packages.Package) {
			if _, ok := deps[pkg.PkgPath]; ok {
				return // Avoid re-processing the same dependency
			}
			deps[pkg.PkgPath] = struct{}{}
			for _, dep := range pkg.Imports {
				collectDeps(dep)
			}
		}

		// Start collecting dependencies
		for _, pkg := range pkgs {
			collectDeps(pkg)
		}
	} else {
		// Direct dependencies only
		for _, pkg := range pkgs {
			deps[pkg.PkgPath] = struct{}{} // Include the package itself
			for _, dep := range pkg.Imports {
				deps[dep.PkgPath] = struct{}{}
			}
		}
	}

	return deps, nil
}

func TestMustNotImport(t *testing.T) {
	withRepo := func(pkg string) string {
		return fmt.Sprintf("%s/%s", repo, pkg)
	}
	mustNotImport := map[string][]string{
		// The following sub-packages of plugin/evm must not import core, core/vm
		// so clients (e.g., wallets, e2e tests) can import them without pulling in
		// the entire VM logic.
		// Importing these packages configures libevm globally and it is not
		// possible to do so for both coreth and subnet-evm, where the client may
		// wish to connect to multiple chains.

		"plugin/evm/atomic":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/client":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/config":       {"core", "plugin/evm/customtypes", "core/extstate", "params"},
		"plugin/evm/customheader": {"core", "core/extstate", "core/vm", "params"},
		"ethclient":               {"plugin/evm/customtypes", "core/extstate", "params"},
		"warp":                    {"plugin/evm/customtypes", "core/extstate", "params"},
	}

	for packageName, forbiddenImports := range mustNotImport {
		imports, err := getDependencies(withRepo(packageName), true) // recursive=true for transitive deps
		require.NoError(t, err)

		for _, forbiddenImport := range forbiddenImports {
			fullForbiddenImport := withRepo(forbiddenImport)
			_, found := imports[fullForbiddenImport]
			require.False(t, found, "package %s must not import %s, check output of go list -f '{{ .Deps }}' \"%s\" ", packageName, fullForbiddenImport, withRepo(packageName))
		}
	}
}

// TestLibevmImportsAreAllowed ensures that all libevm imports in the codebase
// are explicitly allowed via the eth-allowed-packages.txt file.
func TestLibevmImportsAreAllowed(t *testing.T) {
	allowedPackages, err := loadAllowedPackages("../../scripts/eth-allowed-packages.txt")
	require.NoError(t, err, "Failed to load allowed packages")

	// Find all libevm imports in source files with proper filtering
	foundImports, err := findFilteredLibevmImportsWithFiles("../..")
	require.NoError(t, err, "Failed to find libevm imports")

	// Check for any imports not in the allowed list and build detailed error message
	var extraImports []string
	for importPath := range foundImports {
		if _, allowed := allowedPackages[importPath]; !allowed {
			extraImports = append(extraImports, importPath)
		}
	}

	slices.Sort(extraImports)

	if len(extraImports) > 0 {
		var errorMsg strings.Builder
		errorMsg.WriteString("New libevm imports should be added to ./scripts/eth-allowed-packages.txt to prevent accidental imports:\n\n")

		for _, importPath := range extraImports {
			files := foundImports[importPath]
			fileList := make([]string, 0, len(files))
			for file := range files {
				fileList = append(fileList, file)
			}
			slices.Sort(fileList)

			errorMsg.WriteString(fmt.Sprintf("- %s\n", importPath))
			errorMsg.WriteString(fmt.Sprintf("   Used in %d file(s):\n", len(fileList)))
			for _, file := range fileList {
				errorMsg.WriteString(fmt.Sprintf("   • %s\n", file))
			}
			errorMsg.WriteString("\n")
		}

		require.Fail(t, errorMsg.String())
	}
}

// loadAllowedPackages reads the allowed packages from the specified file
func loadAllowedPackages(filename string) (map[string]struct{}, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open allowed packages file: %w", err)
	}
	defer file.Close()

	allowed := make(map[string]struct{})
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			// Remove quotes if present
			line = strings.Trim(line, `"`)
			allowed[line] = struct{}{}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read allowed packages file: %w", err)
	}

	return allowed, nil
}

// findFilteredLibevmImportsWithFiles finds all libevm imports in the codebase,
// excluding underscore imports and "eth*" named imports.
// Returns a map of import paths to the set of files that contain them
func findFilteredLibevmImportsWithFiles(rootDir string) (map[string]map[string]struct{}, error) {
	imports := make(map[string]map[string]struct{})
	libevmRegex := regexp.MustCompile(`^github\.com/ava-labs/libevm/`)

	return imports, filepath.Walk(rootDir, func(path string, _ os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".go") {
			return err
		}

		// Skip generated files and main_test.go
		filename := filepath.Base(path)
		if strings.HasPrefix(filename, "gen_") || strings.Contains(path, "core/main_test.go") {
			return nil
		}

		node, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
		if err != nil {
			return nil //nolint:nilerr // Skip unparseable files
		}

		for _, imp := range node.Imports {
			if imp.Path == nil {
				continue
			}

			importPath := strings.Trim(imp.Path.Value, `"`)
			if !libevmRegex.MatchString(importPath) {
				continue
			}

			// Skip underscore and "eth*" named imports
			if imp.Name != nil && (imp.Name.Name == "_" || strings.HasPrefix(imp.Name.Name, "eth")) {
				continue
			}

			if imports[importPath] == nil {
				imports[importPath] = make(map[string]struct{})
			}
			imports[importPath][path] = struct{}{}
		}
		return nil
	})
}
