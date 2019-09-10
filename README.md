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

### Option 4: middleware integrate with Gin

```go
package main

import (
    "fmt"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/pigfoot/zapgcl"
)

func main() {
    r := gin.New()

    logger, _ := zapdrive.NewProduction("projects/project_id", "LogID")

    // Add a ginzap middleware, which:
    //   - Logs all requests, like a combined access and error log.
    //   - Logs to stdout.
    //   - RFC3339 with UTC time format.
    r.Use(zapgcl.ZipGin(logger, time.RFC3339, true))

    // Logs all panic to error log
    //   - stack means whether output the stack info.
    r.Use(zapgcl.RecoveryWithZap(logger, true))

    // Example ping request.
    r.GET("/ping", func(c *gin.Context) {
        c.String(200, "pong "+fmt.Sprint(time.Now().Unix()))
    })

    // Example when panic happen.
    r.GET("/panic", func(c *gin.Context) {
        panic("An unexpected error happen!")
    })

    // Listen and Server in 0.0.0.0:8080
    r.Run(":8080")
}
```
