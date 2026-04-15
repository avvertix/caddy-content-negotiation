package markdownintercept

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

// mockReplacer implements just enough of the Caddy replacer for tests.
type mockHandler struct {
	called bool
}

func (h *mockHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	h.called = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("original content"))
	return nil
}

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create directory structure:
	// dir/
	//   docs/
	//     page.html   (won't actually be served, but path exists)
	//     page.md
	//     index.md
	//   about.md
	//   readme.md
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)

	os.WriteFile(filepath.Join(dir, "docs", "page.md"), []byte("# Page\n\nHello from page.md"), 0o644)
	os.WriteFile(filepath.Join(dir, "docs", "index.md"), []byte("# Docs Index\n\nWelcome"), 0o644)
	os.WriteFile(filepath.Join(dir, "about.md"), []byte("# About\n\nAbout page"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Readme\n\nReadme content"), 0o644)

	return dir
}

func TestAcceptsMarkdown(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		want   bool
	}{
		// Basic matching
		{"exact text/markdown", "text/markdown", true},
		{"exact text/x-markdown", "text/x-markdown", true},
		{"with charset param", "text/markdown; charset=utf-8", true},

		// q-value: markdown must be tied for highest priority
		{"markdown and html equal priority", "text/html, text/markdown", true},
		{"markdown higher than html", "text/html;q=0.9, text/markdown;q=1.0", true},
		{"markdown lower than html", "text/html;q=1.0, text/markdown;q=0.9", false},
		{"markdown explicitly rejected q=0", "text/html, text/markdown;q=0", false},
		{"markdown only with q", "text/markdown;q=0.5", true},

		// Non-markdown types
		{"html only", "text/html", false},
		{"empty", "", false},
		{"wildcard", "*/*", false},
		{"text wildcard", "text/*", false},
		{"json", "application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}
			got := acceptsMarkdown(r)
			if got != tt.want {
				t.Errorf("acceptsMarkdown() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReplaceExtWithMd(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"page.html", "page.md"},
		{"index.php", "index.md"},
		{"file.txt", "file.md"},
		{"noext", "noext.md"},
		{"multi.dots.html", "multi.dots.md"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := replaceExtWithMd(tt.input)
			if got != tt.want {
				t.Errorf("replaceExtWithMd(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveMarkdownPath(t *testing.T) {
	dir := setupTestDir(t)

	m := &MarkdownIntercept{
		IndexNames: []string{"index.html", "index.htm", "index.php"},
		Extensions: []string{".html", ".htm", ".php", ".txt"},
	}

	tests := []struct {
		name    string
		reqPath string
		wantMd  bool
		wantEnd string // expected filename at end of resolved path
	}{
		{
			name:    "html file with md counterpart",
			reqPath: "/docs/page.html",
			wantMd:  true,
			wantEnd: "page.md",
		},
		{
			name:    "directory index",
			reqPath: "/docs/",
			wantMd:  true,
			wantEnd: "index.md",
		},
		{
			name:    "no extension with md file",
			reqPath: "/about",
			wantMd:  true,
			wantEnd: "about.md",
		},
		{
			name:    "nonexistent file",
			reqPath: "/docs/nonexistent.html",
			wantMd:  false,
		},
		{
			name:    "nonexistent directory",
			reqPath: "/nowhere/",
			wantMd:  false,
		},
		{
			name:    "root index",
			reqPath: "/docs/page.php",
			wantMd:  true,
			wantEnd: "page.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.resolveMarkdownPath(dir, tt.reqPath)
			if tt.wantMd && result == "" {
				t.Error("expected to find markdown file, got empty")
			}
			if !tt.wantMd && result != "" {
				t.Errorf("expected no markdown file, got %q", result)
			}
			if tt.wantMd && result != "" && filepath.Base(result) != tt.wantEnd {
				t.Errorf("expected filename %q, got %q", tt.wantEnd, filepath.Base(result))
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	dir := setupTestDir(t)

	if !fileExists(filepath.Join(dir, "docs", "page.md")) {
		t.Error("expected page.md to exist")
	}
	if fileExists(filepath.Join(dir, "docs", "nonexistent.md")) {
		t.Error("expected nonexistent.md to not exist")
	}
	if fileExists(filepath.Join(dir, "docs")) {
		t.Error("expected directory to not count as existing file")
	}
}

// TestServeHTTPIntegration tests the full middleware flow using a real replacer.
// Note: This requires a proper Caddy context, so we test the resolve logic
// more thoroughly and the handler in a simplified way.
func TestServeHTTPNoAcceptHeader(t *testing.T) {
	dir := setupTestDir(t)

	r := httptest.NewRequest("GET", "/docs/page.html", nil)
	// No Accept: text/markdown header
	_ = dir

	w := httptest.NewRecorder()
	next := &mockHandler{}

	// Since there's no Accept: text/markdown, it should pass through
	// We can't fully test ServeHTTP without a Caddy context, but we
	// can verify the accept check works
	if acceptsMarkdown(r) {
		t.Error("should not accept markdown without Accept header")
	}
	_ = next
	_ = w
}

func TestDirectoryTraversal(t *testing.T) {
	dir := setupTestDir(t)
	m := &MarkdownIntercept{
		IndexNames: []string{"index.html"},
		Extensions: []string{".html"},
	}

	traversalPaths := []string{
		"/../../../etc/passwd",
		"/docs/../../etc/passwd",
		"/docs/../../../etc/shadow",
		"/docs/%2F..%2F..%2Fetc%2Fpasswd", // percent-encoded (already decoded by net/http, but belt-and-suspenders)
		"/..",
		"/docs/..",
	}
	for _, p := range traversalPaths {
		result := m.resolveMarkdownPath(dir, p)
		if result != "" {
			t.Errorf("traversal path %q should not resolve, got %q", p, result)
		}
	}
}

// TestSafeJoin verifies that safeJoin blocks any path that escapes absRoot.
func TestSafeJoin(t *testing.T) {
	root := t.TempDir() // always absolute on all platforms

	tests := []struct {
		name      string
		elem      string
		wantEmpty bool
	}{
		{"normal file", "docs/page.md", false},
		{"nested subpath", filepath.Join("a", "b", "c.md"), false},
		{"root itself", ".", false},
		{"one level up", "..", true},
		{"one level up with file", filepath.Join("..", "escape.md"), true},
		{"two levels up", filepath.Join("..", "..", "etc", "passwd"), true},
		{"up then back in via subdir", filepath.Join("docs", "..", "..", "escape.md"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeJoin(root, tt.elem)
			if tt.wantEmpty && got != "" {
				t.Errorf("safeJoin(%q, %q) = %q; expected empty (traversal should be blocked)", root, tt.elem, got)
			}
			if !tt.wantEmpty && got == "" {
				t.Errorf("safeJoin(%q, %q) = empty; expected a path within root", root, tt.elem)
			}
			// Any non-empty result must actually be within root.
			if got != "" && got != root && !strings.HasPrefix(got, root+string(filepath.Separator)) {
				t.Errorf("safeJoin(%q, %q) = %q escapes root", root, tt.elem, got)
			}
		})
	}
}

// TestResolveMarkdownPathEdgeCases covers the branches not exercised by
// TestResolveMarkdownPath: unknown extension (case 4) and no-extension path
// that falls through to a directory index file (case 3b).
func TestResolveMarkdownPathEdgeCases(t *testing.T) {
	dir := setupTestDir(t)

	m := &MarkdownIntercept{
		IndexNames: []string{"index.html", "index.htm"},
		Extensions: []string{".html", ".htm"},
	}

	tests := []struct {
		name    string
		reqPath string
		wantMd  bool
		wantEnd string
	}{
		{
			// Unknown extensions are ignored — no .md lookup is attempted.
			name:    "unknown extension is ignored",
			reqPath: "/readme.rst",
			wantMd:  false,
		},
		{
			// Case 3b: /docs has no extension, docs.md does not exist, but
			// docs/index.md does — resolveMarkdownPath should find it.
			name:    "no extension resolves to directory index",
			reqPath: "/docs",
			wantMd:  true,
			wantEnd: "index.md",
		},
		{
			// Unknown extension with traversal segments: ignored outright.
			name:    "traversal via unknown extension path — ignored",
			reqPath: "/../../../etc/passwd.rst",
			wantMd:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.resolveMarkdownPath(dir, tt.reqPath)
			if tt.wantMd && result == "" {
				t.Errorf("expected markdown file, got empty")
			}
			if !tt.wantMd && result != "" {
				t.Errorf("expected no markdown file, got %q", result)
			}
			if tt.wantMd && result != "" && filepath.Base(result) != tt.wantEnd {
				t.Errorf("expected filename %q, got %q", tt.wantEnd, filepath.Base(result))
			}
		})
	}
}

// newRequestWithCaddyContext builds a request with the Caddy replacer injected
// into its context, which is required by ServeHTTP.
func newRequestWithCaddyContext(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	repl := caddy.NewReplacer()
	ctx := context.WithValue(r.Context(), caddy.ReplacerCtxKey, repl)
	return r.WithContext(ctx)
}

// TestServeHTTP exercises the full middleware handler for the three outcomes:
// markdown file served, pass-through with X-Content-Md header, and plain
// pass-through when the client does not request markdown.
func TestServeHTTP(t *testing.T) {
	dir := setupTestDir(t)

	m := MarkdownIntercept{
		Root:       dir,
		IndexNames: []string{"index.html", "index.htm"},
		Extensions: []string{".html", ".htm"},
		logger:     zap.NewNop(),
	}

	tests := []struct {
		name            string
		path            string
		acceptHeader    string
		wantStatus      int
		wantContentType string
		wantNextCalled  bool
		wantXContentMd  string // expected X-Content-Md on the request seen by next
	}{
		{
			name:            "markdown file found — serve it",
			path:            "/docs/page.html",
			acceptHeader:    "text/markdown",
			wantStatus:      http.StatusOK,
			wantContentType: "text/markdown; charset=utf-8",
			wantNextCalled:  false,
		},
		{
			name:           "markdown requested but no .md file — signal next",
			path:           "/docs/missing.html",
			acceptHeader:   "text/markdown",
			wantNextCalled: true,
			wantXContentMd: "requested",
		},
		{
			name:           "no Accept header — pass through silently",
			path:           "/docs/page.html",
			acceptHeader:   "",
			wantNextCalled: true,
			wantXContentMd: "", // must NOT be set
		},
		{
			name:           "html preferred over markdown — pass through silently",
			path:           "/docs/page.html",
			acceptHeader:   "text/html;q=1.0, text/markdown;q=0.5",
			wantNextCalled: true,
			wantXContentMd: "",
		},
		{
			name:            "directory index served as markdown",
			path:            "/docs/",
			acceptHeader:    "text/markdown",
			wantStatus:      http.StatusOK,
			wantContentType: "text/markdown; charset=utf-8",
			wantNextCalled:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRequestWithCaddyContext("GET", tt.path)
			if tt.acceptHeader != "" {
				r.Header.Set("Accept", tt.acceptHeader)
			}

			w := httptest.NewRecorder()
			next := &mockHandler{}

			if err := m.ServeHTTP(w, r, next); err != nil {
				t.Fatalf("ServeHTTP returned error: %v", err)
			}

			if next.called != tt.wantNextCalled {
				t.Errorf("next.called = %v, want %v", next.called, tt.wantNextCalled)
			}
			if tt.wantContentType != "" {
				if got := w.Header().Get("Content-Type"); got != tt.wantContentType {
					t.Errorf("Content-Type = %q, want %q", got, tt.wantContentType)
				}
			}
			if tt.wantStatus != 0 {
				if w.Code != tt.wantStatus {
					t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
				}
			}
			// X-Content-Md is set on the request (for downstream handlers).
			if got := r.Header.Get("X-Content-Md"); got != tt.wantXContentMd {
				t.Errorf("X-Content-Md = %q, want %q", got, tt.wantXContentMd)
			}
		})
	}
}
