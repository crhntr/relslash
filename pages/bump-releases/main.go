//+build js,wasm

package main

import (
	"syscall/js"
	"time"
)

func main() {
	document := js.Global().Get("document")

	dots := "..."

	for i := 0; ; i++{
		document.Get("body").Set("innerHTML", "Loading"+dots[:i%(len(dots)+1)])
		time.Sleep(time.Second)
	}
}
