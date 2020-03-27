//+build js,wasm

package main

import (
	"syscall/js"
	"time"
)

func main() {
	document := js.Global().Get("document")
	body := document.Get("body")

	statusIndicator := make(chan string)
	go statusText(body, "Loading", statusIndicator)

	time.Sleep(time.Second * 8)

	statusIndicator <- "petting dog"

	time.Sleep(time.Second * 8)

	close(statusIndicator)

	select {}
}

func statusText(el js.Value, initial string, status chan string) {
	msg := initial

	ticker := time.NewTicker(time.Second)

	dots := "..."

loop:
	for i := 0; ; i++ {
		select {
		case <-ticker.C:
			el.Set("innerHTML", msg+dots[:i%(len(dots)+1)])
		case str, open := <-status:
			if !open {
				el.Set("innerHTML", "")
				ticker.Stop()
				break loop
			}
			dots = "..."
			msg = str
		}
	}
}
