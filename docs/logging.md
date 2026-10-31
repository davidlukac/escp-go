# Logging

The library writes nowhere on its own. Pass a `*slog.Logger` in `Config.Logger` (a client) or `DiscoverOptions.Logger` (`Discover`, `Locate`) and it reports through that; nil discards everything. You own the format, destination and level, so the lines look like the rest of your logs:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
client, err := escp.Dial(escp.Config{ /* ... */ Logger: logger.With("request_id", id)})
```

`log/slog` is the standard library's structured logger and needs no dependency. A program on zap, zerolog or logr keeps its own pipeline through the `slog.Handler` bridges those projects provide (`zapslog`, `logr.FromSlogHandler` and their kin).

## Identification

Every line carries `component=escp`, which identifies the library the way a logger name does; filter on it to keep or drop the library's lines. A client also attaches `host` and `mac`, so several clients in one log can be told apart. If your logger already has a `component` attribute, both appear; rename yours with a handler's `ReplaceAttr`.

## Context fields

What you attach with `Logger.With` (a request id, a tenant) comes along on every line. Every message is logged with the context of the call (slog's `...Context` methods), so a handler can add what lives in the context, such as a trace id: the mapped diagnostic context of other logging frameworks. The library puts nothing into the context and defines no context keys.

## Levels

The levels are slog's four.

- **Debug** is the protocol: each request and reply with the operation, sequence number, TLV types and sizes, and each fragment.
- **Info** is the lifecycle: a session established, the switch located by MAC and where, a write with its operation and impact, a lease obtained.
- **Warn** is a recoverable anomaly: no reply and a retry, a session reopened, a final fragment missing, a download repeated.
- **Error** is not used: failures are returned, not logged.

## Contract

- **Attribute names are the contract**: lower case, snake_case, one meaning each (`host`, `mac`, `op`, `seq`, `attempt`, `attempts`, `tlvs`, `bytes`, `error_code`, `operation`, `impact`, `ports`, `round`). New names may be added in any release (a setting the hardware of a later model has, such as PoE, brings its own); an existing name is not renamed, repurposed or removed before a major version, and a level change of an existing message counts as a change. Message texts may change.
- **No secrets.** TLV *values* are never logged, only their types and sizes, so no message carries the password, the user name or the session key. `Config` is never logged as a whole. MAC and IP addresses are logged: they identify the switch for the person who owns the log.
- **No printing.** No `fmt.Print*` or `log.*` in library code; a lint rule enforces it.

The Terraform provider routes the logger to `tflog`, so `TF_LOG=DEBUG` shows what the client does.
