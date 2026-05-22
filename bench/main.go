// Snooze MongoDB vs PostgreSQL benchmark harness.
//
// Drives a running snooze-server (against either backend) through a fixed
// production-like workload: seed 200 rules + 5000 records (deterministic PRNG),
// then run write-burst, read-only, and mixed phases, recording per-op latency
// histograms.
//
// Identical inputs across runs: every alert and rule is generated from the
// same fixed seed, so MongoDB and PostgreSQL are exercised on bit-for-bit
// equal data.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	seed             = 0xCAFE_BEEF
	numRules         = 200
	numInitialRecord = 5000
	numWriteBurst    = 5000
	writeConcurrency = 20
	numReadsPerKind  = 200
	readConcurrency  = 10
	mixedWrites      = 3000
	mixedReads       = 1000
	mixedDuration    = 30 * time.Second
)

var hostsPool = []string{
	"srv-web-01", "srv-web-02", "srv-web-03", "srv-web-04",
	"srv-db-01", "srv-db-02", "srv-db-03",
	"srv-app-01", "srv-app-02", "srv-app-03", "srv-app-04", "srv-app-05",
	"srv-cache-01", "srv-cache-02",
	"srv-queue-01", "srv-queue-02",
	"srv-edge-01", "srv-edge-02", "srv-edge-03",
	"srv-lb-01",
}

var severities = []string{"info", "warning", "error", "critical", "ok"}
var sources = []string{"syslog", "snmptrap", "prometheus", "alertmanager", "grafana", "smtp"}
var environments = []string{"prod", "staging", "dev", "qa"}
var processes = []string{"nginx", "postgres", "mongodb", "redis", "kafka", "python", "java", "node", "go"}

