package scripting

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestAssertionsContinue(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	rep := runScript(t, e, "tests.js", `
		pm.test("one", function () { pm.expect(1).to.equal(2); });
		pm.test("two", function () { pm.expect(1).to.equal(1); });
		pm.test("three", function () { throw new Error("boom"); });
		console.log("reached");
	`)
	if len(rep.Logs) != 1 || rep.Logs[0] != "reached" {
		t.Errorf("Logs = %v, want [reached] (a failed test must not stop the script)", rep.Logs)
	}
	want := []TestResult{
		{Name: "one", Failed: true, Error: "expected 1 to equal 2"},
		{Name: "two"},
		{Name: "three", Failed: true, Error: "boom"},
	}
	if !reflect.DeepEqual(rep.Tests, want) {
		t.Errorf("Tests = %+v, want %+v (errors are the bare first-line messages)", rep.Tests, want)
	}

	// The next run on the same engine starts with an empty slate.
	rep = runScript(t, e, "reset.js", `console.log("no tests here");`)
	if len(rep.Tests) != 0 {
		t.Errorf("Tests = %v, want empty after the next run", rep.Tests)
	}
}

func TestAssertionChains(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"equal strict", `pm.expect(1).to.equal(1);`},
		{"equal not", `pm.expect("1").to.not.equal(1);`}, // no loose equality
		{"a string", `pm.expect("x").to.be.a("string");`},
		{"an array", `pm.expect([]).to.be.an("array");`},
		{"a null", `pm.expect(null).to.be.a("null");`},
		{"a number", `pm.expect(5).to.be.a("number");`},
		{"property present", `pm.expect({a: 1}).to.have.property("a");`},
		{"property value", `pm.expect({a: 1}).to.have.property("a", 1);`},
		{"include string", `pm.expect("hello world").to.include("world");`},
		{"include array member", `pm.expect([1, 2, 3]).to.include(2);`},
		{"include deep member", `pm.expect([{a: 1}]).to.deep.include({a: 1});`},
		{"include object subset", `pm.expect({a: 1, b: 2}).to.include({a: 1});`},
		{"lengthOf array", `pm.expect([1, 2]).to.have.lengthOf(2);`},
		{"lengthOf string", `pm.expect("abc").to.have.lengthOf(3);`},
		{"flag true", `pm.expect(true).to.be.true;`},
		{"flag false", `pm.expect(false).to.be.false;`},
		{"flag null", `pm.expect(null).to.be.null;`},
		{"flag undefined", `pm.expect(undefined).to.be.undefined;`},
		{"chain continuation", `pm.expect(true).to.be.true.and.to.equal(true);`},
		{"not negation", `pm.expect(1).to.not.equal(2);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(nil)
			defer e.Close()
			runScript(t, e, "chain.js", tc.code)
		})
	}
}

func TestAssertionFailures(t *testing.T) {
	for _, tc := range []struct{ name, snippet, want string }{
		{"equal is strict", `pm.expect(1).to.equal("1");`, `expected 1 to equal "1"`},
		{"deep equal order matters", `pm.expect([1, 2]).to.deep.equal([2, 1]);`, "expected [1,2] to equal [2,1]"},
		{"deep equal extra keys matter", `pm.expect({a: 1, b: 2}).to.deep.equal({a: 1});`, `to equal {"a":1}`},
		{"flag true", `pm.expect(1).to.be.true;`, "expected 1 to be true"},
		{"flag null", `pm.expect(undefined).to.be.null;`, "expected undefined to be null"},
		{"lengthOf", `pm.expect([1]).to.have.lengthOf(2);`, "expected [1] to have a length of 2"},
		{"include string", `pm.expect("abc").to.include("xyz");`, `expected "abc" to include "xyz"`},
		{"unsupported word", `pm.expect(5).to.exist;`, "unsupported assertion .exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(nil)
			defer e.Close()

			// A failed test is recorded, not raised: the run still succeeds.
			rep := runScript(t, e, "fail.js", `pm.test("t", function () { `+tc.snippet+` });`)
			if len(rep.Tests) != 1 || !rep.Tests[0].Failed {
				t.Fatalf("Tests = %+v, want exactly one failed test", rep.Tests)
			}
			if rep.Tests[0].Name != "t" || !strings.Contains(rep.Tests[0].Error, tc.want) {
				t.Errorf("Tests[0] = %+v, want name t and an error mentioning %q", rep.Tests[0], tc.want)
			}
		})
	}

	// A bare failing assertion outside pm.test still fails the script.
	e := NewEngine(nil)
	defer e.Close()
	mustFail(t, e, "bare.js", `pm.expect(1).to.equal(2);`, "expected 1 to equal 2")
}

func TestAssertionDeepEqual(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	rep := runScript(t, e, "deep.js", `
		pm.test("nested equal", function () {
			pm.expect({a: {b: [1, {c: 2}]}}).to.deep.equal({a: {b: [1, {c: 2}]}});
		});
		pm.test("key order irrelevant", function () {
			pm.expect({a: 1, b: 2}).to.deep.equal({b: 2, a: 1});
		});
		pm.test("NaN deep equals NaN", function () {
			pm.expect(NaN).to.deep.equal(NaN);
		});
		pm.test("differs one level deep", function () {
			pm.expect({a: {b: 1}}).to.deep.equal({a: {b: 2}});
		});
		pm.test("NaN strict equal fails", function () {
			pm.expect(NaN).to.equal(NaN);
		});
	`)
	want := []TestResult{
		{Name: "nested equal"},
		{Name: "key order irrelevant"},
		{Name: "NaN deep equals NaN"},
		{Name: "differs one level deep", Failed: true, Error: `expected {"a":{"b":1}} to equal {"a":{"b":2}}`},
		{Name: "NaN strict equal fails", Failed: true, Error: "expected NaN to equal NaN"},
	}
	if !reflect.DeepEqual(rep.Tests, want) {
		t.Errorf("Tests = %+v, want %+v", rep.Tests, want)
	}
}

func TestAsyncTestRejected(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	mustFail(t, e, "async.js",
		`pm.test("late", async function () { pm.expect(1).to.equal(1); });`,
		"asynchronous test callbacks are not supported")
	mustFail(t, e, "promise.js",
		`pm.test("p", function () { return Promise.resolve(5); });`,
		"the callback returned a Promise")
}

func TestResponseStatusAssertion(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.SetPostRequest(&ExecRequest{Method: "GET", URL: "http://x"},
		ResponseData{Code: 200, Status: "OK", Headers: http.Header{}})

	runScript(t, e, "status.js", `pm.response.to.have.status(200);`)
	mustFail(t, e, "status.js", `pm.response.to.have.status(404);`,
		"expected response to have status code 404 but got 200")

	// Wrapped in pm.test the same failure is recorded instead of raised.
	rep := runScript(t, e, "wrapped.js", `pm.test("status", function () { pm.response.to.have.status(404); });`)
	if len(rep.Tests) != 1 || !rep.Tests[0].Failed ||
		rep.Tests[0].Error != "expected response to have status code 404 but got 200" {
		t.Errorf("Tests = %+v, want one failed status test with the bare message", rep.Tests)
	}

	mustFail(t, e, "bogus.js", `pm.response.to.be.success;`,
		`unsupported pm API "pm.response.to.be"`, "supported: have")
}
