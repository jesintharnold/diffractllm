// Fence: makes this folder its own Go module so `go build ./...` and `go vet ./...`
// never walk into node_modules (some npm packages ship .go files).
module diffractllm-ui-web

go 1.25
