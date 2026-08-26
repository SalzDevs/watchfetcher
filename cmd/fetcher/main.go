// WatchFetcher: crawls the configured watch models across four marketplaces
// and writes canonical NDJSON listings. One run = one nightly snapshot.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"watchfetcher/internal/httpclient"
	"watchfetcher/internal/model"
	"watchfetcher/internal/sources"
)

type config struct {
	GeneratedAt int64               `json:"generated_at"`
	Brands      map[string][]string `json:"brands"`
}

func loadConfig(path string) (config, error) {
	var cfg config
	f, err := os.Open(path)
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&cfg)
	return cfg, err
}

func sourceList(enabled string) []sources.Source {
	all := []sources.Source{
		&sources.Chrono24Source{},
		&sources.BobswatchesSource{MaxPages: 3},
		&sources.The1916CompanySource{MaxPages: 2},
		&sources.WatchfinderSource{MaxPages: 3},
	}
	var out []sources.Source
	for _, s := range all {
		for _, id := range strings.Split(enabled, ",") {
			if strings.TrimSpace(id) == s.ID() {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

func main() {
	configPath := flag.String("config", "config/models.json", "path to models.json")
	sourcesFlag := flag.String("sources", "chrono24,bobswatches,the1916company,watchfinder", "comma-separated source ids")
	maxPerModel := flag.Int("max-per-model", 240, "cap per (source, model)")
	outPath := flag.String("out", "", "NDJSON output path (default: stdout)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	chosen := sourceList(*sourcesFlag)
	if len(chosen) == 0 {
		fmt.Fprintln(os.Stderr, "no sources enabled")
		os.Exit(1)
	}

	client, err := httpclient.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "http client:", err)
		os.Exit(1)
	}

	out := os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "out:", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}
	enc := json.NewEncoder(out)

	type job struct {
		source sources.Source
		brand  string
		model  string
	}
	var jobs []job
	for _, src := range chosen {
		for brand, models := range cfg.Brands {
			for _, model := range models {
				jobs = append(jobs, job{src, brand, model})
			}
		}
	}

	started := time.Now()
	var (
		mu      sync.Mutex
		sem     = make(chan struct{}, 4) // polite concurrency
		wg      sync.WaitGroup
		okCount int
		errCount int
	)

	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			listings, err := j.source.Fetch(context.Background(), client, j.brand, j.model, *maxPerModel)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errCount++
				fmt.Fprintf(os.Stderr, "ERR  %-16s %-30s %s\n", j.source.ID(), j.brand+" "+j.model, err)
				return
			}
			okCount++
			for _, l := range listings {
				if err := enc.Encode(l); err != nil {
					return
				}
			}
			fmt.Fprintf(os.Stderr, "OK   %-16s %-30s %d listings\n", j.source.ID(), j.brand+" "+j.model, len(listings))
		}(j)
		// polite pacing between job launches
		time.Sleep(120 * time.Millisecond)
	}
	wg.Wait()

	fmt.Fprintf(os.Stderr, "\ndone: %d jobs (%d ok, %d err) in %.0fs\n",
		len(jobs), okCount, errCount, time.Since(started).Seconds())
	_ = model.Listing{}
}
