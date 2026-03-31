# About

This is a demo site for **caddy-markdown-intercept**, a Caddy module that
serves `.md` files when the client requests `Accept: text/markdown`.

## How it works

1. Client sends `Accept: text/markdown`
2. Middleware resolves the corresponding `.md` file path
3. If the file exists it is served with `Content-Type: text/markdown`
4. Otherwise the request passes through to the next handler
