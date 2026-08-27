package lang

// render is a thin alias over the production printer (Print), kept so the
// round-trip idempotency checks in parser_test.go read naturally.
func render(f *File) string { return Print(f) }
