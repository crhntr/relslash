//+build js,wasm

package main

import (
	"bytes"
	"fmt"
	"os"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"html/template"
	"sort"
	"syscall/js"
	"net/http"
	"encoding/json"
	"time"

	"github.com/crhntr/relslash"
)

func main() {
	document := js.Global().Get("document")
	body := document.Get("body")

	statusIndicator := make(chan string)
	go statusText(body, statusIndicator)

	fatal := func(err error) {
		statusIndicator <- "ERROR " + err.Error()
		time.Sleep(time.Second)
		os.Exit(1)
	}

	statusIndicator <- "Requesting data"

	var data struct {
		relslash.BoshReleaseBumpSetData
		VersionMapping map[string][]relslash.Reference
	}

	res, err := http.Get("/api/v0/data")
	if err != nil {
		fatal(err)
	}

	if res.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("non successful status code: %d", res.StatusCode))
	}

	statusIndicator <- "Parsing data"
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		fatal(err)
	}

	sort.Sort(relslash.VersionsDecreasing(data.BoshReleaseVersions))

	statusIndicator <- "Rendering Data"

	t := template.New("page-template")
	t = t.Funcs(template.FuncMap{
		"ShortBranchName": func(ref relslash.Reference) string {
			return (*plumbing.Reference)(&ref).Name().Short()
		},
		"CurrentBOSHReleaseVersion": func(ref relslash.Reference) string {
			for v, branches := range data.VersionMapping {
				for _, b := range branches {
					if (*plumbing.Reference)(&b).Strings() == (*plumbing.Reference)(&ref).Strings() {
						ver, err := semver.NewVersion(v)
						if err != nil {
							fmt.Println("could not render version for branch: %s", err)
						}
						return ver.String()
					}
				}
			}
			return ""
		},
	})
	pageTemplate := template.Must(t.Parse(document.Call("getElementById", "page-template").Get("innerText").String()))

	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, data); err != nil {
		fatal(err)
	}

	close(statusIndicator)

	body.Set("innerHTML", buf.String())

	select {}
}

func statusText(el js.Value, status chan string) {
	msg := el.Get("innerTEXT").String()

	ticker := time.NewTicker(time.Second)

	dots := "..."

loop:
	for i := 0; ; i++ {
		select {
		case <-ticker.C:
			el.Set("innerHTML", msg+dots[:i%(len(dots)+1)])
		case str, open := <-status:
			if !open {
				ticker.Stop()
				break loop
			}
			dots = "..."
			msg = str
		}
	}
}
