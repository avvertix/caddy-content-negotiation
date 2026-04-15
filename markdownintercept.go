// Package markdownintercept provides a Caddy HTTP middleware that intercepts
// requests with an Accept header requesting markdown (text/markdown) and serves
// a precomputed .md file located alongside the originally requested resource.
//
// For example:
//   - GET /docs/page.html with Accept: text/markdown → serves /docs/page.md
//   - GET /docs/ with Accept: text/markdown → serves /docs/index.md
//   - GET /about with Accept: text/markdown → serves /about.md
package markdownintercept

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(MarkdownIntercept{})
	httpcaddyfile.RegisterHandlerDirective("markdown_intercept", parseCaddyfile)
}

// MarkdownIntercept is a Caddy middleware that checks if the client accepts
// text/markdown. If so, it looks for a .md file corresponding to the requested
// path and serves it instead of delegating to the next handler.
type MarkdownIntercept struct {
	// Root is the filesystem path from which to look for .md files.
	// Defaults to the current working directory or Caddy's configured root.
	Root string `json:"root,omitempty"`

	// IndexNames is the list of default index filenames to try when the
	// request path ends with "/". Defaults to ["index.html", "index.htm", "index.php"].
	IndexNames []string `json:"index_names,omitempty"`

	// Extensions is the list of file extensions to consider when rewriting
	// to .md. Defaults to [".html", ".htm", ".php", ".txt"].
	Extensions []string `json:"extensions,omitempty"`

	logger *zap.Logger
}

// CaddyModule returns the Caddy module information.
func (MarkdownIntercept) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.markdown_intercept",
		New: func() caddy.Module { return new(MarkdownIntercept) },
	}
}

// Provision sets up the module.
func (m *MarkdownIntercept) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger(m)

	if m.Root == "" {
		m.Root = "{http.vars.root}"
	}

	if len(m.IndexNames) == 0 {
		m.IndexNames = []string{"index.html", "index.htm", "index.php"}
	}

	if len(m.Extensions) == 0 {
		m.Extensions = []string{".html", ".htm", ".php", ".txt"}
	}

	return nil
}

