package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Timing is the aggregate of one stage's spans.
type Timing struct {
	C    int     `json:"c"`
	Mean float64 `json:"m"`
	P95  float64 `json:"p95"`
	Max  float64 `json:"mx"`
}

// JStageTiming pins a timing to a chain index of one route.
type JStageTiming struct {
	I     int     `json:"i"`
	C     int     `json:"c"`
	Mean  float64 `json:"m"`
	P95   float64 `json:"p95"`
	Max   float64 `json:"mx"`
	Share float64 `json:"sh"`
}

// JRouteTiming is what a route cost across every span we were given.
type JRouteTiming struct {
	Spans  int            `json:"spans"`
	Total  float64        `json:"total"`
	Worst  float64        `json:"worst"`
	Slow   int            `json:"slow"`
	Stages []JStageTiming `json:"st"`
}

type rawSpan struct {
	Method string  `json:"method"`
	Route  string  `json:"route"`
	Path   string  `json:"path"`
	Stage  string  `json:"stage"`
	Name   string  `json:"name"`
	MS     float64 `json:"ms"`
	Dur    float64 `json:"duration_ms"`
}

// buildTiming reads a trace file and attributes its spans to the chain of each
// route. Two shapes are accepted: a flat list of spans, or an OTLP/JSON export.
// Spans that cannot be matched to a chain position are counted but not plotted.
func buildTiming(path string, routes []*Route) (map[string]JRouteTiming, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	spans, err := parseSpans(b)
	if err != nil {
		return nil, err
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("no spans in %s", path)
	}

	byPath := map[string][]*Route{}
	for _, r := range routes {
		byPath[r.Path] = append(byPath[r.Path], r)
	}

	agg := map[string]map[string][]float64{} // "METHOD path" -> stage -> ms
	for _, s := range spans {
		if s.Route == "" || s.Stage == "" || s.MS <= 0 {
			continue
		}
		method := strings.ToUpper(s.Method)
		if method == "" {
			if rs := byPath[s.Route]; len(rs) == 1 {
				method = rs[0].Method
			}
		}
		key := method + " " + s.Route
		m := agg[key]
		if m == nil {
			m = map[string][]float64{}
			agg[key] = m
		}
		m[s.Stage] = append(m[s.Stage], s.MS)
	}

	out := map[string]JRouteTiming{}
	for _, r := range routes {
		key := r.Method + " " + r.Path
		m := agg[key]
		if m == nil {
			continue
		}
		rt := JRouteTiming{Slow: -1}
		for idx, ref := range r.Chain {
			ms, ok := lookupStage(m, ref.Name)
			if !ok {
				continue
			}
			st := JStageTiming{I: idx, C: len(ms), Mean: mean(ms), P95: pct(ms, 0.95), Max: maxOf(ms)}
			rt.Total += st.Mean
			if st.P95 > rt.Worst {
				rt.Worst = st.P95
			}
			rt.Spans += st.C
			rt.Stages = append(rt.Stages, st)
		}
		if len(rt.Stages) == 0 {
			continue
		}
		for i := range rt.Stages {
			if rt.Total > 0 {
				rt.Stages[i].Share = rt.Stages[i].Mean / rt.Total
			}
			if rt.Slow < 0 || rt.Stages[i].Mean > rt.Stages[rt.Slow].Mean {
				rt.Slow = i
			}
		}
		out[key] = rt
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no span matched a route in %s — check the route and stage names", path)
	}
	return out, nil
}

// lookupStage finds the spans for a chain entry. Trace stage names are usually
// a middleware constructor (`cors.New`) but often only `cors` or `New`, so the
// match falls back to a suffix/prefix comparison before giving up.
func lookupStage(m map[string][]float64, chainName string) ([]float64, bool) {
	if ms, ok := m[chainName]; ok {
		return ms, true
	}
	want := strings.ToLower(lastSegment(chainName))
	for name, ms := range m {
		got := strings.ToLower(lastSegment(name))
		if got == want || strings.Contains(strings.ToLower(name), strings.ToLower(chainName)) ||
			strings.Contains(strings.ToLower(chainName), strings.ToLower(name)) {
			return ms, true
		}
	}
	return nil, false
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func parseSpans(b []byte) ([]rawSpan, error) {
	var flat []rawSpan
	if err := json.Unmarshal(b, &flat); err == nil {
		return flat, nil
	}
	var wrap struct {
		Spans []rawSpan `json:"spans"`
	}
	if err := json.Unmarshal(b, &wrap); err == nil && len(wrap.Spans) > 0 {
		return wrap.Spans, nil
	}
	var otlp struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Spans []struct {
					Name          string `json:"name"`
					Start         string `json:"startTimeUnixNano"`
					End           string `json:"endTimeUnixNano"`
					StartUnixNano any    `json:"start_unix_nano"`
					EndUnixNano   any    `json:"end_unix_nano"`
					Attributes    []attr `json:"attributes"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(b, &otlp); err != nil {
		return nil, fmt.Errorf("unrecognised trace file: want a list of spans or an OTLP json export")
	}
	var out []rawSpan
	for _, rs := range otlp.ResourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, sp := range ss.Spans {
				s := rawSpan{Name: sp.Name}
				for _, a := range sp.Attributes {
					switch a.Key {
					case "http.route", "http.target", "url.path":
						if s.Route == "" {
							s.Route = a.Value.StringValue
						}
					case "http.method", "http.request.method":
						if s.Method == "" {
							s.Method = a.Value.StringValue
						}
					case "synoptic.route", "route":
						s.Route = a.Value.StringValue
					case "synoptic.stage", "stage":
						s.Stage = a.Value.StringValue
					}
				}
				if s.Stage == "" {
					s.Stage = sp.Name
				}
				if s.MS == 0 {
					s.MS = nanosToMS(sp.Start, sp.End, sp.StartUnixNano, sp.EndUnixNano)
				}
				out = append(out, s)
			}
		}
	}
	return out, nil
}

type attr struct {
	Key   string `json:"key"`
	Value struct {
		StringValue string  `json:"stringValue"`
		IntValue    string  `json:"intValue"`
		DoubleValue float64 `json:"doubleValue"`
	} `json:"value"`
}

func nanosToMS(a, b string, ai, bi any) float64 {
	if a != "" && b != "" {
		var x, y float64
		if _, err := fmt.Sscan(a, &x); err == nil {
			if _, err := fmt.Sscan(b, &y); err == nil && y > x {
				return (y - x) / 1e6
			}
		}
	}
	toF := func(v any) (float64, bool) {
		switch t := v.(type) {
		case float64:
			return t, true
		case string:
			var f float64
			if _, err := fmt.Sscan(t, &f); err == nil {
				return f, true
			}
		}
		return 0, false
	}
	x, ok1 := toF(ai)
	y, ok2 := toF(bi)
	if ok1 && ok2 && y > x {
		return (y - x) / 1e6
	}
	return 0
}

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func maxOf(xs []float64) float64 {
	var m float64
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

// pct is the nearest-rank percentile over a copy, so callers keep their order.
func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	i := int(p * float64(len(c)-1))
	if i < 0 {
		i = 0
	}
	if i >= len(c) {
		i = len(c) - 1
	}
	return c[i]
}
