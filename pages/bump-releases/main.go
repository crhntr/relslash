//+build js,wasm

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"syscall/js"
	"time"

	"github.com/crhntr/relslash"
)

func main() {
	document := js.Global().Get("document")
	body := document.Get("body")

	statusIndicator := make(chan string)
	go statusText(body, "Loading Bump Request Data", statusIndicator)

	fatal := func(err error) {
		statusIndicator <- "Error fetching data: " + err.Error()
		time.Sleep(time.Second)
		os.Exit(1)
	}

	var data struct {
		relslash.BoshReleaseBumpSetData
		VersionMapping map[string][]relslash.Reference
	}
	for {
		res, err := http.Get("/api/v0/data")
		if err != nil {
			fatal(err)
		}

		if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
			fatal(err)
		}

		break
	}

	statusIndicator <- fmt.Sprintf("%#v", data)

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
