package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// This file exports the route surface the page shows. Both documents are
// evidence-only: every path, parameter, body field, response and stream comes
// from JData, which the analyzer read from source. Nothing here guesses a
// schema or a status that the reader did not prove.

type oaDocument struct {
	OpenAPI   string                    `json:"openapi"`
	Info      oaInfo                    `json:"info"`
	Paths     map[string]map[string]any `json:"paths"`
	XSynoptic oaTop                     `json:"x-synoptic"`
}

type oaInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type oaTop struct {
	Repo        string      `json:"repo"`
	GeneratedBy string      `json:"generatedBy"`
	Change      *JChange    `json:"change,omitempty"`
	Diag        oaDiag      `json:"diag"`
	Skipped     []oaSkipped `json:"skipped,omitempty"`
}

type oaDiag struct {
	Blind   int `json:"blind"`
	Unknown int `json:"unknown"`
	Skipped int `json:"skipped"`
}

type oaSkipped struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type oaOperation struct {
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationID string                `json:"operationId"`
	Parameters  []oaParameter         `json:"parameters,omitempty"`
	RequestBody *oaRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]oaResponse `json:"responses"`
	XSynoptic   oaOperationExtensions `json:"x-synoptic"`
}

type oaParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required,omitempty"`
	Schema   map[string]any `json:"schema"`
}

type oaRequestBody struct {
	Content map[string]oaMedia `json:"content"`
}

type oaResponse struct {
	Description string             `json:"description"`
	Content     map[string]oaMedia `json:"content,omitempty"`
}

type oaMedia struct {
	Schema map[string]any `json:"schema"`
}

type oaOperationExtensions struct {
	Handler            oaHandler `json:"handler"`
	Middleware         []string  `json:"middleware,omitempty"`
	Service            string    `json:"service,omitempty"`
	Auth               bool      `json:"auth,omitempty"`
	UnprovenPathParams bool      `json:"unprovenPathParams,omitempty"`
	Stream             *oaStream `json:"stream,omitempty"`
}

