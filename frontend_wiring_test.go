package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This test guards the boundary between the Vue frontend and the Go backend.
// The frontend calls methods through generated bindings, so a renamed or
// missing Go method (or a misspelled event name) only shows up as a runtime
// error in the packaged app - exactly the kind of breakage a build cannot see.

var (
	appImportRe = regexp.MustCompile(`(?s)import\s*\{([^}]*)\}\s*from\s*['"][^'"]*wailsjs/go/main/App['"]`)
	eventsOnRe  = regexp.MustCompile(`EventsOn\(\s*['"]([^'"]+)['"]`)
)

func frontendSources(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("frontend", "src")
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".vue", ".ts", ".js":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Skipf("frontend sources unavailable: %v", err)
	}
	return files
}

// TestFrontendCallsExistOnApp fails when a Vue component imports a binding that
// the Go App does not export.
func TestFrontendCallsExistOnApp(t *testing.T) {
	sources := frontendSources(t)

	appType := reflect.TypeOf(&App{})
	methods := map[string]bool{}
	for i := 0; i < appType.NumMethod(); i++ {
		methods[appType.Method(i).Name] = true
	}

	called := map[string]bool{}
	for path, src := range sources {
		for _, m := range appImportRe.FindAllStringSubmatch(src, -1) {
			for _, raw := range strings.Split(m[1], ",") {
				name := strings.TrimSpace(raw)
				if name == "" {
					continue
				}
				// "Foo as Bar" still calls Foo.
				if i := strings.Index(name, " as "); i >= 0 {
					name = strings.TrimSpace(name[:i])
				}
				if name == "" {
					continue
				}
				called[name] = true
				if !methods[name] {
					t.Errorf("%s imports %s, but App has no such method", path, name)
				}
			}
		}
	}

	if len(called) < 20 {
		t.Fatalf("only found %d bindings; the import parser is probably broken", len(called))
	}
	t.Logf("checked %d frontend bindings against %d App methods", len(called), len(methods))
}

// TestFrontendListensToEmittedEvents fails when a component subscribes to an
// event the backend never sends (or sends under a different name).
func TestFrontendListensToEmittedEvents(t *testing.T) {
	sources := frontendSources(t)

	goSource := strings.Builder{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		goSource.Write(data)
	}
	backend := goSource.String()

	listened := map[string]bool{}
	for _, src := range sources {
		for _, m := range eventsOnRe.FindAllStringSubmatch(src, -1) {
			listened[m[1]] = true
		}
	}
	if len(listened) == 0 {
		t.Fatal("no EventsOn calls found; the parser is probably broken")
	}

	names := make([]string, 0, len(listened))
	for name := range listened {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !strings.Contains(backend, `"`+name+`"`) {
			t.Errorf("the frontend listens for %q but no Go file mentions it", name)
		}
	}
	t.Logf("checked %d event subscriptions: %v", len(names), names)
}
