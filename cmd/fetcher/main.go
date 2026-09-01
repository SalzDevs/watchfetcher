// WatchFetcher: crawls the configured watch models across four marketplaces
// and writes canonical NDJSON listings. One run = one nightly snapshot.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"watchfetcher/internal/crawl"
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
	maxPerModel := flag.Int("max-per-model", 80, "cap per (source, model)")
	outPath := flag.String("out", "", "NDJSON output path (default: stdout)")
	metricsPath := flag.String("metrics", "data/crawl_metrics.json", "path to write crawl metrics JSON (for Fly monitoring)")
	concurrency := flag.Int("concurrency", 3, "max concurrent jobs (polite)")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

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
	// Shuffle jobs to avoid hitting same brand/model patterns every night.
	rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })

	// Metrics + circuit breaker (Fly monitoring for Chrono24 bans)
	metrics := crawl.NewMetrics()
	crawl.SetGlobal(metrics)

	started := time.Now()
	var (
		mu       sync.Mutex
		sem      = make(chan struct{}, *concurrency)
		wg       sync.WaitGroup
		okCount  int
		errCount int
	)

	for _, j := range jobs {
		// Check circuit breaker before launching — skip if source appears banned
		if metrics.CircuitOpen(j.source.ID()) {
			// Still need to count as job but skip network
			metrics.RecordJobStart(j.source.ID())
			metrics.RecordJobErr(j.source.ID(), "circuit open (skipped)", true)
			mu.Lock()
			errCount++
			fmt.Fprintf(os.Stderr, "SKIP %-16s %-30s circuit open (banned)\n", j.source.ID(), j.brand+" "+j.model)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Per-job timeout — hung connections don't block nightly run
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()

			metrics.RecordJobStart(j.source.ID())
			listings, err := j.source.Fetch(ctx, client, j.brand, j.model, *maxPerModel)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				banned := isBannedErr(err)
				metrics.RecordJobErr(j.source.ID(), err.Error(), banned)
				errCount++
				// Mark 403/429 explicitly for monitoring
				prefix := "ERR "
				if banned {
					prefix = "BAN "
				}
				fmt.Fprintf(os.Stderr, "%s %-16s %-30s %s\n", prefix, j.source.ID(), j.brand+" "+j.model, err)
				return
			}
			metrics.RecordJobOK(j.source.ID(), len(listings))
			okCount++
			for _, l := range listings {
				if err := enc.Encode(l); err != nil {
					return
				}
			}
			fmt.Fprintf(os.Stderr, "OK   %-16s %-30s %d listings\n", j.source.ID(), j.brand+" "+j.model, len(listings))
		}(j)
		// Polite staggering with jitter — prevents synchronized bursts to same source
		time.Sleep(crawl.Jitter(400 * time.Millisecond))
	}
	wg.Wait()

	metrics.Finish()
	fmt.Fprintf(os.Stderr, "\ndone: %d jobs (%d ok, %d err) in %.0fs\n",
		len(jobs), okCount, errCount, time.Since(started).Seconds())
	fmt.Fprint(os.Stderr, metrics.Summary())
	if alert := metrics.AlertString(); alert != "" {
		fmt.Fprint(os.Stderr, "\n"+alert)
	}
	// Persist metrics for Fly monitoring / dashboards (even on partial failure)
	if *metricsPath != "" {
		if dir := filepath.Dir(*metricsPath); dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		if err := metrics.WriteJSON(*metricsPath); err != nil {
			fmt.Fprintf(os.Stderr, "warn: write metrics %s: %v\n", *metricsPath, err)
		} else {
			fmt.Fprintf(os.Stderr, "metrics written to %s\n", *metricsPath)
		}
	}
	_ = model.Listing{}
}

func isBannedErr(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "403") || strings.Contains(s, "429") || strings.Contains(s, "banned") || strings.Contains(s, "rate-limit") || strings.Contains(s, "rate limit") || strings.Contains(s, "waf") || strings.Contains(s, "forbidden")
}
