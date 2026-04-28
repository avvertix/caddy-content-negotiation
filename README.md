# Content Negotiation module for Caddy

A Caddy middleware module that intercepts HTTP requests containing `Accept: text/markdown` and serves precomputed `.md` files located alongside the originally requested resources.

## How It Works

When a client sends a request with `Accept: text/markdown` (or `text/x-markdown`) in the header, this middleware:

1. Determines which `.md` file corresponds to the requested path
2. Checks if that `.md` file exists on disk
3. If found, serves the markdown content with `Content-Type: text/markdown; charset=utf-8`
4. If not found, passes the request to the next handler as normal (or returns `406` in strict mode)

When `strict_mode` is enabled, the middleware also rejects requests whose `Accept` header contains only types incompatible with text content (e.g. `image/png`) with `406 Not Acceptable`, before any file lookup takes place.

### Path Resolution Examples

| Request Path | Markdown File Checked |
|---|---|
| `/docs/page.html` | `/docs/page.md` |
| `/docs/page.php` | `/docs/page.md` |
| `/docs/` | `/docs/index.md` |
| `/about` | `/about.md` |
| `/` | `/index.md` |

## Installation

### Using xcaddy

Build Caddy with this module using `xcaddy`:

```bash
xcaddy build --with github.com/avvertix/caddy-content-negotiation
```

### Using Docker

A sample Docker setup is included. It builds a custom Caddy image with the
module baked in and serves the demo content in `docker/content/`.

```bash
# Build and start
docker compose up --build

# Test content negotiation
curl -H "Accept: text/markdown" http://localhost/
curl -H "Accept: text/markdown" http://localhost/docs/page.html
curl -H "Accept: text/markdown" http://localhost/about
```

To use your own content, mount a volume over `/srv` in `docker-compose.yml`
or copy files into `docker/content/` before building.

## Caddyfile Configuration

### Minimal

`markdown_intercept` is not a standard ordered directive, so you must register
its position in the global options block:

```caddyfile
{
    order markdown_intercept before file_server
}

example.com {
    markdown_intercept
    file_server
}
```

### Full Options

```caddyfile
{
    order markdown_intercept before file_server
}

example.com {
    markdown_intercept {
        root /var/www/html
        index_names index.html index.htm index.php
        extensions .html .htm .php .txt
        experimental_range_requests
        strict_mode
    }
    file_server
}
```

### Directives

| Directive | Default | Description |
|---|---|---|
| `root` | Site root (`{http.vars.root}`) | Filesystem path to look for `.md` files |
| `index_names` | `index.html index.htm index.php` | Index filenames to try for directory requests |
| `extensions` | `.html .htm .php .txt` | File extensions eligible for `.md` substitution |
| `experimental_range_requests` | disabled | Enable the `x-frontmatter` range unit (see below) |
| `strict_mode` | disabled | Reject unsupported `Accept` types with `406` (see below) |

## JSON Configuration

```json
{
  "handler": "markdown_intercept",
  "root": "/var/www/html",
  "index_names": ["index.html", "index.htm"],
  "extensions": [".html", ".htm", ".php"],
  "experimental_range_requests": true,
  "strict_mode": true
}
```

## Client Usage

Request markdown from any endpoint by setting the `Accept` header:

```bash
# Get the markdown version of a page
curl -H "Accept: text/markdown" https://example.com/docs/page.html

# Normal requests are unaffected
curl https://example.com/docs/page.html
```

### Frontmatter range requests (experimental)

When `experimental_range_requests` is enabled, clients can request only the
frontmatter block of a markdown file using the non-standard `x-frontmatter`
[range](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Range_requests) unit:

```bash
curl -H "Accept: text/markdown" \
     -H "Range: x-frontmatter" \
     https://example.com/docs/page.html
```

The server responds with `206 Partial Content` and only the frontmatter section
(the content between the opening and closing `---` delimiters). If the file has
no frontmatter block, the server returns `416 Range Not Satisfiable`.

When the feature is enabled, every markdown response includes
`Accept-Ranges: x-frontmatter` so clients can discover support before issuing a
range request.

### Strict content-type negotiation

When `strict_mode` is enabled the middleware enforces two rules:

**1. Unsupported `Accept` types are rejected with `406 Not Acceptable`**

If the request's `Accept` header contains only types outside the `text/*` family
and no `*/*` wildcard, the middleware returns `406` immediately without
performing any file lookup or calling the next handler. This rejects probes and
requests for content the server cannot produce for text-based resources:

```bash
# Rejected — not a text type
curl -i -H "Accept: image/png" https://example.com/docs/page.html
# → 406 Not Acceptable

curl -i -H "Accept: application/x-content-negotiation-probe" https://example.com/about
# → 406 Not Acceptable
```

Requests that include at least one compatible type are allowed through:

```bash
# Allowed — text/html matches text/*
curl -i -H "Accept: text/html, application/json" https://example.com/docs/page.html

# Allowed — wildcard covers everything
curl -i -H "Accept: */*" https://example.com/docs/page.html
```

**2. Missing markdown files return `406` instead of passing through**

When the client explicitly requests `text/markdown` but no `.md` file exists for
the requested path, the middleware returns `406` rather than forwarding the
request to the next handler:

```bash
# 406 if /docs/page.md does not exist
curl -i -H "Accept: text/markdown" https://example.com/docs/page.html
```

Without `strict_mode` the same request would be forwarded to the next handler
(e.g. a file server that serves the HTML version), and the `X-Content-Md:
requested` header would be added to the forwarded request.

## Response Headers

When a markdown file is served, the response includes:

- `Content-Type: text/markdown; charset=utf-8`
- `Accept-Ranges: x-frontmatter` (only when `experimental_range_requests` is enabled)

A `206 Partial Content` frontmatter response additionally includes:

- `Content-Range: x-frontmatter 0-<end>/<total>` — byte offsets of the frontmatter block within the full file

## Development

```bash
# Run tests
go test -v -race ./...

# Build Caddy locally with the module (requires xcaddy)
xcaddy build --with github.com/avvertix/caddy-content-negotiation=.
```
