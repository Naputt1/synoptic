package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func redactedFixture() JData {
	return JData{
		Repo: "fixture",
		Mod:  "example.com/redact",
		Flows: map[string]JFunc{
			"example.com/redact|handler": {
				Name: "handler",
				File: "main.go",
				Line: 10,
				Doc:  "handler serves the thing.",
				Src:  "func handler() {\n\tsecret()\n}",
				Steps: []Step{
					{
						Kind: "call",
						Text: "secret()",
						Code: "raw-secret-call",
						Codes: []string{
							"raw-secret-call",
						},
						Line: 11,
						Body: []Step{
							{
								Kind: "if",
								Text: "if ok",
								Code: "raw-if-body",
								Codes: []string{
									"raw-if-body",
								},
								Line: 12,
								Body: []Step{
									{
										Kind:  "call",
										Text:  "deep()",
										Code:  "raw-deep-call",
										Codes: []string{"raw-deep-call"},
										Line:  13,
									},
								},
								Else: []Step{
									{
										Kind:  "ret",
										Text:  "return",
										Code:  "return",
										Codes: []string{"return"},
										Line:  15,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func assertNoSource(t *testing.T, steps []Step) {
	t.Helper()
	for _, s := range steps {
		if s.Code != "" || len(s.Codes) != 0 {
			t.Fatalf("step %q still carries code at line %d: %q %v", s.Text, s.Line, s.Code, s.Codes)
		}
		assertNoSource(t, s.Body)
		assertNoSource(t, s.Else)
	}
}

func TestRedactClearsSourceAtEveryDepth(t *testing.T) {
	d := redactedFixture()
	redact(&d)
	for key, f := range d.Flows {
		if f.Src != "" {
			t.Fatalf("flow %q still carries source", key)
		}
		assertNoSource(t, f.Steps)
	}
	blob, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"raw-secret-call", "raw-if-body", "raw-deep-call", "raw-return", "func handler()"} {
		if strings.Contains(string(blob), leak) {
			t.Fatalf("json leaks %q: %s", leak, blob)
		}
	}
}

func TestRedactKeepsDocsAndLines(t *testing.T) {
	d := redactedFixture()
	redact(&d)
	f := d.Flows["example.com/redact|handler"]
	if f.Doc != "handler serves the thing." || f.Line != 10 || f.File != "main.go" {
		t.Fatalf("metadata lost: %+v", f)
	}
	if len(f.Steps) != 1 || f.Steps[0].Line != 11 {
		t.Fatalf("line numbers lost: %+v", f.Steps)
	}
	if f.Steps[0].Body[0].Else[0].Line != 15 {
		t.Fatalf("nested line numbers lost: %+v", f.Steps[0].Body[0].Else)
	}
	if !d.Redact {
		t.Fatal("data does not say it was redacted")
	}
}
