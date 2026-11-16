package main

import (
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

type Spec struct {
	Ticker   string `json:"ticker"`
	Provider string `json:"provider_ticker"`
	Currency string `json:"expected_currency"`
}
type Config struct {
	Instruments []Spec `json:"instruments"`
}
type RawPoint struct {
	Date     string
	Adjusted float64
	Close    float64
	Dividend float64
}
type Chart struct {
	Currency string
	Symbol   string
	Name     string
	Rows     []RawPoint
	URL      string
}
type chartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency string `json:"currency"`
				Symbol   string `json:"symbol"`
				Name     string `json:"longName"`
				TZ       string `json:"exchangeTimezoneName"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Adjclose []struct {
					Adjclose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
			Events struct {
				Dividends map[string]struct {
					Amount float64 `json:"amount"`
					Date   int64   `json:"date"`
				} `json:"dividends"`
			} `json:"events"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

func canonical(s string) string {
	if s == "GBp" || s == "GBX" {
		return "GBX"
	}
	return strings.ToUpper(s)
}
func finitePositive(x float64) bool { return x > 0 && !math.IsNaN(x) && !math.IsInf(x, 0) }
func compatible(got, want string) bool {
	got = canonical(got)
	want = canonical(want)
	return got == want || ((got == "GBP" || got == "GBX") && (want == "GBP" || want == "GBX"))
}
func parseChart(b []byte, symbol, source string, needAdjusted bool) (Chart, error) {
	var out chartResponse
	if e := json.Unmarshal(b, &out); e != nil {
		return Chart{}, fmt.Errorf("invalid provider JSON: %w", e)
	}
	if len(out.Chart.Result) == 0 {
		return Chart{}, fmt.Errorf("provider returned no history for %s (%v)", symbol, out.Chart.Error)
	}
	r := out.Chart.Result[0]
	if r.Meta.Currency == "" {
		return Chart{}, fmt.Errorf("missing currency metadata")
	}
	if len(r.Indicators.Quote) == 0 {
		return Chart{}, fmt.Errorf("missing closing prices")
	}
	if needAdjusted && len(r.Indicators.Adjclose) == 0 {
		return Chart{}, fmt.Errorf("adjusted close unavailable; unadjusted prices are not substituted")
	}
	loc, e := time.LoadLocation(r.Meta.TZ)
	if e != nil {
		loc = time.UTC
	}
	dividends := map[string]float64{}
	for _, d := range r.Events.Dividends {
		dividends[time.Unix(d.Date, 0).In(loc).Format("2006-01-02")] += d.Amount
	}
	c := Chart{Currency: canonical(r.Meta.Currency), Symbol: r.Meta.Symbol, Name: r.Meta.Name, URL: source}
	cutoff := time.Now().UTC().Format("2006-01-02")
	seen := map[string]bool{}
	for i, t := range r.Timestamp {
		d := time.Unix(t, 0).In(loc).Format("2006-01-02")
		if d >= cutoff {
			continue
		}
		quotes := r.Indicators.Quote[0].Close
		if i >= len(quotes) || quotes[i] == nil || !finitePositive(*quotes[i]) {
			continue
		}
		v := *quotes[i]
		if needAdjusted {
			arr := r.Indicators.Adjclose[0].Adjclose
			if i >= len(arr) || arr[i] == nil || !finitePositive(*arr[i]) {
				continue
			}
			v = *arr[i]
		}
		if seen[d] {
			return Chart{}, fmt.Errorf("duplicate observation date %s", d)
		}
		seen[d] = true
		c.Rows = append(c.Rows, RawPoint{Date: d, Adjusted: v, Close: *quotes[i], Dividend: dividends[d]})
	}
	sort.Slice(c.Rows, func(i, j int) bool { return c.Rows[i].Date < c.Rows[j].Date })
	if len(c.Rows) < 3 {
		return Chart{}, fmt.Errorf("fewer than three valid completed-session observations")
	}
	return c, nil
}
func (a *App) chart(symbol string, adjusted bool) (Chart, error) {
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(-10, 0, -7)
	q := url.Values{"period1": {strconv.FormatInt(start.Unix(), 10)}, "period2": {strconv.FormatInt(end.Unix(), 10)}, "interval": {"1d"}, "events": {"div,splits"}, "includeAdjustedClose": {"true"}}
	var last error
	for _, host := range []string{"query1.finance.yahoo.com", "query2.finance.yahoo.com"} {
		u := "https://" + host + "/v8/finance/chart/" + url.PathEscape(symbol) + "?" + q.Encode()
		b, e := a.rawGet(u)
		if e != nil {
			last = e
			if strings.Contains(e.Error(), "429") {
				return Chart{}, e
			}
			continue
		}
		c, e := parseChart(b, symbol, u, adjusted)
		if e == nil {
			return c, nil
		}
		last = e
	}
	return Chart{}, last
}

// A documented one-for-one technical share conversion permits an explicit chain.
// The old adjusted-price series is rescaled to raw price at its last observation;
// new adjusted returns then continue, with any transition-date distribution added.
func chainAlzchem(old, new Chart) (Chart, error) {
	cut := "2026-09-17"
	var pre, post []RawPoint
	for _, r := range old.Rows {
		if r.Date < cut {
			pre = append(pre, r)
		}
	}
	for _, r := range new.Rows {
		if r.Date >= cut {
			post = append(post, r)
		}
	}
	if len(pre) < 3 || len(post) < 1 {
		return new, fmt.Errorf("insufficient observations around registered-share conversion")
	}
	prev, next := pre[len(pre)-1], post[0]
	d1, _ := time.Parse("2006-01-02", prev.Date)
	d2, _ := time.Parse("2006-01-02", next.Date)
	if d2.Sub(d1) > 7*24*time.Hour {
		return new, fmt.Errorf("gap around technical share conversion exceeds seven days")
	}
	bridge := (next.Close + next.Dividend) / prev.Close
	if bridge < .5 || bridge > 1.5 {
		return new, fmt.Errorf("suspect transition return; old and new prices were not joined")
	}
	factor := prev.Close / prev.Adjusted
	for i := range pre {
		pre[i].Adjusted *= factor
	}
	factor = prev.Close * bridge / next.Adjusted
	for i := range post {
		post[i].Adjusted *= factor
	}
	new.Rows = append(pre, post...)
	new.URL += " ; prior history: " + old.URL
	return new, nil
}
func fxAt(fx []Point, d string) (Point, bool) {
	i := sort.Search(len(fx), func(i int) bool { return fx[i].Date > d }) - 1
	if i < 0 {
		return Point{}, false
	}
	target, e := time.Parse("2006-01-02", d)
	if e != nil {
		return Point{}, false
	}
	actual, _ := time.Parse("2006-01-02", fx[i].Date)
	if target.Sub(actual) > 4*24*time.Hour {
		return Point{}, false
	}
	return fx[i], true
}
func convert(c Chart, fx []Point) ([]Point, Mark, int, error) {
	var out []Point
	var mark Mark
	dropped := 0
	for _, r := range c.Rows {
		scale, fxDate := 1.0, r.Date
		if c.Currency == "GBX" {
			scale = .01
		} else if c.Currency != "GBP" {
			v, ok := fxAt(fx, r.Date)
			if !ok {
				dropped++
				continue
			}
			scale = v.Value
			fxDate = v.Date
		}
		v := r.Adjusted * scale
		if !finitePositive(v) {
			return nil, Mark{}, 0, fmt.Errorf("invalid GBP-adjusted level")
		}
		if len(out) > 0 && math.Abs(v/out[len(out)-1].Value-1) > .5 {
			return nil, Mark{}, 0, fmt.Errorf("absolute one-observation move above 50%% on %s; series quarantined", r.Date)
		}
		out = append(out, Point{r.Date, v})
		mark = Mark{r.Date, r.Close, c.Currency, r.Close * scale, fxDate}
	}
	if len(out) < 3 {
		return nil, Mark{}, dropped, fmt.Errorf("insufficient GBP observations after FX alignment")
	}
	return out, mark, dropped, nil
}
func (a *App) refreshMarkets() {
	raw, _ := assets.ReadFile("data/provider_map.json")
	var cfg Config
	if e := json.Unmarshal(raw, &cfg); e != nil {
		a.failure("config", e)
		return
	}
	fx := map[string][]Point{}
	for _, pair := range []struct{ Currency, Symbol string }{{"USD", "GBPUSD=X"}, {"EUR", "GBPEUR=X"}} {
		c, e := a.chart(pair.Symbol, false)
		if e != nil {
			a.failure("FX_"+pair.Currency, e)
			continue
		}
		for _, r := range c.Rows {
			fx[pair.Currency] = append(fx[pair.Currency], Point{r.Date, 1 / r.Close})
		}
		a.progress("Retrieved " + pair.Currency + "/GBP history")
	}
	jobs := make(chan Spec)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for spec := range jobs {
				func() {
					defer a.progress("Processed " + spec.Ticker)
					c, e := a.chart(spec.Provider, true)
					if e != nil {
						a.failure(spec.Ticker, e)
						return
					}
					if !compatible(c.Currency, spec.Currency) {
						a.failure(spec.Ticker, fmt.Errorf("currency mismatch: expected %s, got %s", spec.Currency, c.Currency))
						return
					}
					chained := false
					chainNote := ""
					if spec.Ticker == "ACT.DE" && c.Rows[0].Date > "2026-09-01" {
						old, err := a.chart("ACT.DE", true)
						if err == nil {
							var joined Chart
							joined, err = chainAlzchem(old, c)
							if err == nil {
								c = joined
								chained = true
							}
						}
						if err != nil {
							chainNote = "Old ACT history unavailable or failed conversion checks: " + err.Error()
						}
					}
					// A single unverified extreme observation must not make the
					// entire holding disappear. Retain only the contiguous segment
					// after the LAST >50% GBP-level jump; never bridge that jump.
					// The discarded history is explicitly disclosed in metadata.
					fullCount := len(c.Rows)
					p, m, dropped, e := convert(c, fx[c.Currency])
					truncationNote := ""
					if e != nil && strings.Contains(e.Error(), "one-observation move above 50%") {
						// Locate breaks in GBP units (FX can contribute to a break).
						lastBreak := -1
						prev := 0.0
						for i, r := range c.Rows {
							scale := 1.0
							if c.Currency == "GBX" {
								scale = .01
							} else if c.Currency != "GBP" {
								fxp, ok := fxAt(fx[c.Currency], r.Date)
								if !ok {
									continue
								}
								scale = fxp.Value
							}
							v := r.Adjusted * scale
							if !finitePositive(v) {
								continue
							}
							if prev > 0 && math.Abs(v/prev-1) > .5 {
								lastBreak = i
							}
							prev = v
						}
						if lastBreak > 0 && len(c.Rows)-lastBreak >= 30 {
							truncationNote = fmt.Sprintf("Unverified >50%% jump at %s: %d earlier observations excluded; history begins after break. Do not interpret this as full ten-year history or a validated corporate-action adjustment.", c.Rows[lastBreak].Date, lastBreak)
							c.Rows = c.Rows[lastBreak:]
							p, m, dropped, e = convert(c, fx[c.Currency])
						}
					}
					if e != nil {
						a.failure(spec.Ticker, e)
						return
					}
					meta := map[string]any{"source": "Yahoo Finance provider-adjusted close; automatic retrieval", "sourceURL": c.URL, "providerTicker": spec.Provider, "currency": c.Currency, "first": p[0].Date, "last": p[len(p)-1].Date, "rows": len(p), "retrieved_utc": time.Now().UTC().Format(time.RFC3339), "basis": "Dividend/split-adjusted market-price total-return proxy in GBP; investor withholding taxes not reconstructed", "fx_dropped": dropped, "stale": false, "corporateActionChain": chained, "continuityNote": chainNote, "originalRows": fullCount, "truncationWarning": truncationNote}
					if chained {
						meta["chainAuthority"] = "https://www.alzchem.com/en/investor-relations/stock/"
						meta["chainMethod"] = "Verified 1:1 technical conversion on 2026-09-17; explicit raw-price bridge between adjusted-return segments"
					}
					if spec.Ticker == "SWDA.L" {
						meta["benchmarkWarning"] = "ETF proxy, NOT the licensed MSCI World Net Total Return GBP index"
					}
					a.store(spec.Ticker, p, meta, &m)
					last, _ := time.Parse("2006-01-02", p[len(p)-1].Date)
					if time.Since(last) > 7*24*time.Hour {
						a.failure(spec.Ticker, fmt.Errorf("provider history ends %s; cached but stale", p[len(p)-1].Date))
					}
					if truncationNote != "" {
						a.failure("ACT_history_warning", fmt.Errorf("%s", truncationNote))
					}
					if spec.Ticker == "KAP.L" && len(p) < 252 {
						a.failure("KAP_history_warning", fmt.Errorf("provider supplied only %d daily observations; older Kazatomprom GDR history unavailable from this symbol; no pre-inception values generated", len(p)))
					}
					if chainNote != "" {
						a.failure("ACT_continuity", fmt.Errorf("%s", chainNote))
					}
				}()
			}
		}()
	}
	for _, s := range cfg.Instruments {
		jobs <- s
	}
	close(jobs)
	wg.Wait()
}
func parseDate(s string) (string, bool) {
	s = strings.TrimSpace(strings.Trim(s, "\ufeff"))
	for _, layout := range []string{"2006-01-02", "02 Jan 2006", "2 Jan 2006", "02-Jan-2006", "02/01/2006", "2006/01/02"} {
		if t, e := time.Parse(layout, s); e == nil {
			return t.Format("2006-01-02"), true
		}
	}
	if len(s) >= 10 {
		if t, e := time.Parse("2006-01-02", s[:10]); e == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}
func csvPoints(b []byte, column string) ([]Point, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff")))
	reader.FieldsPerRecord = -1
	rows, e := reader.ReadAll()
	if e != nil {
		return nil, e
	}
	col := -1
	start := 0
	for i, r := range rows {
		for j, v := range r {
			if strings.EqualFold(strings.TrimSpace(v), column) {
				col = j
				start = i + 1
				break
			}
		}
		if col >= 0 {
			break
		}
	}
	if col < 0 {
		return nil, fmt.Errorf("CSV column %s not found", column)
	}
	result := map[string]float64{}
	for _, r := range rows[start:] {
		if len(r) <= col || len(r) < 2 {
			continue
		}
		d, ok := parseDate(r[0])
		if !ok {
			continue
		}
		v, e := strconv.ParseFloat(strings.TrimSpace(r[col]), 64)
		if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		result[d] = v
	}
	p := []Point{}
	for d, v := range result {
		p = append(p, Point{d, v})
	}
	sort.Slice(p, func(i, j int) bool { return p[i].Date < p[j].Date })
	if len(p) < 3 {
		return nil, fmt.Errorf("no usable dated numeric observations")
	}
	return p, nil
}
func compoundSONIA(rates []Point) ([]Point, error) {
	if len(rates) < 3 {
		return nil, fmt.Errorf("insufficient SONIA rates")
	}
	out := []Point{{rates[0].Date, 100}}
	value := 100.0
	for i := 1; i < len(rates); i++ {
		prev, next := rates[i-1], rates[i]
		d1, _ := time.Parse("2006-01-02", prev.Date)
		d2, _ := time.Parse("2006-01-02", next.Date)
		days := int(d2.Sub(d1).Hours() / 24)
		if days < 1 || days > 7 || prev.Value <= -100 || prev.Value > 100 {
			return nil, fmt.Errorf("invalid SONIA observation/gap around %s", next.Date)
		}
		base := value
		for d := 1; d <= days; d++ {
			value = base * (1 + prev.Value/100*float64(d)/365)
			out = append(out, Point{d1.AddDate(0, 0, d).Format("2006-01-02"), value})
		}
	}
	// The latest published overnight rate accrues to the next business day only.
	last := rates[len(rates)-1]
	d, _ := time.Parse("2006-01-02", last.Date)
	end := d.AddDate(0, 0, 1)
	for end.Weekday() == time.Saturday || end.Weekday() == time.Sunday {
		end = end.AddDate(0, 0, 1)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if end.After(today) {
		end = today
	}
	base := value
	for n := 1; !d.AddDate(0, 0, n).After(end); n++ {
		out = append(out, Point{d.AddDate(0, 0, n).Format("2006-01-02"), base * (1 + last.Value/100*float64(n)/365)})
	}
	return out, nil
}
func (a *App) fred(id string) ([]Point, string, error) {
	u := "https://fred.stlouisfed.org/graph/graph.csv?id=" + url.QueryEscape(id) + "&cosd=" + time.Now().AddDate(-10, 0, -7).Format("2006-01-02")
	b, e := a.rawGet(u)
	if e != nil {
		return nil, u, e
	}
	p, e := csvPoints(b, id)
	return p, u, e
}
func (a *App) refreshMacro() {
	start := time.Now().AddDate(-10, 0, -7)
	q := url.Values{"csv.x": {"yes"}, "Datefrom": {start.Format("02/Jan/2006")}, "Dateto": {"now"}, "SeriesCodes": {"IUDSOIA"}, "CSVF": {"TN"}, "UsingCodes": {"Y"}, "VPD": {"Y"}, "VFD": {"N"}}
	source := "https://www.bankofengland.co.uk/boeapps/database/_iadb-fromshowcolumns.asp?" + q.Encode()
	b, e := a.rawGet(source)
	var rates []Point
	if e == nil {
		rates, e = csvPoints(b, "IUDSOIA")
	}
	if e != nil {
		rates, source, e = a.fred("IUDSOIA")
	}
	if e == nil {
		var p []Point
		p, e = compoundSONIA(rates)
		if e == nil {
			a.store("CASH_GBP", p, map[string]any{"source": "Bank of England SONIA overnight rates", "sourceURL": source, "basis": "ACT/365 compounded published SONIA; previous business-day rate applied across its overnight period; not an investable fund", "retrieved_utc": time.Now().UTC().Format(time.RFC3339), "first": p[0].Date, "last": p[len(p)-1].Date, "stale": false}, nil)
		}
	}
	if e != nil {
		a.failure("CASH_GBP", e)
	}
	a.progress("Processed SONIA cash benchmark")
	specs := []struct{ ID, Series, Transform, Unit string }{{"US_REAL_YIELD", "DFII10", "difference", "percentage point"}, {"UST_10Y", "DGS10", "difference", "percentage point"}, {"CREDIT_SPREAD", "BAMLH0A0HYM2", "difference", "percentage point"}, {"OIL", "DCOILWTICO", "difference", "USD per barrel change (can include negative prices)"}, {"VIX", "VIXCLS", "difference", "index point"}, {"USD", "DTWEXBGS", "return", "decimal index return"}}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3)
	for _, spec := range specs {
		wg.Add(1)
		go func(s struct{ ID, Series, Transform, Unit string }) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem; a.progress("Processed " + s.ID) }()
			p, src, err := a.fred(s.Series)
			if err != nil {
				a.mu.Lock()
				if m := a.payload.Meta[s.ID]; m != nil {
					m["stale"] = true
					m["refreshError"] = err.Error()
				}
				a.mu.Unlock()
				a.failure(s.ID, err)
				return
			}
			v := []FactorPoint{}
			for _, x := range p {
				v = append(v, FactorPoint{x.Date, x.Value, s.Transform, s.Unit})
			}
			a.mu.Lock()
			a.payload.Factors[s.ID] = v
			a.payload.Meta[s.ID] = map[string]any{"source": "FRED / originating institution", "sourceURL": src, "retrieved_utc": time.Now().UTC().Format(time.RFC3339), "basis": "Observed market series; current vintage, not point-in-time economic release data", "stale": false}
			a.mu.Unlock()
		}(spec)
	}
	wg.Wait()
}

