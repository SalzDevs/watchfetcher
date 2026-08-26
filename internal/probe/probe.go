package main

import (
	"fmt"
	"regexp"
	"strings"

	"watchfetcher/internal/httpclient"
)

func main() {
	client, _ := httpclient.New()
	_, body, _ := client.Get(nil,
		"https://www.watchfinder.co.uk/catalogsearch/result/?q=rolex+submariner")
	fmt.Println("bytes:", len(body))

	// my current regex
	myRe := regexp.MustCompile(
		`<a\s+href="(https://www\.watchfinder\.co\.uk/watches/[^"]+)"\s+class="product-card"(.*?)</a>`)
	fmt.Println("my regex matches:", len(myRe.FindAllStringSubmatch(body, -1)))

	// find the first product-card occurrence and print raw context
	i := strings.Index(body, `class="product-card"`)
	if i < 0 {
		fmt.Println("no product-card class found")
		return
	}
	start := i - 250
	if start < 0 {
		start = 0
	}
	fmt.Println("---- raw context around first product-card ----")
	fmt.Println(body[start:i+150])
}
