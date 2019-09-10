# ⚡ zapgcl [![GoDoc](https://godoc.org/github.com/pigfoot/zapgcl?status.svg)](https://godoc.org/github.com/pigfoot/zapgcl)

**(experimental)**

Formerly https://github.com/jonstaryuk/gcloudzap
Modified https://github.com/dhduvall/gcloudzap

This package provides a zap logger that forwards entries to the Google Stackdriver Logging service as structured payloads.

## Quickstart

Outside of Google Compute Engine, just add the environment variable `GOOGLE_APPLICATION_CREDENTIALS` with a path to a JSON credential file. For more info on this approach, see the [docs](https://developers.google.com/identity/protocols/application-default-credentials#howtheywork).

#### Option 1: Less configuration

```go
import "github.com/pigfoot/zapgcl"

log, err := zapgcl.NewDevelopment("your-project-id", "your-log-id")
if err != nil {
    panic(err)
}

log.Sugar().
    With("simple", true).
    With("foo", "bar").
    Info("This will get written to both stderr and Stackdriver Logging.")

// don't forget this log contain google logging handle
// if program terminates too early, then log may not send to stackdriver
log.Sync()
```

#### Option 2: More flexibility

```go
import (
    "go.uber.org/zap"
    "cloud.google.com/go/logging"
    "github.com/pigfoot/zapgcl"
)

// Configure the pieces
client, err := logging.NewClient(...)
defer client.Close()
cfg := zap.Config{...}

// Create a logger
log, err := zapgcl.New(cfg, client, "your-log-id", zap.Option(), ...)
```

#### Option 3: Most flexibility

```go
import (
    "go.uber.org/zap/zapcore"
    "cloud.google.com/go/logging"
    "github.com/pigfoot/zapgcl"
)

// Configure the pieces
client, err := logging.NewClient(...)
baseCore := zapcore.NewCore(...)

// Create a core
core := zapgcl.Tee(baseCore, client, "your-log-id")
```
