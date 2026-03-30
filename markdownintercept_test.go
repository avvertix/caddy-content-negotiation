package markdownintercept

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

	// Attempt directory traversal
	result := m.resolveMarkdownPath(dir, "/../../../etc/passwd")
	if result != "" {
		t.Errorf("directory traversal should not resolve, got %q", result)
	}
}
