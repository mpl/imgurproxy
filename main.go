package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
)

const (
	idstring     = "http://golang.org/pkg/http/#ListenAndServe"
	cacheSize    = 2
	imgSizeLimit = 10 << 20
)

var (
	help   = flag.Bool("h", false, "show this help.")
	host   = flag.String("host", "localhost:8080", "listening port and hostname.")
	prefix = flag.String("prefix", "/", "URL prefix for which the server runs (as in http://foo:8080/prefix).")
)

func usage() {
	fmt.Fprintf(os.Stderr, "imgurproxy \n")
	flag.PrintDefaults()
	os.Exit(2)
}

var (
	mu              sync.RWMutex
	cache           map[string][]byte = make(map[string][]byte)
	galleryTemplate *template.Template
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

	galleryTemplate = template.Must(template.New("gallery").Parse(galleryHTML))

	*prefix = path.Join("/" + *prefix)
	if *prefix != "/" {
		*prefix = *prefix + "/"
	}

	// TODO(mpl): allow full imgur URL, or post form or something.
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
	for k, _ := range cache {
		if size <= cacheSize {
			break
		}
		delete(cache, k)
		size--
	}
}

func fetchImage(imgName string) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	b, ok := cache[imgName]
	if ok {
		return b, nil
	}
	resp, err := http.Get("https://i.imgur.com/" + imgName)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, imgSizeLimit))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(http.DetectContentType(body), "image") {
		return nil, errors.New("not an image")
	}
	log.Printf("fetched %v\n", imgName)
	cache[imgName] = body
	return body, nil
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
	body, err := fetchImage(imgName)
	if err != nil {
		http.Error(w, "error fetching image", http.StatusInternalServerError)
		log.Printf("error fetching image %v: %v", imgName, err)
		return
	}
	w.Write(body)
	go func() {
		trimCache()
	}()
}

// TODO(mpl): add url parameter to control the number of images we slurp

var (
	imgPattern = "//i.imgur.com/"
	// TODO(mpl): class="zoom" hint is super lame, but it helps only getting the actual images from the gallery. Actually does not work, since some of them have no zoom.
	//	imgRxp = regexp.MustCompile(`.*`+imgPattern+`(.*?)" class="zoom".*`)
	// TODO(mpl): will imgur always use jpg ?
	imgRxp = regexp.MustCompile(`.*` + imgPattern + `(.*?\.jpg)".*`)
)

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
	log.Printf("fetched gallery %v\n", galleryName)

	sc := bufio.NewScanner(bytes.NewReader(body))
	// TODO(mpl): map gives us dedup, but we want ordered, so back to slice. do better.
	//	var images []string
	images := make(map[string]struct{})
	for sc.Scan() {
		l := sc.Text()
		if !strings.Contains(l, imgPattern) {
			continue
		}
		m := imgRxp.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		imgName := m[1]
		/*
			body, err := fetchImage(imgName)
			if err != nil {
				http.Error(w, "error fetching image", http.StatusInternalServerError)
				log.Printf("error fetching image %v: %v", imgName, err)
				return
			}
		*/
		//		images = append(images, imgName)
		images[imgName] = struct{}{}
	}
	if err := sc.Err(); err != nil {
		http.Error(w, "error parsing gallery", http.StatusInternalServerError)
		log.Printf("error parsing gallery %v: %v", galleryName, err)
		return
	}
	d := struct {
		//		Host string
		Prefix string
		//		Images []string
		Images map[string]struct{}
	}{
		//		Host: *host,
		Prefix: *prefix,
		Images: images,
	}
	if err := galleryTemplate.Execute(w, &d); err != nil {
		http.Error(w, "error serving template", http.StatusInternalServerError)
		log.Printf("error serving template: %v", err)
		return
	}
	go func() {
		trimCache()
	}()
}

var galleryHTML = `
<!DOCTYPE html PUBLIC "-//W3C//DTD HTML 4.01//EN"
   "http://www.w3.org/TR/html4/strict.dtd">

<html lang="en">
<head>
	<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
	<title>imgurproxy</title>
</head>
<body>
{{ $prefix := .Prefix }}
{{ if .Images }}
	{{ range $imgName, $_ := .Images }}
		<img src="{{$prefix}}{{$imgName}}" alt="{{$imgName}}" height="800" width="600">
	{{ end }}
{{ end }}
</body>
</html lang="en">
`

//	{{ range $_, $imgName := .Images }}
