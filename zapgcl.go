/*
Package gcloudzap provides a zap logger that forwards entries to the Google
Stackdriver Logging service as structured payloads.

All zap.Logger instances created with this package are safe for concurrent
use.

Network calls (which are delegated to the Google Cloud Platform package) are
asynchronous and payloads are buffered. These benchmarks, on a MacBook Pro 2.4
GHz Core i5, are a loose approximation of latencies on the critical path for
the zapcore.Core implementation provided by this package.

	$ go test -bench . github.com/dhduvall/gcloudzap
	goos: darwin
	goarch: amd64
	pkg: github.com/dhduvall/gcloudzap
	BenchmarkCoreClone-4   	 2000000	       607 ns/op
	BenchmarkCoreWrite-4   	 1000000	      2811 ns/op


Zap docs: https://godoc.org/go.uber.org/zap

Stackdriver Logging docs: https://cloud.google.com/logging/docs/

*/
package zapgcl

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	gcl "cloud.google.com/go/logging"
	"github.com/blendle/zapdriver"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	logpb "google.golang.org/genproto/googleapis/logging/v2"
)

const (
	// InsertIDKey is the payload field key to use to set the insertId field
	// in the LogEntry object.
	InsertIDKey = "logging.googleapis.com/insertId"
)

func newClient(projectID string) (*gcl.Client, error) {
	if projectID == "" {
		return nil, newError("the provided projectID is empty")
	}

	return gcl.NewClient(context.Background(), projectID)
}

// NewDevelopment builds a development Logger that writes DebugLevel and above
// logs to standard error in a human-friendly format, as well as to
// Stackdriver using Application Default Credentials.
func NewDevelopment(projectID string, logID string) (*zap.Logger, error) {
	if logID == "" {
		return nil, fmt.Errorf("the provided logID is empty")
	}

	client, err := newClient(projectID)
	if err != nil {
		return nil, newError("creating Google Logging client: %v", err)
	}

	return New(zap.NewDevelopmentConfig(), client, logID)
}

// NewProduction builds a production Logger that writes InfoLevel and above
// logs to standard error as JSON, as well as to Stackdriver using Application
// Default Credentials.
func NewProduction(projectID string, logID string) (*zap.Logger, error) {
	if logID == "" {
		return nil, fmt.Errorf("the provided logID is empty")
	}

	client, err := newClient(projectID)
	if err != nil {
		return nil, newError("creating Google Logging client: %v", err)
	}

	return New(zap.NewProductionConfig(), client, logID)
}

// New creates a new zap.Logger which will write entries to Stackdriver in
// addition to the destination specified by the provided zap configuration.
func New(cfg zap.Config, client *gcl.Client, logID string, opts ...zap.Option) (*zap.Logger, error) {
	zl, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	if client == nil {
		return nil, fmt.Errorf("The provided GCL client is nil")
	}

	// Here we translate all the members of a zap.Config into a zap.Option
	// array to pass to zap.New(), since otherwise the config passed in to
	// zl by cfg.Build() is lost when we grab its core; we basically copy
	// zap.Config.buildOptions().
	var nopts []zap.Option
	if cfg.Development {
		nopts = append(nopts, zap.Development())
	}

	if !cfg.DisableCaller {
		nopts = append(nopts, zap.AddCaller())
	}

	stackLevel := zap.ErrorLevel
	if cfg.Development {
		stackLevel = zap.WarnLevel
	}
	if !cfg.DisableStacktrace {
		nopts = append(nopts, zap.AddStacktrace(stackLevel))
	}

	if cfg.Sampling != nil {
		nopts = append(nopts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSampler(core, time.Second,
				int(cfg.Sampling.Initial), int(cfg.Sampling.Thereafter))
		}))
	}

	if len(cfg.InitialFields) > 0 {
		fs := make([]zap.Field, 0, len(cfg.InitialFields))
		keys := make([]string, 0, len(cfg.InitialFields))
		for k := range cfg.InitialFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fs = append(fs, zap.Any(k, cfg.InitialFields[k]))
		}
		nopts = append(nopts, zap.Fields(fs...))
	}

	// The user-supplied options must override our defaults
	opts = append(nopts, opts...)

	tee := Tee(zl.Core(), client, logID)
	return zap.New(tee, opts...), nil
}

