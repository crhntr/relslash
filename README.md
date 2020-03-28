# relslash
Proof of concept for updating multiple release branches of a tile

Need a new name (suggestions welcome). I thought since most backported branch names in the tile repos start with "rel/", that was the first thing that came to my name when creating the repo.

I am feeling the best place for most of this code is to be integrated into the https://github.com/pivotal-cf/kiln repo or the tile-checker.

## Notes

The product repo must be in good shape ("master" and "rel/*' branches clean).

Make sure the env script is run to get the correct `wasm_exec.js` file.

Also if you don't mind please replace the git name and email to your own.

## Development Setup

I ran the following in the demo on 2020/03/27.

```sh
GOOS=js GOARCH=wasm go build -o bin/bump-releases.wasm pages/bump-releases/*.go && \
          PORT=8080 go run cmd/bump-release-server/*.go
```