// Validate ensures the module configuration is valid.
func (m *MarkdownIntercept) Validate() error {
	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (m MarkdownIntercept) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Check if the client accepts text/markdown
	if !acceptsMarkdown(r) {
		return next.ServeHTTP(w, r)
	}

	// Resolve the root directory, expanding any Caddy placeholders
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	root := repl.ReplaceAll(m.Root, ".")

	reqPath := r.URL.Path

	// Sanitize the path to prevent directory traversal
	reqPath = path.Clean("/" + reqPath)

	// Determine the markdown file path to look for
	mdPath := m.resolveMarkdownPath(root, reqPath)
	if mdPath == "" {
		// No markdown file found; signal to downstream handlers that the client
		// requested markdown, then pass through.
		m.logger.Debug("no markdown file found",
			zap.String("request_path", r.URL.Path),
		)
		r.Header.Set("X-Content-Md", "requested")
		return next.ServeHTTP(w, r)
	}

	m.logger.Debug("serving markdown file",
		zap.String("request_path", r.URL.Path),
		zap.String("markdown_file", mdPath),
	)

	// Read and serve the markdown file
	content, err := os.ReadFile(mdPath)
	if err != nil {
		m.logger.Error("failed to read markdown file",
			zap.String("path", mdPath),
			zap.Error(err),
		)
		return next.ServeHTTP(w, r)
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	// w.Header().Set("X-Markdown-Source", filepath.Base(mdPath))
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(content)
	return err
}

// resolveMarkdownPath attempts to find a .md file corresponding to the
// requested path. It returns the absolute filesystem path to the .md file,
// or an empty string if none is found.
func (m *MarkdownIntercept) resolveMarkdownPath(root, reqPath string) string {
	// Case 1: Path ends with "/" — try index files
	if strings.HasSuffix(reqPath, "/") {
		for _, idx := range m.IndexNames {
			mdName := replaceExtWithMd(idx)
			candidate := filepath.Join(root, filepath.FromSlash(reqPath), mdName)
			if fileExists(candidate) {
				return candidate
			}
		}
		return ""
	}

	// Case 2: Path has a recognized extension — replace it with .md
	ext := path.Ext(reqPath)
	if ext != "" && m.isKnownExtension(ext) {
		mdName := replaceExtWithMd(filepath.Base(reqPath))
		candidate := filepath.Join(root, filepath.FromSlash(path.Dir(reqPath)), mdName)
		if fileExists(candidate) {
			return candidate
		}
		return ""
	}

	// Case 3: No extension (e.g., /about) — try appending .md directly
	if ext == "" {
		candidate := filepath.Join(root, filepath.FromSlash(reqPath+".md"))
		if fileExists(candidate) {
			return candidate
		}
		// Also try as a directory with index
		for _, idx := range m.IndexNames {
			mdName := replaceExtWithMd(idx)
			candidate := filepath.Join(root, filepath.FromSlash(reqPath), mdName)
			if fileExists(candidate) {
				return candidate
			}
		}
		return ""
	}

	// Case 4: Unknown extension — try replacing with .md anyway
	mdName := replaceExtWithMd(filepath.Base(reqPath))
	candidate := filepath.Join(root, filepath.FromSlash(path.Dir(reqPath)), mdName)
	if fileExists(candidate) {
		return candidate
	}

	return ""
}

// isKnownExtension checks if ext is in the configured Extensions list.
func (m *MarkdownIntercept) isKnownExtension(ext string) bool {
	for _, e := range m.Extensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// acceptsMarkdown checks the Accept header to see if the client's most
// preferred type includes text/markdown or text/x-markdown.
//
// It respects q-values (RFC 9110 §12.4.2):
//   - q=0 means "not acceptable" — markdown is never served
//   - markdown must have a q-value >= the highest q-value of any other listed
//     type; if the client ranks something else higher, we pass through
func acceptsMarkdown(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}

	type entry struct {
		mediaType string
		q         float64
	}

	var all []entry
	for _, raw := range strings.Split(accept, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		q := 1.0
		if qStr, ok := params["q"]; ok {
			if v, err := strconv.ParseFloat(qStr, 64); err == nil {
				q = v
			}
		}
		all = append(all, entry{mediaType, q})
	}

	// Determine the highest q-value across all entries and the best
	// q-value for markdown specifically.
	maxQ := 0.0
	markdownQ := -1.0
	for _, e := range all {
		if e.q > maxQ {
			maxQ = e.q
		}
		if e.mediaType == "text/markdown" || e.mediaType == "text/x-markdown" {
			if e.q > markdownQ {
				markdownQ = e.q
			}
		}
	}

	// Serve markdown only when it is explicitly present, not rejected (q>0),
	// and tied for the highest preference among all listed types.
	return markdownQ > 0 && markdownQ >= maxQ
}

// replaceExtWithMd replaces the file extension with .md.
// e.g., "page.html" → "page.md", "index.php" → "index.md"
func replaceExtWithMd(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return filename + ".md"
	}
	return strings.TrimSuffix(filename, ext) + ".md"
}

// fileExists checks whether a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// UnmarshalCaddyfile sets up the module from Caddyfile tokens.
//
// Syntax:
//
//	markdown_intercept {
//	    root <path>
//	    index_names <name1> <name2> ...
//	    extensions <.ext1> <.ext2> ...
//	}
func (m *MarkdownIntercept) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "root":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.Root = d.Val()

			case "index_names":
				args := d.RemainingArgs()
				if len(args) == 0 {
					return d.ArgErr()
				}
				m.IndexNames = args

			case "extensions":
				args := d.RemainingArgs()
				if len(args) == 0 {
					return d.ArgErr()
				}
				m.Extensions = args

			default:
				return d.Errf("unrecognized subdirective '%s'", d.Val())
			}
		}
	}
	return nil
}

// parseCaddyfile parses the Caddyfile directive.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m MarkdownIntercept
	err := m.UnmarshalCaddyfile(h.Dispenser)
	return m, err
}

// Interface guards
var (
	_ caddy.Provisioner           = (*MarkdownIntercept)(nil)
	_ caddy.Validator             = (*MarkdownIntercept)(nil)
	_ caddyhttp.MiddlewareHandler = (*MarkdownIntercept)(nil)
	_ caddyfile.Unmarshaler       = (*MarkdownIntercept)(nil)
)