// A Core implements zapcore.Core and writes entries to a Logger from the
// Google Cloud package.
//
// It's safe for concurrent use by multiple goroutines as long as it's not
// mutated after first use.
type Core struct {
	// Logger is a logging.Logger instance from the Google Cloud Platform Go
	// library.
	Logger GoogleCloudLogger

	// Provide your own mapping of zapcore's Levels to Google's Severities, or
	// use DefaultSeverityMapping. All of the Core's children will default to
	// using this map.
	//
	// This must not be mutated after the Core's first use.
	SeverityMapping map[zapcore.Level]gcl.Severity

	// MinLevel is the minimum level for a log entry to be written.
	MinLevel zapcore.Level

	// fields should be built once and never mutated again.
	fields map[string]interface{}
}

// Tee returns a zapcore.Core that writes entries to both the provided core
// and to Stackdriver using the provided client and log ID.
//
// For fields to be written to Stackdriver, you must use the With() method on
// the returned Core rather than just on zc. (This function has no way of
// knowing about fields that already exist on zc. They will be preserved when
// writing to zc's existing destination, but not to Stackdriver.)
func Tee(zc zapcore.Core, client *gcl.Client, gclLogID string) zapcore.Core {
	gc := &Core{
		Logger:          client.Logger(gclLogID),
		SeverityMapping: DefaultSeverityMapping,
	}

	for l := zapcore.DebugLevel; l <= zapcore.FatalLevel; l++ {
		if zc.Enabled(l) {
			gc.MinLevel = l
			break
		}
	}

	return zapcore.NewTee(zc, gc)
}

// Enabled implements zapcore.Core.
func (c *Core) Enabled(l zapcore.Level) bool {
	return l >= c.MinLevel
}

// With implements zapcore.Core.
func (c *Core) With(newFields []zapcore.Field) zapcore.Core {
	return &Core{
		Logger:          c.Logger,
		SeverityMapping: c.SeverityMapping,
		MinLevel:        c.MinLevel,
		fields:          clone(c.fields, newFields),
	}
}

// Check implements zapcore.Core.
func (c *Core) Check(e zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(e.Level) {
		return ce.AddCore(e, c)
	}
	return ce
}

// Write implements zapcore.Core. It writes a log entry to Stackdriver.
//
// Certain fields in the zapcore.Entry are used to populate the Stackdriver
// entry.  The Message field maps to "message", and the LoggerName and Stack
// fields map to "logger" and "stack", respectively, if they're present.  The
// Caller field is mapped to the Stackdriver entry object's SourceLocation
// field.
func (c *Core) Write(ze zapcore.Entry, newFields []zapcore.Field) error {
	severity, specified := c.SeverityMapping[ze.Level]
	if !specified {
		severity = gcl.Default
	}

	payload := clone(c.fields, newFields)

	if ze.Stack != "" {
		payload["stack"] = ze.Stack
	}
	payload["message"] = ze.Message

	entry := gcl.Entry{
		Timestamp: ze.Time,
		Severity:  severity,
		Payload:   payload,
	}
	if ze.LoggerName != "" {
		entry.LogName = ze.LoggerName
	}

	for k, v := range payload {
		if strings.HasPrefix(k, "labels.") {
			if len(entry.Labels) == 0 {
				entry.Labels = make(map[string]string)
			}
			new_k := k[7:]
			entry.Labels[new_k] = v.(string)
			delete(payload, k)
		} else if k == "httpRequest" {
			_v := v.(*zapdriver.HTTPPayload)
			req := &http.Request{
				Method: _v.RequestMethod,
				Header: make(http.Header),
			}
			req.URL, _ = url.Parse(_v.RequestURL)
			req.Header.Set("User-Agent", _v.UserAgent)
			req.Header.Set("Referer", _v.Referer)
			reqSize, _ := strconv.ParseInt(_v.RequestSize, 10, 64)
			respSize, _ := strconv.ParseInt(_v.ResponseSize, 10, 64)
			latency, _ := time.ParseDuration(_v.Latency)
			entry.HTTPRequest = &gcl.HTTPRequest{
				Request:                        req,
				RequestSize:                    reqSize,
				Status:                         _v.Status,
				ResponseSize:                   respSize,
				Latency:                        latency,
				LocalIP:                        _v.ServerIP,
				RemoteIP:                       _v.RemoteIP,
				CacheHit:                       _v.CacheHit,
				CacheValidatedWithOriginServer: _v.CacheValidatedWithOriginServer,
			}
			delete(payload, k)
		}
	}

	insertID, ok := payload[InsertIDKey].(string)
	if ok && insertID != "" {
		entry.InsertID = insertID
	}
	delete(payload, InsertIDKey)

	if ze.Caller.Defined {
		entry.SourceLocation = &logpb.LogEntrySourceLocation{
			File:     ze.Caller.File,
			Line:     int64(ze.Caller.Line),
			Function: runtime.FuncForPC(ze.Caller.PC).Name(),
		}
	}
	c.Logger.Log(entry)

	return nil
}

