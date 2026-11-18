// The module of the lint fixtures (see README.md). It carries the library's own module path on purpose:
// the layering rules of .golangci.yml name packages of that path, so the fixtures can only exercise them
// under it. It is a module of its own, so `go test ./...`, `go vet ./...` and the lint of the library never see it.
module github.com/davidlukac/escp-go

go 1.26.2
