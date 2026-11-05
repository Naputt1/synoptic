package main

import (
	"os"
	"path/filepath"
	"testing"
)

const statusFixture = `package fixture

type Ctx struct{}

func (c *Ctx) JSON(code int, v any) {}
func (c *Ctx) Status(code int)       {}
func (c *Ctx) Next()                 {}

func allowed() bool { return true }

func writeErr(c *Ctx, code int) {
	c.JSON(code, nil)
}

func writeBoth(c *Ctx) {
	if allowed() {
		c.JSON(400, nil)
	} else {
		c.JSON(500, nil)
	}
}

func writeDeep1(c *Ctx) { c.JSON(409, nil) }
func writeDeep2(c *Ctx) { writeDeep1(c) }
func writeDeep3(c *Ctx) { writeDeep2(c) }
func writeDeep4(c *Ctx) { writeDeep3(c) }

func writeRecursive(c *Ctx, n int) {
	if n > 0 {
		writeRecursive(c, n-1)
	}
	c.JSON(418, nil)
}

func writeLocal(c *Ctx) {
	code := 422
	c.JSON(code, nil)
}

func handleSingle(c *Ctx) {
	c.JSON(201, nil)
}

func handleBoth(c *Ctx) {
	if !allowed() {
		writeBoth(c)
		return
	}
	c.JSON(200, nil)
}

func handleParam(c *Ctx) {
	if !allowed() {
		writeErr(c, 403)
		return
	}
	c.JSON(200, nil)
}

func handleLocal(c *Ctx) {
	if !allowed() {
		writeLocal(c)
		return
	}
	c.JSON(200, nil)
}

func handleDeep(c *Ctx) {
	if !allowed() {
		writeDeep4(c)
		return
	}
	c.JSON(200, nil)
}

func handleRecursive(c *Ctx) {
	if !allowed() {
		writeRecursive(c, 3)
		return
	}
	c.JSON(200, nil)
}

func handleNested(c *Ctx) {
	if !allowed() {
		if allowed() {
			c.JSON(400, nil)
		} else {
			writeErr(c, 500)
			return
		}
	}
	c.JSON(200, nil)
}

func pickStatus() (int, error) { return 201, nil }

func handleReturnedStatus(c *Ctx) {
	status, err := pickStatus()
	if err != nil {
		writeErr(c, 500)
		return
	}
	c.JSON(status, nil)
}

func handleSwitch(c *Ctx) {
	status, err := pickStatus()
	if err != nil {
		switch status {
		case 401:
			writeErr(c, 401)
		default:
			writeErr(c, 503)
		}
		return
	}
	c.JSON(status, nil)
}
`

func loadStatusFixture(t *testing.T) *Module {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.go"), []byte(statusFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := loadModule(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func walkSteps(list []Step, visit func(Step)) {
	for _, s := range list {
		visit(s)
		walkSteps(s.Body, visit)
		walkSteps(s.Else, visit)
	}
}

func exitCodes(steps []Step) map[string]bool {
	out := map[string]bool{}
	walkSteps(steps, func(s Step) {
		if s.Kind != "exit" {
			return
		}
		for _, code := range stepCodes(s) {
			out[code] = true
		}
	})
	return out
}

func retCodes(steps []Step) map[string]bool {
	out := map[string]bool{}
	walkSteps(steps, func(s Step) {
		if s.Kind != "ret" {
			return
		}
		for _, code := range stepCodes(s) {
			out[code] = true
		}
	})
	return out
}

func TestStatusesFollowHelpers(t *testing.T) {
	m := loadStatusFixture(t)

	cases := []struct {
		name  string
		codes map[string]bool
		want  []string
	}{
		{"handleBoth", exitCodes(buildFlow(m.PKG["example.com/fixture"]["handleBoth"])), []string{"400", "500"}},
		{"handleParam", exitCodes(buildFlow(m.PKG["example.com/fixture"]["handleParam"])), []string{"403"}},
		{"handleLocal", exitCodes(buildFlow(m.PKG["example.com/fixture"]["handleLocal"])), []string{"422"}},
		{"handleDeep", exitCodes(buildFlow(m.PKG["example.com/fixture"]["handleDeep"])), []string{"409"}},
		{"handleRecursive", exitCodes(buildFlow(m.PKG["example.com/fixture"]["handleRecursive"])), []string{"418"}},
		{"handleSingle", retCodes(buildFlow(m.PKG["example.com/fixture"]["handleSingle"])), []string{"201"}},
		{"handleNested", retCodes(buildFlow(m.PKG["example.com/fixture"]["handleNested"])), []string{"200", "400", "500"}},
		{"handleReturnedStatus", retCodes(buildFlow(m.PKG["example.com/fixture"]["handleReturnedStatus"])), []string{"201"}},
		{"handleSwitch", retCodes(buildFlow(m.PKG["example.com/fixture"]["handleSwitch"])), []string{"201", "401", "503"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.codes) != len(tc.want) {
				t.Fatalf("codes = %v, want %v", tc.codes, tc.want)
			}
			for _, code := range tc.want {
				if !tc.codes[code] {
					t.Fatalf("codes = %v, want %v", tc.codes, tc.want)
				}
			}
		})
	}
}
