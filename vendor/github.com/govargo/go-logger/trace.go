package logger

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"
)

const (
	traceKey        = "logging.googleapis.com/trace"
	spanKey         = "logging.googleapis.com/spanId"
	traceSampledKey = "logging.googleapis.com/trace_sampled"
)

// addCloudLoggingFields adds the correct Cloud Logging "trace", "span", "trace_sampled" fields
// see: https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry
func addCloudLoggingFields(trace string, spanId string, sampled bool, projectName string) (traceFiled zap.Field, spanField zap.Field, sampledField zap.Field) {
	return zap.String(traceKey, fmt.Sprintf("projects/%s/traces/%s", projectName, trace)),
		zap.String(spanKey, spanId),
		zap.Bool(traceSampledKey, sampled)
}

// getTraceContext gets trace value from context.Context by trace key
func getTraceContext(ctx context.Context) *Trace {
	raw, _ := ctx.Value(TraceCtxKey).(*Trace)
	return raw
}

// TraceContext returns zap.Fields for grouping for Cloud Logging
func TraceContext(ctx context.Context) (traceId zap.Field, spanId zap.Field, sampled zap.Field) {
	trace := getTraceContext(ctx)
	if trace != nil {
		traceField, spanField, sampledField := addCloudLoggingFields(trace.TraceID, trace.SpanID, trace.Sampled, os.Getenv("GOOGLE_CLOUD_PROJECT"))
		return traceField, spanField, sampledField
	}
	return zap.Skip(), zap.Skip(), zap.Skip()
}
