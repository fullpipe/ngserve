package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/fullpipe/memfs/pkg/fscache"
	cache "github.com/victorspringer/http-cache"
	"github.com/victorspringer/http-cache/adapter/memory"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	appRoot := os.Getenv("APP_ROOT")
	if appRoot == "" {
		appRoot = "/"
	}

	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		webRoot = "./app"
	}

	noCache := getEnvAsBool("NO_CACHE", false)

	var handler http.Handler
	if noCache {
		fs := http.Dir(webRoot)
		handler = http.FileServer(fs)
	} else {
		fs, terminate := fscache.NewFSCache(http.Dir(webRoot))
		fs.SetTtl(60 * 60 * 24)
		defer terminate()

		handler = http.FileServer(fs)
		handler = httpCache(handler)
	}

	handler = gziphandler.GzipHandler(handler)
	handler = indexFile(handler)
	handler = onlyGetRequests(handler)
	if !noCache {
		handler = responceCache(handler)
	}
	handler = http.StripPrefix(appRoot, handler)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	http.Handle("/", handler)

	log.Fatal(srv.ListenAndServe().Error())
}

func onlyGetRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Method != "GET" {
			http.Error(w, "Method is not supported.", http.StatusNotFound)
			return
		}

		h.ServeHTTP(w, r)
	})
}

func indexFile(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) == "" {
			r.URL.Path = "/"
		}

		h.ServeHTTP(w, r)
	})
}

func responceCache(h http.Handler) http.Handler {
	memcached, err := memory.NewAdapter(
		memory.AdapterWithAlgorithm(memory.LRU),
		memory.AdapterWithCapacity(10000000),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	cacheClient, err := cache.NewClient(
		cache.ClientWithAdapter(memcached),
		cache.ClientWithTTL(10*time.Minute),
		cache.ClientWithRefreshKey("opn"),
	)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	return cacheClient.Middleware(h)
}
func httpCache(h http.Handler) http.Handler {
	etagSeed := strconv.FormatInt(time.Now().Unix(), 10)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		e := `"` + key + "-" + etagSeed + `"`
		w.Header().Set("Etag", e)
		w.Header().Set("Cache-Control", "max-age=2592000") // 30 days

		if match := r.Header.Get("If-None-Match"); match != "" {
			if strings.Contains(match, e) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		h.ServeHTTP(w, r)
	})
}

func getEnvAsBool(name string, defaultVal bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return defaultVal
	}

	val, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultVal
	}

	return val
}
