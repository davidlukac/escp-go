// The module of the dead-code fixtures (see README.md). It is a module of its own, so `go test ./...`,
// `go vet ./...` and the lint of the library never see it.
module example.com/deadcodefixtures

go 1.26.2