// Sync implements zapcore.Core. It flushes the Core's Logger instance.
func (c *Core) Sync() error {
	if err := c.Logger.Flush(); err != nil {
		return newError("flushing Google Cloud logger: %v", err)
	}
	return nil
}

// DefaultSeverityMapping is the default mapping of zap's Levels to Google's
// Severities.
var DefaultSeverityMapping = map[zapcore.Level]gcl.Severity{
	zapcore.DebugLevel:  gcl.Debug,
	zapcore.InfoLevel:   gcl.Info,
	zapcore.WarnLevel:   gcl.Warning,
	zapcore.ErrorLevel:  gcl.Error,
	zapcore.DPanicLevel: gcl.Critical,
	zapcore.PanicLevel:  gcl.Critical,
	zapcore.FatalLevel:  gcl.Critical,
}

// clone creates a new field map without mutating the original.
func clone(orig map[string]interface{}, newFields []zapcore.Field) map[string]interface{} {
	clone := make(map[string]interface{})

	for k, v := range orig {
		clone[k] = v
	}

	for _, f := range newFields {
		switch f.Type {
		// case zapcore.UnknownType:
		case zapcore.ArrayMarshalerType:
			clone[f.Key] = f.Interface
		case zapcore.ObjectMarshalerType:
			clone[f.Key] = f.Interface
		case zapcore.BinaryType:
			clone[f.Key] = f.Interface
		case zapcore.BoolType:
			clone[f.Key] = (f.Integer == 1)
		case zapcore.ByteStringType:
			clone[f.Key] = f.String
		case zapcore.Complex128Type:
			clone[f.Key] = fmt.Sprint(f.Interface)
		case zapcore.Complex64Type:
			clone[f.Key] = fmt.Sprint(f.Interface)
		case zapcore.DurationType:
			clone[f.Key] = time.Duration(f.Integer).String()
		case zapcore.Float64Type:
			clone[f.Key] = float64(f.Integer)
		case zapcore.Float32Type:
			clone[f.Key] = float32(f.Integer)
		case zapcore.Int64Type:
			clone[f.Key] = int64(f.Integer)
		case zapcore.Int32Type:
			clone[f.Key] = int32(f.Integer)
		case zapcore.Int16Type:
			clone[f.Key] = int16(f.Integer)
		case zapcore.Int8Type:
			clone[f.Key] = int8(f.Integer)
		case zapcore.StringType:
			clone[f.Key] = f.String
		case zapcore.TimeType:
			// Handle uber-go/zap#425
			if f.Interface == nil {
				clone[f.Key] = time.Unix(0, f.Integer)
			} else {
				clone[f.Key] = time.Unix(0, f.Integer).In(f.Interface.(*time.Location))
			}
		case zapcore.Uint64Type:
			clone[f.Key] = uint64(f.Integer)
		case zapcore.Uint32Type:
			clone[f.Key] = uint32(f.Integer)
		case zapcore.Uint16Type:
			clone[f.Key] = uint16(f.Integer)
		case zapcore.Uint8Type:
			clone[f.Key] = uint8(f.Integer)
		case zapcore.UintptrType:
			clone[f.Key] = uintptr(f.Integer)
		case zapcore.ReflectType:
			clone[f.Key] = f.Interface
		// case zapcore.NamespaceType:
		case zapcore.StringerType:
			clone[f.Key] = f.Interface.(fmt.Stringer).String()
		case zapcore.ErrorType:
			clone[f.Key] = f.Interface.(error).Error()
		case zapcore.SkipType:
			continue
		default:
			clone[f.Key] = f.Interface
		}
	}

	return clone
}

const packageName = "gcloudzap"

// newError calls fmt.Errorf() and prefixes the error with the packageName.
func newError(format string, args ...interface{}) error {
	return fmt.Errorf(packageName+": "+format, args)
}

// GoogleCloudLogger encapsulates the important methods of gcl.Logger
type GoogleCloudLogger interface {
	Flush() error
	Log(e gcl.Entry)
}
