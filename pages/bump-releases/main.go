//+build js,wasm

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Masterminds/semver"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"html/template"
	"net/http"
	"os"
	"sort"
	"syscall/js"
	"time"

	"github.com/crhntr/relslash"
)

func main() {
	document := js.Global().Get("document")
	body := document.Get("body")

	statusIndicator := make(chan string)
	go statusText(body, "Loading Data From Repos", statusIndicator)

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

		if res.StatusCode != http.StatusOK {
			fatal(fmt.Errorf("non successful status code: %d", res.StatusCode))
		}

		statusIndicator <- "Parsing data"
		if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
			fatal(err)
		}

		break
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
				ticker.Stop()
				break loop
			}
			dots = "..."
			msg = str
		}
	}
}