type rssDocument struct {
	Channel struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Date  string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
	Entries []struct {
		Title   string `xml:"title"`
		Updated string `xml:"updated"`
		Links   []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func parseNews(b []byte, source string) ([]News, error) {
	var d rssDocument
	if e := xml.Unmarshal(b, &d); e != nil {
		return nil, e
	}
	out := []News{}
	for _, x := range d.Channel.Items {
		u, e := url.Parse(x.Link)
		if e == nil && (u.Scheme == "https" || u.Scheme == "http") {
			out = append(out, News{x.Title, x.Link, x.Date, source})
		}
	}
	for _, x := range d.Entries {
		for _, l := range x.Links {
			u, e := url.Parse(l.Href)
			if e == nil && (u.Scheme == "https" || u.Scheme == "http") && (l.Rel == "" || l.Rel == "alternate") {
				out = append(out, News{x.Title, l.Href, x.Updated, source})
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no RSS/Atom entries found")
	}
	if len(out) > 25 {
		out = out[:25]
	}
	return out, nil
}
func (a *App) refreshNews() {
	feeds := []struct{ Name, URL string }{{"Federal Reserve", "https://www.federalreserve.gov/feeds/press_all.xml"}, {"Bank of England", "https://www.bankofengland.co.uk/rss/news"}, {"European Central Bank", "https://www.ecb.europa.eu/rss/press.html"}, {"UK Ministry of Defence", "https://www.gov.uk/government/organisations/ministry-of-defence.atom"}}
	var wg sync.WaitGroup
	for _, f := range feeds {
		wg.Add(1)
		go func(name, u string) {
			defer wg.Done()
			defer a.progress("Processed " + name + " news")
			b, e := a.rawGet(u)
			if e != nil {
				a.failure("News: "+name, e)
				return
			}
			v, e := parseNews(b, name)
			if e != nil {
				a.failure("News: "+name, e)
				return
			}
			a.mu.Lock()
			old := a.payload.News[:0]
			for _, n := range a.payload.News {
				if n.Source != name {
					old = append(old, n)
				}
			}
			a.payload.News = append(old, v...)
			a.mu.Unlock()
		}(f.Name, f.URL)
	}
	wg.Wait()
}
