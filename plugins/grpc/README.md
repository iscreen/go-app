# gRPC

Provide gRPC server with gracefully shutdown and common interceptors.

## Usage

There is a preset gRPC server you can use direct

```golang
package main

import (
  go_app "github.com/shoplineapp/go-app"
  "github.com/shoplineapp/go-app/plugins/grpc/presets"
  "github.com/shoplineapp/go-app/plugins/logger"
)

func main() {
  app := go_app.NewApplication()

  // Use DefaultGrpcServerWithSentry with presets
  app.Run(func(
    logger *logger.Logger,
    grpc *presets.DefaultGrpcServerWithSentry,
  ) {
    logger.Info("Hello world")
  })
}
```

Or you prefer to do that manually with your own implementation

```golang
package main

import (
  "context"
  go_app "github.com/shoplineapp/go-app"
  grpc_plugin "github.com/shoplineapp/go-app/plugins/grpc"
  "github.com/shoplineapp/go-app/plugins"
  "github.com/shoplineapp/go-app/plugins/env"
  "github.com/shoplineapp/go-app/plugins/grpc/healthcheck"
  "github.com/shoplineapp/go-app/plugins/grpc/interceptors"
  "github.com/shoplineapp/go-app/plugins/logger"
  "google.golang.org/grpc"
  "go.uber.org/fx"
)

func main() {
  app := go_app.NewApplication()

  // Inject dependencies we need
  app.Run(func(
    lc fx.Lifecycle,
    logger *logger.Logger,
    env *env.Env,
    grpcServer *grpc_plugin.GrpcServer,
    deadline *interceptors.DeadlineInterceptor,
    requestLog *interceptors.RequestLogInterceptor,
    recovery *interceptors.RecoveryInterceptor,
    healthcheckServer *healthcheck.HealthCheckServer,
  ) {
    // Setup gRPC server
    s := *grpcServer
    plugin := &DefaultGrpcServerWithSentry{
      GrpcServer: s,
    }
    // Configure with interceptors
    plugin.Configure(
      grpc.ChainUnaryInterceptor(
        requestLog.Handler(),
        deadline.Handler(),
        recovery.Handler(),
      ),
    )
    // Register servers
    healthcheck.RegisterHealthServer(plugin.Server(), healthcheckServer)

    // Use Uber fx lifecycle and trigger gracefully shutdown
    lc.Append(fx.Hook{
      OnStart: func(ctx context.Context) error {
        plugin.Serve()
        return nil
      },
      OnStop: func(ctx context.Context) error {
        plugin.Shutdown()
        return nil
      },
    })
  })
}
```

---

## Response log redaction

Responses are logged unchanged by default. Configure a request log interceptor's
allowlist before serving requests to redact selected response types:

```go
// pb is your application's generated protobuf package.
r := &common.Redactor{}
r.AddFilters(common.DefaultFilters...)
r.AddFilters(common.NewFilter(
    &pb.GetMerchantResponse{},
    "merchant_rollout_keys",
    common.FullRedact,
))
interceptors.SetRedactor(r)

requestLog.SetResponseRedactAllowlist((*pb.GetMerchantResponse)(nil))
```

Import `common` from `github.com/shoplineapp/go-app/common` and `interceptors`
from `github.com/shoplineapp/go-app/plugins/grpc/interceptors`.

The allowlist belongs to each interceptor instance and matches exact Go types:
`*Response` and `Response` are different entries. Pass multiple examples to allow
multiple types. Each call replaces the list; calling with no arguments clears it.
Typed nil pointers are supported; untyped nil entries are ignored.

Only allowlisted response log values use the existing default/custom filters.
Other response types skip recursive redaction. The response returned to the
caller is unchanged. Request redaction remains active regardless of this list.
The redactor configured by `SetRedactor` is shared by request and response logging.
Configure both before serving requests; concurrent configuration updates are not
supported.

## Environment variable

Supporting environment variable configurations

| Key | Type | Description |
| --------- | --- | ---- |
| `GRPC_SERVER_PORT` | string | Control the port that gRPC server listen to, default: `3000` |


