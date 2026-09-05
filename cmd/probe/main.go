// Command probe (dev-only): scan Bonhams auction IDs from the sale sitemap,
// report Watches-department sales. Rate-limited, polite.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var reNEXT = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
var reAuction = regexp.MustCompile(`/auctions/(\d+)/`)

func main() {
	f, err := os.Open("/tmp/bh-sales.xml")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	var ids []int
	seen := map[string]bool{}
	for scanner.Scan() {
		for _, m := range reAuction.FindAllStringSubmatch(scanner.Text(), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				n, _ := strconv.Atoi(m[1])
				ids = append(ids, n)
			}
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))
	fmt.Println("total auction urls:", len(ids))

	client := &http.Client{Timeout: 30 * time.Second}
	checked := 0
	for _, id := range ids {
		if id < 30000 {
			continue
		}
		url := fmt.Sprintf("https://www.bonhams.com/auctions/%d/", id)
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
		res, err := client.Do(req)
		if err != nil || res.StatusCode != 200 {
			if res != nil {
				res.Body.Close()
			}
			continue
		}
		var sb strings.Builder
		tmp := make([]byte, 65536)
		for {
			n, err := res.Body.Read(tmp)
			sb.Write(tmp[:n])
			if err != nil || sb.Len() > 2<<20 {
				break
			}
		}
		res.Body.Close()
		if m := reNEXT.FindStringSubmatch(sb.String()); m != nil {
			var tree any
			if json.Unmarshal([]byte(m[1]), &tree) == nil {
				watchSales(id, tree)
			}
		}
		checked++
		if checked%20 == 0 {
			fmt.Printf("  checked %d...\n", checked)
		}
		time.Sleep(700 * time.Millisecond)
	}
	fmt.Println("done, checked:", checked)
}

func watchSales(id int, n any) {
	switch v := n.(type) {
	case []any:
		for _, c := range v {
			watchSales(id, c)
		}
	case map[string]any:
		if dm, ok := v["department"].(map[string]any); ok {
			name, _ := dm["name"].(string)
			if strings.Contains(strings.ToLower(name), "watch") {
				status, _ := v["auctionStatus"].(string)
				title, _ := v["name"].(string)
				fmt.Printf("WATCH SALE: id=%d status=%s name=%q\n", id, status, title)
			}
		}
		for _, c := range v {
			watchSales(id, c)
		}
	}
}
