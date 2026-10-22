// Command probe is a tiny HTTP health check for containers without curl.
// It prints the response body and exits 0 when GET <url> answers 2xx.
package main

import (
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		os.Stderr.WriteString("usage: probe URL\n")
		os.Exit(2)
	}
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get(os.Args[1])
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	defer resp.Body.Close()
	io.Copy(os.Stdout, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		os.Exit(1)
	}
}
