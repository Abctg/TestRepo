// Static GitHub Pages data exporter. Runs on GitHub Actions, not a user's device.
package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

//go:embed data/provider_map.json
var assets embed.FS

type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}
type FactorPoint struct {
	Date      string  `json:"date"`
	Value     float64 `json:"value"`
	Transform string  `json:"transform"`
	Unit      string  `json:"unit"`
}
type Mark struct {
	Date     string  `json:"date"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
	GBP      float64 `json:"gbp"`
	FXDate   string  `json:"fxDate"`
}
type News struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Date   string `json:"date"`
	Source string `json:"source"`
}
type Payload struct {
	AsOf      string                    `json:"asOf"`
	History   map[string][]Point        `json:"history"`
	Meta      map[string]map[string]any `json:"historyMeta"`
	Factors   map[string][]FactorPoint  `json:"factors"`
	Marks     map[string]Mark           `json:"marks"`
	News      []News                    `json:"news"`
	Errors    map[string]string         `json:"errors"`
	Retrieved string                    `json:"retrieved"`
	Revision  int64                     `json:"revision"`
}
type Status struct {
	Busy        bool              `json:"busy"`
	Message     string            `json:"message"`
	Completed   int               `json:"completed"`
	Total       int               `json:"total"`
	Revision    int64             `json:"revision"`
	LastAttempt string            `json:"lastAttempt"`
	NextAttempt string            `json:"nextAttempt"`
	Errors      map[string]string `json:"errors"`
}
type App struct {
	mu      sync.RWMutex
	payload Payload
	status  Status
	dir     string
	client  *http.Client
	token   string
	stop    chan struct{}
}

func emptyPayload() Payload {
	return Payload{History: map[string][]Point{}, Meta: map[string]map[string]any{}, Factors: map[string][]FactorPoint{}, Marks: map[string]Mark{}, News: []News{}, Errors: map[string]string{}}
}
func newApp(dir string) *App {
	a := &App{dir: dir, payload: emptyPayload(), stop: make(chan struct{}), client: &http.Client{Timeout: 14 * time.Second}}
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	a.token = hex.EncodeToString(b)
	if raw, e := os.ReadFile(filepath.Join(dir, "market-cache.json")); e == nil {
		var p Payload
		if json.Unmarshal(raw, &p) == nil && p.History != nil && p.Meta != nil && p.Factors != nil && p.Marks != nil && p.Errors != nil {
			a.payload = p
		}
	}
	a.status = Status{Message: "Starting automatic retrieval", Revision: a.payload.Revision, Errors: map[string]string{}}
	return a
}
func (a *App) rawGet(u string) ([]byte, error) {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		req, e := http.NewRequest("GET", u, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 PortfolioObservatory/2.0 personal-research")
		req.Header.Set("Accept", "application/json,text/csv,application/xml,text/xml,*/*")
		res, e := a.client.Do(req)
		if e != nil {
			last = e
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(res.Body, 24<<20))
		res.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if res.StatusCode != 200 {
			last = fmt.Errorf("provider returned HTTP %d", res.StatusCode)
			if res.StatusCode == 429 {
				return nil, last
			}
			continue
		}
		if len(data) >= 24<<20 {
			return nil, fmt.Errorf("provider response exceeded safe size limit")
		}
		return data, nil
	}
	return nil, last
}
func (a *App) persist() error {
	a.mu.RLock()
	b, e := json.Marshal(a.payload)
	a.mu.RUnlock()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(a.dir, 0700); e != nil {
		return e
	}
	p := filepath.Join(a.dir, "market-cache.json")
	if e = os.WriteFile(p+".tmp", b, 0600); e != nil {
		return e
	}
	return os.Rename(p+".tmp", p)
}
func (a *App) progress(message string) {
	a.mu.Lock()
	a.status.Completed++
	a.status.Message = message
	a.mu.Unlock()
}
func (a *App) failure(id string, e error) {
	a.mu.Lock()
	a.payload.Errors[id] = e.Error()
	a.status.Errors[id] = e.Error()
	if m := a.payload.Meta[id]; m != nil {
		m["stale"] = true
		m["refreshError"] = e.Error()
	}
	a.mu.Unlock()
	log.Printf("%s: %v", id, e)
}
func (a *App) store(id string, p []Point, m map[string]any, mark *Mark) {
	if len(p) < 3 {
		a.failure(id, fmt.Errorf("fewer than three valid observations"))
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.payload.History[id] = p
	a.payload.Meta[id] = m
	a.payload.AsOf = time.Now().UTC().Format("2006-01-02")
	a.payload.Revision = time.Now().UnixMilli()
	a.status.Revision = a.payload.Revision
	delete(a.payload.Errors, id)
	delete(a.status.Errors, id)
	if mark != nil {
		a.payload.Marks[id] = *mark
	}
}
func (a *App) refresh() {
	a.mu.Lock()
	if a.status.Busy {
		a.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	a.status = Status{Busy: true, Message: "Downloading adjusted histories and FX", Total: 35, Revision: a.payload.Revision, LastAttempt: now.Format(time.RFC3339), Errors: map[string]string{}}
	a.payload.Errors = map[string]string{}
	a.mu.Unlock()
	defer func() {
		if e := recover(); e != nil {
			a.failure("refresh", fmt.Errorf("refresh interrupted: %v", e))
		}
		a.mu.Lock()
		a.payload.AsOf = time.Now().UTC().Format("2006-01-02")
		a.payload.Retrieved = time.Now().UTC().Format(time.RFC3339)
		a.payload.Revision = time.Now().UnixMilli()
		a.status.Revision = a.payload.Revision
		a.status.Busy = false
		delay := 6 * time.Hour
		if len(a.status.Errors) > 0 {
			delay = 15 * time.Minute
		}
		a.status.NextAttempt = time.Now().UTC().Add(delay).Format(time.RFC3339)
		a.status.Message = fmt.Sprintf("%d cached series; %d refresh warnings", len(a.payload.History), len(a.status.Errors))
		a.mu.Unlock()
		if e := a.persist(); e != nil {
			a.failure("cache", e)
		}
	}()
	a.refreshMarkets()
	a.refreshMacro()
	a.refreshNews()
}
func (a *App) autoLoop() {
	a.refresh()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-a.stop:
			return
		case <-t.C:
			a.mu.RLock()
			s := a.status
			a.mu.RUnlock()
			d, e := time.Parse(time.RFC3339, s.NextAttempt)
			if !s.Busy && (e != nil || time.Now().After(d)) {
				a.refresh()
			}
		}
	}
}

// A failed provider request never creates a synthetic return or replaces a valid cache.
func main() {
	out := flag.String("out", "../data/market.json", "Output JSON path deployed with GitHub Pages")
	cacheDir := flag.String("cache-dir", "../.market-cache", "Retained history from the previous scheduled action")
	flag.Parse()
	a := newApp(*cacheDir)
	a.refresh()
	a.mu.RLock()
	payload := a.payload
	a.mu.RUnlock()
	if payload.History == nil || payload.Meta == nil {
		log.Fatal("Internal error: missing series maps")
	}
	content, err := json.Marshal(payload)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, content, 0644); err != nil {
		log.Fatal(err)
	}
	log.Printf("Published %d market histories, %d factors, %d marks, %d provider warnings, %d bytes", len(payload.History), len(payload.Factors), len(payload.Marks), len(payload.Errors), len(content))
	if len(payload.History) < 20 {
		log.Printf("WARNING: %d out of 20 requested holdings have histories; the dashboard will show incomplete data", len(payload.History))
	}
	fmt.Printf("DATA_READY=%s\n", *out)
}

