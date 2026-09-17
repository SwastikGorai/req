package scripting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dop251/goja"
)

// expectPrelude defines globalThis.__reqExpect, the factory behind pm.expect.
// It lives in JavaScript because the chain is a Proxy whose word getters (to,
// be, have, and) must return the proxy itself, not the raw target: only then
// does a later typo like .to.be.bogus hit the trap instead of resolving to
// undefined and silently passing. Terminal assertions return a fresh chain,
// so flags reset after each and .and keeps working.
const expectPrelude = `(function () {
	"use strict";
	function show(v) {
		if (v === undefined) return "undefined";
		if (v === null) return "null";
		var t = typeof v;
		if (t === "string") return JSON.stringify(v);
		if (t === "function") return "[Function]";
		if (t === "object") { try { return JSON.stringify(v); } catch (e) { return String(v); } }
		return String(v);
	}
	function deepEqual(a, b) {
		if (a === b) return true;
		if (a !== a && b !== b) return true; // NaN deep-equals NaN, as Chai does
		if (a === null || b === null || typeof a !== "object" || typeof b !== "object") return false;
		if (Array.isArray(a) !== Array.isArray(b)) return false;
		if (Array.isArray(a)) {
			if (a.length !== b.length) return false;
			for (var i = 0; i < a.length; i++) if (!deepEqual(a[i], b[i])) return false;
			return true;
		}
		var ka = Object.keys(a), kb = Object.keys(b);
		if (ka.length !== kb.length) return false;
		for (var j = 0; j < ka.length; j++) if (!deepEqual(a[ka[j]], b[ka[j]])) return false;
		return true;
	}
	function typeName(v) {
		if (v === null) return "null";
		if (Array.isArray(v)) return "array";
		return typeof v;
	}
	var flagVals = { "true": true, "false": false, "null": null, "undefined": undefined };
	var article = function (type) { return ("aeiou".indexOf(type[0]) >= 0 ? "an " : "a ") + type; };
	function makeChain(actual, negated, deep) {
		var c = {};
		var p; // the proxy wrapping this chain, defined below
		function sub(n, d) { return makeChain(actual, n, d); }
		function check(ok, pos, neg) {
			if (negated === ok) throw new Error(negated ? neg : pos);
			return sub(false, false);
		}
		function word(name) { Object.defineProperty(c, name, { get: function () { return p; } }); }
		["to", "be", "have", "and"].forEach(word);
		Object.defineProperty(c, "not", { get: function () { return sub(true, deep); } });
		Object.defineProperty(c, "deep", { get: function () { return sub(negated, true); } });
		Object.keys(flagVals).forEach(function (name) {
			Object.defineProperty(c, name, { get: function () {
				return check(actual === flagVals[name],
					"expected " + show(actual) + " to be " + name,
					"expected " + show(actual) + " not to be " + name);
			} });
		});
		c.equal = function (expected) {
			var ok = deep ? deepEqual(actual, expected) : actual === expected;
			return check(ok,
				"expected " + show(actual) + " to equal " + show(expected),
				"expected " + show(actual) + " not to equal " + show(expected));
		};
		var typeAssert = function (type) {
			return check(typeName(actual) === type,
				"expected " + show(actual) + " to be " + article(type),
				"expected " + show(actual) + " not to be " + article(type));
		};
		c.a = typeAssert;
		c.an = typeAssert;
		c.property = function (name, value) {
			var has = actual !== null && actual !== undefined &&
				(typeof actual === "object" || typeof actual === "function") &&
				name in Object(actual);
			if (has && arguments.length > 1)
				has = deep ? deepEqual(actual[name], value) : actual[name] === value;
			return check(has,
				"expected " + show(actual) + " to have a property " + show(String(name)) + (arguments.length > 1 ? " of " + show(value) : ""),
				"expected " + show(actual) + " not to have a property " + show(String(name)) + (arguments.length > 1 ? " of " + show(value) : ""));
		};
		c.include = function (val) {
			var ok = false;
			if (typeof actual === "string")
				ok = actual.indexOf(String(val)) !== -1;
			else if (Array.isArray(actual))
				ok = actual.some(function (x) { return deep ? deepEqual(x, val) : x === val; });
			else if (actual !== null && typeof actual === "object" &&
				val !== null && typeof val === "object" && !Array.isArray(val))
				ok = Object.keys(val).every(function (k) {
					return k in actual && (deep ? deepEqual(actual[k], val[k]) : actual[k] === val[k]);
				});
			return check(ok,
				"expected " + show(actual) + " to include " + show(val),
				"expected " + show(actual) + " not to include " + show(val));
		};
		c.lengthOf = function (n) {
			return check(actual !== null && actual !== undefined && actual.length === n,
				"expected " + show(actual) + " to have a length of " + show(n),
				"expected " + show(actual) + " not to have a length of " + show(n));
		};
		p = new Proxy(c, {
			get: function (target, prop) {
				if (typeof prop === "symbol") return target[prop];
				if (!(prop in target))
					throw new Error("unsupported assertion ." + String(prop) +
						" (supported: to, be, have, and, not, deep, equal, a, an, property, include, lengthOf, true, false, null, undefined)");
				return target[prop];
			}
		});
		return p;
	}
	globalThis.__reqExpect = function (actual) { return makeChain(actual, false, false); };
})();`

