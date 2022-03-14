// Package debugprocessor contains an implementation of OpenTelemetry Go's
// SpanProcessor interface that prints trace information to the console, or any
// other io.Writer provided.
//
// It's inspired by Rust's tracing-tree crate that does a wonderful job of
// displaying trace information in a format that is convenient to consume in a
// terminal context.
package debugprocessor
