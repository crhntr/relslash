package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/crhntr/relslash"
	"gopkg.in/src-d/go-git.v4"

	"github.com/julienschmidt/httprouter"
)

const (
	EnvironmentVariableProductTileRepo = "BUMP_RELEASE_PRODUCT_TILE_REPO"
	EnvironmentVariableReleaseRepo     = "BUMP_RELEASE_RELEASE_REPO"
	//EnvironmentVariableCommitAuthorName  = "BUMP_RELEASE_COMMIT_AUTHOR_NAME"
	//EnvironmentVariableCommitAuthorEmail = "BUMP_RELEASE_COMMIT_AUTHOR_EMAIL"
)

func main() {
	productRepoPath := os.Getenv(EnvironmentVariableProductTileRepo)
	releaseRepoPath := os.Getenv(EnvironmentVariableReleaseRepo)
	//commitAuthorName := os.Getenv(EnvironmentVariableCommitAuthorName)
	//commitAuthorEmail := os.Getenv(EnvironmentVariableCommitAuthorEmail)

	tileRepo, err := git.PlainOpen(productRepoPath)
	if err != nil {
		log.Fatalf("could not open tile repo: %s", err)
	}

	boshReleaseRepo, err := git.PlainOpen(releaseRepoPath)
	if err != nil {
		log.Fatalf("could not open release repo: %s", err)
	}

	mux := httprouter.New()

	mux.Handler(http.MethodGet, "/api/v0/data", http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		data, err := relslash.NewBoshReleaseBumpSetData(tileRepo, boshReleaseRepo)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		mapping, err := data.MapTileBranchesToBoshReleaseVersions(tileRepo)
		if err != nil {
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}

		res.Header().Set("content-type", "application/json")
		res.WriteHeader(http.StatusOK)

		buf, err := json.Marshal(struct {
			relslash.BoshReleaseBumpSetData
			VersionMapping map[string][]relslash.Reference
		}{BoshReleaseBumpSetData: data, VersionMapping: mapping})
		if err != nil {
			log.Print(err)
			return
		}

		_, err = res.Write(buf)
		if err != nil {
			log.Print(err)
		}
	}))

	mux.HandlerFunc(http.MethodGet, "/", PRBoshReleaseVersionBumps)
	mux.ServeFiles("/assets/*filepath", http.Dir("assets"))

	mux.Handler(http.MethodGet, "/bin/bump-releases.wasm", http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("content-type", "application/wasm")
		http.ServeFile(res, req, "bin/bump-releases.wasm")
	}))

	log.Fatal(http.ListenAndServe(":"+os.Getenv("PORT"), rootHandler(mux)))
}

func PRBoshReleaseVersionBumps(res http.ResponseWriter, req *http.Request) {
	header := res.Header()
	header.Set("content-type", "text/html")
	http.ServeFile(res, req, "pages/bump-releases/index.html")
}

func rootHandler(handler http.Handler) http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/assets") {
			res.Header().Set("content-type", "text/plain")
		}

		res.Header().Set("cache-control", "no-cache")

		record := &logRecord{
			ResponseWriter: res,
		}

		handler.ServeHTTP(record, req)

		log.Printf("NET_HTTP\t%s\t%d (%s)\t%s", req.Method, record.status, http.StatusText(record.status), req.URL)
	}
}

type logRecord struct {
	http.ResponseWriter
	status int
}

func (r *logRecord) Write(p []byte) (int, error) {
	return r.ResponseWriter.Write(p)
}

func (r *logRecord) Flush() {
	r.ResponseWriter.(http.Flusher).Flush()
}

// WriteHeader implements ResponseWriter for logRecord
func (r *logRecord) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
