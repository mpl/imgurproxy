package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
)

const (
	idstring = "http://golang.org/pkg/http/#ListenAndServe"
	cacheSize = 2
	imgSizeLimit = 10 << 20
)

var (
	help  = flag.Bool("h", false, "show this help.")
	host  = flag.String("host", "localhost:8080", "listening port and hostname.")
	prefix = flag.String("prefix", "/", "URL prefix for which the server runs (as in http://foo:8080/prefix).")
)

func usage() {
	fmt.Fprintf(os.Stderr, "imgurproxy \n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	mu sync.RWMutex
	cache map[string][]byte = make(map[string][]byte)
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if *help {
		usage()
	}

	nargs := flag.NArg()
	if nargs > 0 {
		usage()
	}

	*prefix = path.Join("/" + *prefix)
	if *prefix != "/" {
		*prefix = *prefix + "/"
	}

	http.HandleFunc(*prefix+"gallery/", galleryHandler)
	http.HandleFunc(*prefix, imgHandler)
	if err := http.ListenAndServe(*host, nil); err != nil {
		log.Fatalf("Could not start http server: %v", err)
	}
}

const noImgHTML = `
<!DOCTYPE HTML>
<html>
	<head><title>imgurproxy</title></head>
	<body><h1>Append an imgur image or gallery URL path to the URL.</h1></body>
</html>
`

func trimCache() {
	mu.Lock()
	defer mu.Unlock()
	size := len(cache)
	// TODO(mpl): this is a bit dumb since we could very well remove the
	// one(s) that were cached the latest, but oh well.
	for k,_ := range cache {
		if size <= cacheSize {
			break
		}
		delete(cache, k)
		size--
	}
}

func imgHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Want GET", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, *prefix) {
		http.Error(w, "NOPE", http.StatusNotFound)
		return
	}
	imgName := strings.TrimPrefix(r.URL.Path, *prefix)
	if imgName == "" {
		w.Write([]byte(noImgHTML))
		return
	}
	mu.Lock()
	defer mu.Unlock()
	b, ok := cache[imgName]
	if ok {
		w.Write(b)
		return
	}
	resp, err := http.Get("https://i.imgur.com/" + imgName)
	if err != nil {
		http.Error(w, "error fetching image", http.StatusInternalServerError)
		log.Printf("error fetching image %v: %v", imgName, err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, imgSizeLimit))
	if err != nil {
		http.Error(w, "error fetching image", http.StatusInternalServerError)
		log.Printf("error fetching image %v: %v", imgName, err)
		return
	}
	if !strings.HasPrefix(http.DetectContentType(body), "image") {
		http.Error(w, "not an image", http.StatusNotFound)
		return
	}
	log.Printf("fetched %v\n", imgName)
	cache[imgName] = body
	w.Write(body)
	go func() {
		trimCache()
	}()
}

// TODO(mpl): add url parameter to control the number of images we slurp

func galleryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Want GET", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, *prefix) {
		http.Error(w, "NOPE", http.StatusNotFound)
		return
	}
	galleryName := strings.TrimPrefix(r.URL.Path, *prefix+"gallery")
	if galleryName == "" {
		w.Write([]byte(noImgHTML))
		return
	}

	resp, err := http.Get("https://imgur.com/gallery/" + galleryName)
	if err != nil {
		http.Error(w, "error fetching gallery", http.StatusInternalServerError)
		log.Printf("error fetching gallery %v: %v", galleryName, err)
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "error reading gallery", http.StatusInternalServerError)
		log.Printf("error reading gallery %v: %v", galleryName, err)
		return
	}
	log.Printf("fetched %v\n", galleryName)

	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		// TODO(mpl): get each i.imgur.com link, slurp the image (if not in cache)
		// serve a page with all the images.
	}
	if err := scanner.Err(); err != nil {
	}
}
