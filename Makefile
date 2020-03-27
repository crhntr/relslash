build:
    GOOS=js GOARCH=wasm go build -o bin/bump-releases.wasm pages/bump-releases/*.go