type oaHandler struct {
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

type oaStream struct {
	Kind   string   `json:"kind"`
	Events []string `json:"events,omitempty"`
}

// buildOpenAPI renders the proven route surface as OpenAPI 3.1 JSON. The
// method-agnostic ANY routes stay evidence: when a path also has a concrete
// method they are recorded under x-synoptic.skipped; when it has none they are
// kept under the path item's x-synoptic-any extension rather than assigned a
// method the source never named.
func buildOpenAPI(d JData) ([]byte, error) {
	doc := oaDocument{
		OpenAPI: "3.1.0",
		Info: oaInfo{
			Title:   d.Repo + " API",
			Version: version,
			Description: "Generated from source by synoptic. Evidence-based: paths, parameters, " +
				"request fields, responses and streams are read from the code; nothing is guessed.",
		},
		Paths: map[string]map[string]any{},
		XSynoptic: oaTop{
			Repo:        d.Repo,
			GeneratedBy: "synoptic",
			Change:      d.Change,
			Diag: oaDiag{
				Blind:   d.Diag.Unresolved,
				Unknown: d.Diag.Unknown,
				Skipped: d.Diag.Skipped,
			},
		},
	}

	groups := map[string]*oaPathGroup{}
	for _, r := range d.Routes {
		p, wildcard := openAPIPath(r.P)
		g := groups[p]
		if g == nil {
			g = &oaPathGroup{path: p}
			groups[p] = g
		}
		g.wildcard = g.wildcard || wildcard
		g.routes = append(g.routes, r)
	}

	paths := make([]string, 0, len(groups))
	for p := range groups {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	ids := map[string]int{}
	for _, p := range paths {
		g := groups[p]
		item := map[string]any{}
		concrete := false
		for _, r := range g.routes {
			if r.M != "ANY" {
				concrete = true
				break
			}
		}
		for _, r := range g.routes {
			if r.M == "ANY" {
				if concrete {
					doc.XSynoptic.Skipped = append(doc.XSynoptic.Skipped, oaSkipped{
						Method: r.M, Path: r.P, Reason: "method-agnostic route alongside a concrete method",
					})
					continue
				}
				item["x-synoptic-any"] = buildOperation(r, p, ids)
				continue
			}
			verb := strings.ToLower(r.M)
			if _, ok := item[verb]; ok {
				doc.XSynoptic.Skipped = append(doc.XSynoptic.Skipped, oaSkipped{
					Method: r.M, Path: r.P, Reason: "duplicate method and path",
				})
				continue
			}
			item[verb] = buildOperation(r, p, ids)
		}
		if g.wildcard {
			item["x-synoptic"] = map[string]any{"wildcard": true}
		}
		doc.Paths[p] = item
	}

	return json.MarshalIndent(doc, "", "  ")
}

type oaPathGroup struct {
	path     string
	wildcard bool
	routes   []JRoute
}

func buildOperation(r JRoute, path string, ids map[string]int) *oaOperation {
	handler, at := handlerStage(r)
	summary, description := splitDoc(handler.Doc)
	op := &oaOperation{
		Summary:     summary,
		Description: description,
		OperationID: nextOperationID(ids, r.M, path),
		Responses:   map[string]oaResponse{},
		XSynoptic: oaOperationExtensions{
			Handler: oaHandler{Name: handler.Name, File: handler.File, Line: handler.Line},
			Service: r.Svc,
		},
	}
	if op.XSynoptic.Handler.Name == "" {
		op.Summary, op.Description = "", ""
	}
	if op.XSynoptic.Handler.File == "" {
		op.XSynoptic.Handler.File = r.F
	}
	if op.XSynoptic.Handler.Line == 0 {
		op.XSynoptic.Handler.Line = r.L
	}
	for i, st := range r.Chain {
		if i >= at || st.Role != "middleware" {
			continue
		}
		op.XSynoptic.Middleware = append(op.XSynoptic.Middleware, st.Name)
	}

	proven := map[string]bool{}
	if doc := r.Doc; doc != nil {
		for _, f := range doc.Path {
			proven[f.N] = true
			op.Parameters = append(op.Parameters, oaParameter{
				Name: f.N, In: "path", Required: true, Schema: schemaOf(f.T),
			})
		}
		for _, q := range doc.Query {
			op.Parameters = append(op.Parameters, oaParameter{
				Name: q.N, In: "query", Required: q.Req, Schema: schemaOf(q.T),
			})
		}
		if len(doc.Body) > 0 || doc.BodyType != "" {
			props := map[string]any{}
			for _, f := range doc.Body {
				props[f.N] = schemaOf(f.T)
			}
			schema := map[string]any{"type": "object"}
			if len(props) > 0 {
				schema["properties"] = props
			}
			op.RequestBody = &oaRequestBody{Content: map[string]oaMedia{
				"application/json": {Schema: schema},
			}}
		}
		for _, resp := range doc.Resp {
			code := resp.Code
			if code == "" || code == "—" {
				if _, ok := op.Responses["default"]; !ok {
					op.Responses["default"] = oaResponse{Description: "no status observed in the handler"}
				}
				continue
			}
			out := oaResponse{Description: "status observed in the handler"}
			if len(resp.Keys) > 0 {
				props := map[string]any{}
				for _, k := range resp.Keys {
					props[k] = map[string]any{}
				}
				out.Content = map[string]oaMedia{
					"application/json": {Schema: map[string]any{"type": "object", "properties": props}},
				}
			}
			op.Responses[code] = out
		}
		op.XSynoptic.Auth = doc.Auth
	}
	for _, name := range pathParams(path) {
		if !proven[name] {
			op.XSynoptic.UnprovenPathParams = true
			break
		}
	}
	if len(op.Responses) == 0 {
		op.Responses["default"] = oaResponse{Description: "no status observed in the handler"}
	}

	streamKind := ""
	var streamEvents []string
	if r.St != nil {
		streamKind = r.St.K
		streamEvents = r.St.Events
	} else if r.Pr != "" {
		streamKind = r.Pr
	}
	if streamKind != "" {
		op.XSynoptic.Stream = &oaStream{Kind: streamKind, Events: append([]string(nil), streamEvents...)}
		switch streamKind {
		case "ws":
			if _, ok := op.Responses["101"]; !ok {
				op.Responses["101"] = oaResponse{Description: "switching protocols"}
			}
		case "sse":
			media := oaMedia{Schema: map[string]any{"type": "string"}}
			if resp, ok := op.Responses["200"]; ok {
				if resp.Content == nil {
					resp.Content = map[string]oaMedia{}
				}
				if _, exists := resp.Content["text/event-stream"]; !exists {
					resp.Content["text/event-stream"] = media
					op.Responses["200"] = resp
				}
			} else {
				op.Responses["200"] = oaResponse{
					Description: "server-sent events",
					Content:     map[string]oaMedia{"text/event-stream": media},
				}
			}
		}
	}
	return op
}

// handlerStage is the last stage the route names as its handler. A chain
// without a role falls back to its last stage, which is how the page reads it.
func handlerStage(r JRoute) (JStage, int) {
	for i := len(r.Chain) - 1; i >= 0; i-- {
		if r.Chain[i].Role == "handler" {
			return r.Chain[i], i
		}
	}
	lastStage := lastStage(r)
	return lastStage, len(r.Chain) - 1
}

func splitDoc(doc string) (summary, description string) {
	doc = strings.TrimSpace(doc)
	if i := strings.Index(doc, " — "); i >= 0 {
		return strings.TrimSpace(doc[:i]), strings.TrimSpace(doc[i+len(" — "):])
	}
	return doc, ""
}

func schemaOf(t string) map[string]any {
	return map[string]any{"type": schemaType(t)}
}

// schemaType names the JSON shape of a proven Go type. Anything the reader
// cannot place stays a string rather than becoming an object it never saw.
func schemaType(t string) string {
	t = strings.TrimSpace(strings.TrimLeft(t, "*"))
	switch {
	case strings.HasPrefix(t, "int"), strings.HasPrefix(t, "uint"):
		return "integer"
	case strings.HasPrefix(t, "float"):
		return "number"
	case t == "bool":
		return "boolean"
	}
	return "string"
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

func pathParams(path string) []string {
	var out []string
	for _, m := range pathParamRe.FindAllStringSubmatch(path, -1) {
		out = append(out, m[1])
	}
	return out
}

// openAPIPath turns the registrations synoptic reads into OpenAPI templates:
// :id and {id} both become {id}, and *rest becomes {rest} with a wildcard mark.
func openAPIPath(path string) (string, bool) {
	segs := strings.Split(path, "/")
	wildcard := false
	for i, seg := range segs {
		switch {
		case strings.HasPrefix(seg, ":") && len(seg) > 1:
			segs[i] = "{" + seg[1:] + "}"
		case strings.HasPrefix(seg, "*") && len(seg) > 1:
			segs[i] = "{" + seg[1:] + "}"
			wildcard = true
		}
	}
	return strings.Join(segs, "/"), wildcard
}

func nextOperationID(seen map[string]int, method, path string) string {
	base := operationID(method, path)
	seen[base]++
	if n := seen[base]; n > 1 {
		return fmt.Sprintf("%s_%d", base, n)
	}
	return base
}

func operationID(method, path string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(method + path) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	id := strings.Trim(b.String(), "_")
	if id == "" {
		return strings.ToLower(method)
	}
	return id
}

// writeExport writes the document to path, or to stdout when path is "-".
func writeExport(path string, blob []byte) error {
	if path == "-" {
		_, err := os.Stdout.Write(blob)
		return err
	}
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, blob, 0o644)
}

// buildMarkdown renders the route inventory, and the -base change view when the
// run produced one, as a PR-comment sized document. It carries no timestamp so
// two runs over the same tree are byte-identical.
func buildMarkdown(d JData) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — route inventory\n\n", d.Repo)
	fmt.Fprintf(&b, "%d routes · generated from source; evidence only, unresolved calls are not guessed\n\n", len(d.Routes))

	if c := d.Change; c != nil {
		fmt.Fprintf(&b, "## Changes vs %s\n\n", c.Ref)
		fmt.Fprintf(&b, "%d added · %d removed · %d modified · %d files touched\n\n",
			len(c.Added), len(c.Removed), len(c.Modified), c.Files)
		if len(c.Added) == 0 && len(c.Removed) == 0 && len(c.Modified) == 0 {
			b.WriteString("No route changes.\n\n")
		}
		if len(c.Added) > 0 {
			b.WriteString("### Added\n\n")
			writeChangeTable(&b, c.Added, "new")
		}
		if len(c.Removed) > 0 {
			b.WriteString("### Removed\n\n")
			writeChangeTable(&b, c.Removed, "removed")
		}
		if len(c.Modified) > 0 {
			b.WriteString("### Modified\n\n")
			b.WriteString("| Method | Path | Why |\n| --- | --- | --- |\n")
			for _, m := range c.Modified {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", cell(m.M), cell(m.P), cell(strings.Join(m.Why, ", ")))
			}
			b.WriteString("\n")
		}
		b.WriteString("<details><summary>All routes</summary>\n\n")
	}

	b.WriteString("## Routes\n\n")
	b.WriteString("| Method | Path | Handler | Middleware | Stream | Service |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, r := range d.Routes {
		handler, at := handlerStage(r)
		var mw []string
		for i, st := range r.Chain {
			if i >= at || st.Role != "middleware" {
				continue
			}
			mw = append(mw, st.Name)
		}
		stream := ""
		if r.St != nil {
			stream = r.St.K
		} else {
			stream = r.Pr
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			cell(r.M), cell(r.P), cell(handler.Name), cell(strings.Join(mw, ", ")), cell(stream), cell(r.Svc))
	}
	b.WriteString("\n")
	if d.Change != nil {
		b.WriteString("</details>\n\n")
	}
	fmt.Fprintf(&b, "Blind spots: %d · Unknown calls: %d · Skipped files: %d\n",
		d.Diag.Unresolved, d.Diag.Unknown, d.Diag.Skipped)
	return []byte(b.String())
}

func writeChangeTable(b *strings.Builder, rows []JRouteKey, why string) {
	b.WriteString("| Method | Path | Why |\n| --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s |\n", cell(r.M), cell(r.P), cell(why))
	}
	b.WriteString("\n")
}

// cell keeps a table row intact when a proven name contains a pipe or a
// newline.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\r", " ", "\n", " ").Replace(s)
}