// installAssertions wires pm.test and pm.expect onto the unguarded pm
// target. It runs before the owner goroutine starts, so direct rt calls are
// safe; the recorded outcomes land in e.tests on the owner goroutine only.
func (e *engine) installAssertions() {
	rt := e.rt
	if _, err := rt.RunString(expectPrelude); err != nil {
		panic(fmt.Errorf("scripting: expect prelude failed: %w", err))
	}
	expectFn, ok := goja.AssertFunction(rt.GlobalObject().Get("__reqExpect"))
	if !ok {
		panic("scripting: expect prelude missing __reqExpect")
	}
	mustSet(e.pmTarget, "expect", func(call goja.FunctionCall) goja.Value {
		v, err := expectFn(goja.Undefined(), call.Argument(0))
		if err != nil {
			panic(err) // a *goja.Exception: re-panicking throws it back into JS
		}
		return v
	})
	mustSet(e.pmTarget, "test", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(rt.NewGoError(errors.New("pm.test(name, function) requires a function argument")))
		}
		res, err := fn(goja.Undefined())
		if err != nil {
			if e.skipRequested {
				panic(err) // the skip sentinel crosses pm.test untouched; the engine's skip path handles it
			}
			// A failed test is recorded, not raised: the script continues.
			e.tests = append(e.tests, TestResult{Name: name, Failed: true, Error: testErrorText(err)})
			return goja.Undefined()
		}
		if _, isPromise := res.Export().(*goja.Promise); isPromise {
			panic(rt.NewGoError(fmt.Errorf("pm.test(%q): asynchronous test callbacks are not supported; the callback returned a Promise", name)))
		}
		e.tests = append(e.tests, TestResult{Name: name})
		return goja.Undefined()
	})
}

// testErrorText reduces a thrown error to the single line recorded for a
// failed pm.test. Goja's Exception.Error() appends the short stack (" at
// file:line:col") without a newline, so the thrown Error's message property
// is preferred; the newline cut is the fallback for exotic throwables.
func testErrorText(err error) string {
	if x, ok := err.(*goja.Exception); ok {
		if o, isObj := x.Value().(*goja.Object); isObj {
			if m := o.Get("message"); m != nil && !goja.IsUndefined(m) && !goja.IsNull(m) {
				return firstLine(m.String())
			}
		}
	}
	return firstLine(err.Error())
}

// firstLine cuts at the first newline, keeping FAIL output single-line when
// a message embeds a stack.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
