package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/julienschmidt/httprouter"
)

func main() {
	mux := httprouter.New()

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

func unsetHeader(header string, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Del(header)
		handler.ServeHTTP(res, req)
	})
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
