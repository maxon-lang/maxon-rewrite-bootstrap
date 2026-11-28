// Command gobench is the Go baseline of bench/http's codec lanes: net/http's request parser (http.ReadRequest on a
// bufio.Reader over the request's bytes, the body read whole by io.ReadAll for the POST) and its response writer
// (http.Response.Write into a buffer), on the same requests and responses as bench/http/codec.maxon, in ns a request.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const getRequest = "GET /api/v1/items?id=42&sort=desc HTTP/1.1\r\nHost: example.com\r\nUser-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36\r\nAccept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\nAccept-Language: en-US,en;q=0.9\r\nAccept-Encoding: gzip, deflate, br\r\nConnection: keep-alive\r\nCache-Control: no-cache\r\nPragma: no-cache\r\nCookie: session=7f3c9a1e5b2d4c8f; theme=dark; lang=en\r\nReferer: https://example.com/catalog/items\r\nX-Request-Id: 2f1e0d3c-4b5a-6978-8796-a5b4c3d2e1f0\r\nUpgrade-Insecure-Requests: 1\r\n\r\n"

const bodyBytes = 65536

func filler(n int) string {
	var b strings.Builder
	for b.Len() < n {
		b.WriteString("0123456789abcdef")
	}
	return b.String()
}

func postRequest(body string) string {
	return "POST /api/v1/upload HTTP/1.1\r\nHost: example.com\r\nUser-Agent: bench/1.0\r\nContent-Type: application/octet-stream\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\n\r\n" + body
}

func parsed(raw []byte, withBody bool) int {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		panic(err)
	}
	found := len(req.Header)
	if withBody {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			panic(err)
		}
		found += len(body)
	}
	return found
}

func written(body string) int {
	resp := &http.Response{
		StatusCode:    200,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{},
		ContentLength: int64(len(body)),
		Close:         true,
		Body:          io.NopCloser(strings.NewReader(body)),
	}
	resp.Header.Set("Content-Type", "text/plain")
	resp.Header.Set("Cache-Control", "no-store")
	resp.Header.Set("Server", "bench")
	resp.Header.Set("X-Request-Id", "2f1e0d3c-4b5a-6978-8796-a5b4c3d2e1f0")
	var out bytes.Buffer
	if err := resp.Write(&out); err != nil {
		panic(err)
	}
	return out.Len()
}

func lane(name string, rounds int, started time.Time, sum int) {
	fmt.Printf("result %s %d ns  (sum %d)\n", name, time.Since(started).Nanoseconds()/int64(rounds), sum)
}

func main() {
	rounds := 100000
	if len(os.Args) > 1 {
		rounds, _ = strconv.Atoi(os.Args[1])
	}
	get := []byte(getRequest)
	large := filler(bodyBytes)
	post := []byte(postRequest(large))
	sum, started := 0, time.Now()
	for i := 0; i < rounds; i++ {
		sum += parsed(get, false)
	}
	lane("parse-get", rounds, started, sum)
	sum, started = 0, time.Now()
	for i := 0; i < rounds; i++ {
		sum += parsed(post, true)
	}
	lane("parse-post-64k", rounds, started, sum)
	sum, started = 0, time.Now()
	for i := 0; i < rounds; i++ {
		sum += written("hello world\n")
	}
	lane("write-small", rounds, started, sum)
	sum, started = 0, time.Now()
	for i := 0; i < rounds; i++ {
		sum += written(large)
	}
	lane("write-64k", rounds, started, sum)
}
