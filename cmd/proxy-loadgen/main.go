// Command proxy-loadgen sends random requests to the proxy at a steady rate.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type configFile struct {
	Addr   string            `json:"addr"`
	Routes map[string]string `json:"routes"`
}

type requestJob struct {
	index    int
	when     time.Time
	method   string
	host     string
	path     string
	rawQuery string
}

type result struct {
	statusCode int
	latency    time.Duration
	err        error
}

func main() {
	var (
		baseURLStr  = flag.String("url", "", "proxy URL (for example http://127.0.0.1:8020); defaults to config.json addr")
		configPath  = flag.String("config", "config.json", "path to proxy-penguin config file")
		hostsFlag   = flag.String("hosts", "", "comma-separated Host headers; defaults to route hosts from config")
		pathsFlag   = flag.String("paths", "/,/health,/api/status", "comma-separated request paths")
		methodsFlag = flag.String("methods", "GET", "comma-separated HTTP methods")
		total       = flag.Int("n", 100, "total number of requests to send")
		duration    = flag.Duration("duration", 30*time.Second, "total run time")
		workers     = flag.Int("parallel", 10, "number of parallel workers")
		timeout     = flag.Duration("timeout", 5*time.Second, "HTTP client timeout per request")
		seed        = flag.Int64("seed", time.Now().UnixNano(), "random seed")
	)

	flag.Parse()

	if *total <= 0 {
		exitf("-n must be > 0")
	}
	if *duration <= 0 {
		exitf("-duration must be > 0")
	}
	if *workers <= 0 {
		exitf("-parallel must be > 0")
	}

	cfg, _ := loadConfig(*configPath)

	baseURL, err := resolveBaseURL(*baseURLStr, cfg)
	if err != nil {
		exitf("invalid base URL: %v", err)
	}

	hosts := parseCSV(*hostsFlag)
	if len(hosts) == 0 {
		hosts = sortedKeys(cfg.Routes)
	}

	paths := parseCSV(*pathsFlag)
	if len(paths) == 0 {
		exitf("no paths provided")
	}

	methods := parseCSV(*methodsFlag)
	if len(methods) == 0 {
		exitf("no methods provided")
	}

	rng := rand.New(rand.NewSource(*seed))
	jobs := buildJobs(rng, *total, *duration, methods, hosts, paths)

	client := &http.Client{Timeout: *timeout}
	runStart := time.Now()
	results := run(client, baseURL, jobs, *workers)
	actualDuration := time.Since(runStart)

	printSummary(baseURL.String(), *seed, *duration, actualDuration, results)
}

func run(client *http.Client, baseURL *url.URL, jobs []requestJob, workers int) []result {
	jobsCh := make(chan requestJob)
	resultsCh := make(chan result, len(jobs))

	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for job := range jobsCh {
				wait := time.Until(job.when)
				if wait > 0 {
					time.Sleep(wait)
				}
				resultsCh <- executeJob(client, baseURL, job)
			}
		})
	}

	for _, job := range jobs {
		jobsCh <- job
	}
	close(jobsCh)

	wg.Wait()
	close(resultsCh)

	results := make([]result, 0, len(jobs))
	for r := range resultsCh {
		results = append(results, r)
	}
	return results
}

func executeJob(client *http.Client, baseURL *url.URL, job requestJob) result {
	target := baseURL.ResolveReference(&url.URL{Path: job.path, RawQuery: job.rawQuery})
	req, err := http.NewRequest(job.method, target.String(), nil)
	if err != nil {
		return result{err: fmt.Errorf("create request: %w", err)}
	}
	if job.host != "" {
		req.Host = job.host
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return result{err: err, latency: time.Since(start)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	return result{statusCode: resp.StatusCode, latency: time.Since(start)}
}

func buildJobs(rng *rand.Rand, total int, duration time.Duration, methods, hosts, paths []string) []requestJob {
	start := time.Now()
	interval := requestInterval(total, duration)

	jobs := make([]requestJob, 0, total)
	for i := range total {
		path := normalizePath(pick(rng, paths))
		host := ""
		if len(hosts) > 0 {
			host = pick(rng, hosts)
		}

		jobs = append(jobs, requestJob{
			index:    i + 1,
			when:     start.Add(time.Duration(i) * interval),
			method:   pick(rng, methods),
			host:     host,
			path:     path,
			rawQuery: fmt.Sprintf("rid=%d&r=%08x", i+1, rng.Uint32()),
		})
	}

	return jobs
}

func requestInterval(total int, duration time.Duration) time.Duration {
	if total <= 1 {
		return 0
	}
	return duration / time.Duration(total-1)
}

func printSummary(target string, seed int64, requestedDuration time.Duration, actualDuration time.Duration, results []result) {
	var (
		success    atomic.Int64
		failed     atomic.Int64
		totalNanos atomic.Int64
		minNanos   atomic.Int64
		maxNanos   atomic.Int64
	)

	statusBuckets := map[string]int{}
	minNanos.Store(int64(time.Hour))

	for _, r := range results {
		if r.err != nil {
			failed.Add(1)
			statusBuckets["error"]++
			continue
		}

		success.Add(1)
		totalNanos.Add(r.latency.Nanoseconds())

		if r.latency.Nanoseconds() < minNanos.Load() {
			minNanos.Store(r.latency.Nanoseconds())
		}
		if r.latency.Nanoseconds() > maxNanos.Load() {
			maxNanos.Store(r.latency.Nanoseconds())
		}

		bucket := fmt.Sprintf("%dxx", r.statusCode/100)
		statusBuckets[bucket]++
	}
	avg := time.Duration(0)
	if success.Load() > 0 {
		avg = time.Duration(totalNanos.Load() / success.Load())
	}
	if minNanos.Load() == int64(time.Hour) {
		minNanos.Store(0)
	}

	keys := make([]string, 0, len(statusBuckets))
	for k := range statusBuckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	rps := 0.0
	if actualDuration > 0 {
		rps = float64(len(results)) / actualDuration.Seconds()
	}

	fmt.Printf("target: %s\n", target)
	fmt.Printf("seed: %d\n", seed)
	fmt.Printf("requests: %d\n", len(results))
	fmt.Printf("requested duration: %v\n", requestedDuration)
	fmt.Printf("actual duration: %v\n", actualDuration)
	fmt.Printf("throughput: %.2f req/s\n", rps)
	fmt.Printf("success: %d\n", success.Load())
	fmt.Printf("failed: %d\n", failed.Load())
	fmt.Printf("latency min/avg/max: %v / %v / %v\n", time.Duration(minNanos.Load()), avg, time.Duration(maxNanos.Load()))
	fmt.Println("status buckets:")
	for _, k := range keys {
		fmt.Printf("  %s: %d\n", k, statusBuckets[k])
	}
}

func resolveBaseURL(flagValue string, cfg configFile) (*url.URL, error) {
	if strings.TrimSpace(flagValue) != "" {
		return url.Parse(flagValue)
	}

	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = ":8020"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "http://127.0.0.1" + addr
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}

	return url.Parse(addr)
}

func loadConfig(path string) (configFile, error) {
	var cfg configFile
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func parseCSV(v string) []string {
	raw := strings.Split(v, ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		s := strings.TrimSpace(part)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return p
	}
	return "/" + p
}

func pick(rng *rand.Rand, values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[rng.Intn(len(values))]
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
