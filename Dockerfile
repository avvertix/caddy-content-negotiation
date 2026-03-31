# Stage 1: Build Caddy with the markdown-intercept module
FROM caddy:2.11.2-builder AS builder

# Copy module source
COPY . /caddy-markdown-intercept

# Build Caddy with the module substituted from local source
RUN xcaddy build \
    --with github.com/avvertix/caddy-content-negotiation=/caddy-markdown-intercept

# Stage 2: Runtime image
FROM caddy:2.11.2

# Replace the stock caddy binary with our custom build
COPY --from=builder /usr/bin/caddy /usr/bin/caddy

# Copy sample Caddyfile
COPY docker/Caddyfile /etc/caddy/Caddyfile

# Copy sample web content (markdown + HTML files)
COPY docker/content /srv

EXPOSE 80 443 2019

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
  CMD caddy validate --config /etc/caddy/Caddyfile || exit 1
