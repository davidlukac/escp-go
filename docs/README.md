# Documentation

The API reference is on [pkg.go.dev](https://pkg.go.dev/github.com/davidlukac/escp-go); these pages are the guides that do not fit in a doc comment.

| Page | Read it to |
|---|---|
| [Getting started](getting-started.md) | install, connect, find switches, read settings, find a switch by its MAC address |
| [Writing safely](writing-safely.md) | understand what a write does, and use gates, impact classes and the dry run |
| [Errors](errors.md) | tell the failures apart with `errors.Is` and `errors.As` |
| [Logging](logging.md) | route the library's log lines into your own logging, and what they contain |
| [Testing against the fake switch](testing.md) | test code that uses the client without hardware (`escptest`) |
| [Architecture](architecture.md) | find your way in the code, and the naming conventions (contributors) |
| [PROTOCOL.md](../PROTOCOL.md) | the wire protocol and what was measured on hardware |
| [CONTRIBUTING.md](../CONTRIBUTING.md) | add or change a setting |
