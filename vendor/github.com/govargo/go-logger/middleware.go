package logger

import (
	"context"
	"net/http"
	"regexp"
)

var TraceCtxKey = &contextTraceKey{"trace"}

type contextTraceKey struct {
	name string
}

type Trace struct {
	TraceID string
	SpanID  string
	Sampled bool
}

func TraceLoggerMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		header := r.Header.Get("X-Cloud-Trace-Context")
		if len(header) > 0 {
			traceID, spanID, sampled := deconstructXCloudTraceContext(header)

			t := &Trace{
				TraceID: traceID,
				SpanID:  spanID,
				Sampled: sampled,
			}

			ctx = context.WithValue(ctx, TraceCtxKey, t)
			r = r.WithContext(ctx)
		}

		h.ServeHTTP(w, r)
	})
}

var reCloudTraceContext = regexp.MustCompile(
	// Matches on "TRACE_ID"
	`([a-f\d]+)?` +
		// Matches on "/SPAN_ID"
		`(?:/([a-f\d]+))?` +
		// Matches on ";0=TRACE_TRUE"
		`(?:;o=(\d))?`)

func deconstructXCloudTraceContext(s string) (traceID, spanID string, traceSampled bool) {
	matches := reCloudTraceContext.FindStringSubmatch(s)

	traceID, spanID, traceSampled = matches[1], matches[2], matches[3] == "1"

	if spanID == "0" {
		spanID = ""
	}

	return
}
