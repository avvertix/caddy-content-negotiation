# Content Negotiation module for Caddy

A Caddy middleware module that intercepts HTTP requests containing `Accept: text/markdown` and serves precomputed `.md` files located alongside the originally requested resources.

## How It Works

When a client sends a request with `Accept: text/markdown` (or `text/x-markdown`) in the header, this middleware:

1. Determines which `.md` file corresponds to the requested path
2. Checks if that `.md` file exists on disk
3. If found, serves the markdown content with `Content-Type: text/markdown; charset=utf-8`
4. If not found, passes the request to the next handler as normal

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

## JSON Configuration

```json
{
  "handler": "markdown_intercept",
  "root": "/var/www/html",
  "index_names": ["index.html", "index.htm"],
  "extensions": [".html", ".htm", ".php"]
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

## Response Headers

When a markdown file is served, the response includes:

- `Content-Type: text/markdown; charset=utf-8`

## Development

```bash
# Run tests
go test -v -race ./...

# Build Caddy locally with the module (requires xcaddy)
xcaddy build --with github.com/avvertix/caddy-content-negotiation=.
```
