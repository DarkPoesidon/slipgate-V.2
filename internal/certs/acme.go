package certs

// ACME/Let's Encrypt is handled automatically by Caddy (caddy-naive).
// No explicit ACME client needed — Caddy's TLS directive with an email
// address triggers automatic certificate provisioning.
//
// The NaiveProxy Caddyfile generated in transport/naive.go contains:
//   tls user@example.com
// which tells Caddy to obtain and renew certs via Let's Encrypt.
