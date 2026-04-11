// Package httptool runs Tool specs with type http (design doc §7.3, issue #20).
package httptool

//
// # Operation → HTTP mapping (MVP)
//
// The workflow "operation" string (after tool.<name>.) is split on ".":
//
//   - If the first segment is get, post, put, delete, or patch (case-insensitive),
//     that becomes the HTTP method and the remaining segments form the path joined with "/"
//     (with a leading slash). Example: post.api.v1.items → POST /api/v1/items
//
//   - Otherwise the method is GET and all segments form the path.
//     Example: health.live → GET /health/live
//
// baseUrl and path are concatenated (trailing slash on baseUrl is stripped).
