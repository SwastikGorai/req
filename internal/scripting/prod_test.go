package scripting

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSkipRequestSignal(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	rep, err := e.Run(context.Background(), Source{
		Name: "skip.js",
		Code: `console.log("before"); pm.execution.skipRequest(); console.log("after");`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Skipped {
		t.Error("Report.Skipped = false, want true after pm.execution.skipRequest()")
	}
	if len(rep.Logs) != 1 || rep.Logs[0] != "before" {
		t.Errorf("Logs = %v, want [before] only (the script must stop at the skip)", rep.Logs)
	}
}

func TestSkipCaughtStillSkips(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	rep, err := e.Run(context.Background(), Source{
		Name: "caught.js",
		Code: `console.log("before"); try { pm.execution.skipRequest(); } catch (e) {} console.log("after");`,
	})
	if err != nil {
		t.Fatalf("Run: %v (a caught skipRequest sentinel must not fail the run)", err)
	}
	if !rep.Skipped {
		t.Error("Report.Skipped = false, want the skip flag to beat the caught sentinel")
	}
	if len(rep.Logs) != 2 || rep.Logs[0] != "before" || rep.Logs[1] != "after" {
		t.Errorf("Logs = %v, want [before after] (script continued after the catch)", rep.Logs)
	}
}

func TestSpikeGlobalsAbsent(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	rep, err := e.Run(context.Background(), Source{
		Name: "surface.js",
		Code: `console.log(JSON.stringify({
			vars: typeof vars,
			log: typeof log,
			httpGet: typeof httpGet,
			httpGetAsync: typeof httpGetAsync,
			pm: typeof pm,
			pmExecution: typeof pm.execution,
			skipRequest: typeof pm.execution.skipRequest,
			console: typeof console,
			consoleLog: typeof console.log,
		}));`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Logs) != 1 {
		t.Fatalf("Logs = %v, want one JSON line", rep.Logs)
	}
	var types map[string]string
	if err := json.Unmarshal([]byte(rep.Logs[0]), &types); err != nil {
		t.Fatalf("log line %q is not the expected JSON object: %v", rep.Logs[0], err)
	}
	for _, name := range []string{"vars", "log", "httpGet", "httpGetAsync"} {
		if got := types[name]; got != "undefined" {
			t.Errorf("typeof %s = %q, want undefined (spike globals must not leak into production)", name, got)
		}
	}
	want := map[string]string{
		"pm":          "object",
		"pmExecution": "object",
		"skipRequest": "function",
		"console":     "object",
		"consoleLog":  "function",
	}
	for name, wantType := range want {
		if got := types[name]; got != wantType {
			t.Errorf("typeof %s = %q, want %q", name, got, wantType)
		}
	}
}