type alert struct {
	Host        string `json:"host"`
	Source      string `json:"source"`
	Process     string `json:"process"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Environment string `json:"environment"`
	Timestamp   string `json:"timestamp"`
}

// genAlert produces a deterministic alert from rng. Distribution roughly
// matches production: more warnings than criticals, more web than db, etc.
func genAlert(rng *rand.Rand) alert {
	sev := severities[rng.Intn(len(severities))]
	host := hostsPool[rng.Intn(len(hostsPool))]
	src := sources[rng.Intn(len(sources))]
	env := environments[rng.Intn(len(environments))]
	proc := processes[rng.Intn(len(processes))]
	msgs := []string{
		"connection refused", "disk usage high", "memory pressure",
		"replication lag", "slow query detected", "timeout on upstream",
		"healthcheck failed", "queue backlog", "auth failure", "tls handshake",
	}
	return alert{
		Host:        host,
		Source:      src,
		Process:     proc,
		Severity:    sev,
		Message:     fmt.Sprintf("%s on %s/%s", msgs[rng.Intn(len(msgs))], proc, host),
		Environment: env,
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// genRule yields a rule doc. Half match on severity (broad), half on host
// (narrow). Each appends a tag via SET. The rule plugin also auto-appends
// the rule name to view["rules"] when it matches.
func genRule(rng *rand.Rand, i int) map[string]any {
	name := fmt.Sprintf("bench-rule-%03d", i)
	var cond map[string]any
	if i%2 == 0 {
		// Broad: match on severity (each severity gets ~20 rules).
		sev := severities[i%len(severities)]
		cond = map[string]any{"op": "=", "field": "severity", "value": sev}
	} else {
		// Narrow: match on host.
		host := hostsPool[i%len(hostsPool)]
		cond = map[string]any{"op": "=", "field": "host", "value": host}
	}
	mods := []any{
		[]any{"SET", fmt.Sprintf("bench_tag_%d", i), "matched"},
	}
	return map[string]any{
		"name":          name,
		"enabled":       true,
		"tree_order":    i,
		"condition":     cond,
		"modifications": mods,
	}
}

type client struct {
	base  string
	token string
	hc    *http.Client
}

func newClient(base string) *client {
	return &client{
		base: base,
		hc: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 200,
				MaxConnsPerHost:     200,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *client) login() error {
	req, _ := http.NewRequest("POST", c.base+"/api/v1/login/anonymous", nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("login: %d %s", resp.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return err
	}
	if out.Token == "" {
		return fmt.Errorf("login: empty token in %s", body)
	}
	c.token = out.Token
	return nil
}

func (c *client) do(method, path string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func (c *client) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := c.hc.Get(c.base + "/readyz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("server not ready at %s", c.base)
}

func (c *client) seedRules() error {
	rng := rand.New(rand.NewSource(seed))
	batch := make([]map[string]any, 0, numRules)
	for i := 0; i < numRules; i++ {
		batch = append(batch, genRule(rng, i))
	}
	code, body, err := c.do("POST", "/api/v1/rule", batch)
	if err != nil {
		return err
	}
	if !ok2xx(code) {
		return fmt.Errorf("seed rules: %d %s", code, body)
	}
	return nil
}

func ok2xx(code int) bool { return code >= 200 && code < 300 }

func (c *client) seedRecords() error {
	rng := rand.New(rand.NewSource(seed + 1))
	const chunk = 500
	for start := 0; start < numInitialRecord; start += chunk {
		end := start + chunk
		if end > numInitialRecord {
			end = numInitialRecord
		}
		batch := make([]alert, 0, end-start)
		for i := start; i < end; i++ {
			batch = append(batch, genAlert(rng))
		}
		code, body, err := c.do("POST", "/api/v1/alerts", batch)
		if err != nil {
			return err
		}
		if !ok2xx(code) {
			return fmt.Errorf("seed records: %d %s", code, body)
		}
	}
	return nil
}

// resetRuleAndRecord wipes the rule and record collections before seeding so
// repeat invocations don't compound state. We delete by an empty AlwaysTrue
// condition (q="" base64-encodes to empty → all docs).
func (c *client) reset() error {
	for _, coll := range []string{"rule", "record"} {
		code, body, err := c.do("DELETE", "/api/v1/"+coll, nil)
		if err != nil {
			return err
		}
		if !ok2xx(code) {
			return fmt.Errorf("reset %s: %d %s", coll, code, body)
		}
	}
	// Give the rule-plugin Reload a moment to settle (it watches the
	// collection via the syncer).
	time.Sleep(2 * time.Second)
	return nil
}

type latencies struct {
	mu   sync.Mutex
	data []time.Duration
}

func (l *latencies) add(d time.Duration) {
	l.mu.Lock()
	l.data = append(l.data, d)
	l.mu.Unlock()
}

func (l *latencies) summary() map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.data) == 0 {
		return map[string]any{"count": 0}
	}
	cp := make([]time.Duration, len(l.data))
	copy(cp, l.data)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	pct := func(p float64) time.Duration {
		idx := int(float64(len(cp)-1) * p)
		return cp[idx]
	}
	var sum time.Duration
	for _, d := range cp {
		sum += d
	}
	return map[string]any{
		"count":  len(cp),
		"min_ms": cp[0].Seconds() * 1000,
		"p50_ms": pct(0.50).Seconds() * 1000,
		"p90_ms": pct(0.90).Seconds() * 1000,
		"p95_ms": pct(0.95).Seconds() * 1000,
		"p99_ms": pct(0.99).Seconds() * 1000,
		"max_ms": cp[len(cp)-1].Seconds() * 1000,
		"avg_ms": (sum / time.Duration(len(cp))).Seconds() * 1000,
	}
}

type phaseResult struct {
	Name         string         `json:"name"`
	DurationSec  float64        `json:"duration_sec"`
	Ops          int64          `json:"ops"`
	Errors       int64          `json:"errors"`
	OpsPerSec    float64        `json:"ops_per_sec"`
	LatencyMs    map[string]any `json:"latency_ms"`
	SubLatencies map[string]any `json:"sub_latencies,omitempty"`
}

func runWriteBurst(c *client) phaseResult {
	rng := rand.New(rand.NewSource(seed + 2))
	jobs := make(chan alert, writeConcurrency*4)
	var wg sync.WaitGroup
	var errs int64
	lat := &latencies{}

	start := time.Now()
	for w := 0; w < writeConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				t0 := time.Now()
				code, _, err := c.do("POST", "/api/v1/alerts", a)
				lat.add(time.Since(t0))
				if err != nil || !ok2xx(code) {
					atomic.AddInt64(&errs, 1)
				}
			}
		}()
	}
	for i := 0; i < numWriteBurst; i++ {
		jobs <- genAlert(rng)
	}
	close(jobs)
	wg.Wait()
	dur := time.Since(start)

	return phaseResult{
		Name:        "write_burst",
		DurationSec: dur.Seconds(),
		Ops:         int64(numWriteBurst),
		Errors:      errs,
		OpsPerSec:   float64(numWriteBurst) / dur.Seconds(),
		LatencyMs:   lat.summary(),
	}
}

// runReadOnly exercises the read paths the UI most often hits.
// We measure each query family separately so we can compare them.
func runReadOnly(c *client) phaseResult {
	queries := []struct {
		name string
		path string
		body any
	}{
		{
			name: "list_record_limit100",
			path: "/api/v1/record?limit=100",
		},
		{
			name: "search_by_host",
			path: "/api/v1/record/search",
			body: map[string]any{
				"condition": map[string]any{
					"op": "=", "field": "host", "value": "srv-web-01",
				},
				"limit": 100,
			},
		},
		{
			name: "search_by_severity",
			path: "/api/v1/record/search",
			body: map[string]any{
				"condition": map[string]any{
					"op": "=", "field": "severity", "value": "critical",
				},
				"limit": 100,
			},
		},
		{
			name: "search_by_env_and_sev",
			path: "/api/v1/record/search",
			body: map[string]any{
				"condition": map[string]any{
					"op": "AND",
					"children": []any{
						map[string]any{"op": "=", "field": "environment", "value": "prod"},
						map[string]any{"op": "=", "field": "severity", "value": "error"},
					},
				},
				"limit": 100,
			},
		},
	}

	subLatencies := make(map[string]any)
	overall := &latencies{}
	var errs int64
	start := time.Now()

	for _, q := range queries {
		method := "GET"
		if q.body != nil {
			method = "POST"
		}
		jobs := make(chan struct{}, readConcurrency*4)
		var wg sync.WaitGroup
		lat := &latencies{}
		for w := 0; w < readConcurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range jobs {
					t0 := time.Now()
					code, _, err := c.do(method, q.path, q.body)
					d := time.Since(t0)
					lat.add(d)
					overall.add(d)
					if err != nil || !ok2xx(code) {
						atomic.AddInt64(&errs, 1)
					}
				}
			}()
		}
		for i := 0; i < numReadsPerKind; i++ {
			jobs <- struct{}{}
		}
		close(jobs)
		wg.Wait()
		subLatencies[q.name] = lat.summary()
	}
	dur := time.Since(start)

	total := int64(len(queries) * numReadsPerKind)
	return phaseResult{
		Name:         "read_only",
		DurationSec:  dur.Seconds(),
		Ops:          total,
		Errors:       errs,
		OpsPerSec:    float64(total) / dur.Seconds(),
		LatencyMs:    overall.summary(),
		SubLatencies: subLatencies,
	}
}

// runMixed: writes + reads in parallel for a fixed duration. Stops when
// either the write budget is exhausted or duration elapses.
func runMixed(c *client) phaseResult {
	writeRng := rand.New(rand.NewSource(seed + 3))
	var writeRngMu sync.Mutex
	nextAlert := func() alert {
		writeRngMu.Lock()
		defer writeRngMu.Unlock()
		return genAlert(writeRng)
	}

	writeLat := &latencies{}
	readLat := &latencies{}
	var writes, reads, errs int64
	done := make(chan struct{})

	var wg sync.WaitGroup
	start := time.Now()
	// 15 writer workers, 10 reader workers.
	for w := 0; w < 15; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if atomic.LoadInt64(&writes) >= mixedWrites {
					return
				}
				a := nextAlert()
				t0 := time.Now()
				code, _, err := c.do("POST", "/api/v1/alerts", a)
				writeLat.add(time.Since(t0))
				atomic.AddInt64(&writes, 1)
				if err != nil || !ok2xx(code) {
					atomic.AddInt64(&errs, 1)
				}
			}
		}()
	}
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(seed)))
			for {
				select {
				case <-done:
					return
				default:
				}
				if atomic.LoadInt64(&reads) >= mixedReads {
					return
				}
				var path string
				var body any
				switch rng.Intn(3) {
				case 0:
					path = "/api/v1/record?limit=50"
				case 1:
					path = "/api/v1/record/search"
					body = map[string]any{
						"condition": map[string]any{
							"op": "=", "field": "host",
							"value": hostsPool[rng.Intn(len(hostsPool))],
						},
						"limit": 50,
					}
				case 2:
					path = "/api/v1/record/search"
					body = map[string]any{
						"condition": map[string]any{
							"op": "=", "field": "severity",
							"value": severities[rng.Intn(len(severities))],
						},
						"limit": 50,
					}
				}
				method := "GET"
				if body != nil {
					method = "POST"
				}
				t0 := time.Now()
				code, _, err := c.do(method, path, body)
				readLat.add(time.Since(t0))
				atomic.AddInt64(&reads, 1)
				if err != nil || !ok2xx(code) {
					atomic.AddInt64(&errs, 1)
				}
			}
		}(r + 1)
	}

	go func() {
		time.Sleep(mixedDuration)
		close(done)
	}()
	wg.Wait()
	dur := time.Since(start)

	total := atomic.LoadInt64(&writes) + atomic.LoadInt64(&reads)
	return phaseResult{
		Name:        "mixed",
		DurationSec: dur.Seconds(),
		Ops:         total,
		Errors:      errs,
		OpsPerSec:   float64(total) / dur.Seconds(),
		LatencyMs: map[string]any{
			"writes": writeLat.summary(),
			"reads":  readLat.summary(),
		},
		SubLatencies: map[string]any{
			"writes": atomic.LoadInt64(&writes),
			"reads":  atomic.LoadInt64(&reads),
		},
	}
}

type benchReport struct {
	Backend   string        `json:"backend"`
	URL       string        `json:"url"`
	Seed      int64         `json:"seed"`
	NumRules  int           `json:"num_rules"`
	NumSeed   int           `json:"num_initial_records"`
	StartedAt string        `json:"started_at"`
	EndedAt   string        `json:"ended_at"`
	Phases    []phaseResult `json:"phases"`
}

func main() {
	url := flag.String("url", "http://localhost:5200", "snooze-server base URL")
	backend := flag.String("backend", "unknown", "label for the DB backend under test")
	out := flag.String("out", "result.json", "output JSON path")
	flag.Parse()

	c := newClient(*url)
	fmt.Printf("[bench] waiting for %s/readyz\n", *url)
	if err := c.waitReady(120 * time.Second); err != nil {
		die(err)
	}
	fmt.Println("[bench] server ready")

	if err := c.login(); err != nil {
		die(fmt.Errorf("login: %w", err))
	}
	fmt.Println("[bench] logged in (anonymous-admin)")

	fmt.Println("[bench] resetting rule/record collections")
	if err := c.reset(); err != nil {
		die(err)
	}

	fmt.Printf("[bench] seeding %d rules\n", numRules)
	t0 := time.Now()
	if err := c.seedRules(); err != nil {
		die(err)
	}
	fmt.Printf("[bench]   rules seeded in %s\n", time.Since(t0))
	// Allow the rule plugin's Reload (watched via syncer) to pick up the
	// freshly-written rules before we start the write-burst.
	time.Sleep(3 * time.Second)

	fmt.Printf("[bench] seeding %d initial records\n", numInitialRecord)
	t0 = time.Now()
	if err := c.seedRecords(); err != nil {
		die(err)
	}
	fmt.Printf("[bench]   records seeded in %s\n", time.Since(t0))

	rep := benchReport{
		Backend:   *backend,
		URL:       *url,
		Seed:      seed,
		NumRules:  numRules,
		NumSeed:   numInitialRecord,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}

	fmt.Println("[bench] PHASE 1 — write burst")
	p := runWriteBurst(c)
	fmt.Printf("[bench]   %d ops in %.2fs → %.1f ops/s (errors=%d)\n", p.Ops, p.DurationSec, p.OpsPerSec, p.Errors)
	rep.Phases = append(rep.Phases, p)

	fmt.Println("[bench] PHASE 2 — read only")
	p = runReadOnly(c)
	fmt.Printf("[bench]   %d ops in %.2fs → %.1f ops/s (errors=%d)\n", p.Ops, p.DurationSec, p.OpsPerSec, p.Errors)
	rep.Phases = append(rep.Phases, p)

	fmt.Println("[bench] PHASE 3 — mixed read+write")
	p = runMixed(c)
	fmt.Printf("[bench]   %d ops in %.2fs → %.1f ops/s (errors=%d)\n", p.Ops, p.DurationSec, p.OpsPerSec, p.Errors)
	rep.Phases = append(rep.Phases, p)

	rep.EndedAt = time.Now().UTC().Format(time.RFC3339)
	f, err := os.Create(*out)
	if err != nil {
		die(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		die(err)
	}
	fmt.Printf("[bench] wrote %s\n", *out)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
