package scripting

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// spikeEngine pairs the production engine with the spike-only variable store
// so the spike tests can inspect it; the production engine carries no spike
// state.
type spikeEngine struct {
	*engine
	vars *varStore
}

// newSpikeEngine constructs an engine and installs the spike surface: vars
// (Go-backed store), log, httpGet (callback) and httpGetAsync (Promise).
// None of this is the production pm API.
func newSpikeEngine() *spikeEngine {
	e := NewEngine(nil)
	vars := newVarStore()
	installSpikeBindings(e, vars)
	return &spikeEngine{engine: e, vars: vars}
}

// installSpikeBindings registers the spike surface on the engine's runtime.
// It exists only in tests; the production surface is installed by
// (*engine).installBindings.
func installSpikeBindings(e *engine, vars *varStore) {
	rt := e.rt

	varsObj := rt.NewObject()
	mustSet := func(name string, value interface{}) {
		if err := varsObj.Set(name, value); err != nil {
			panic(err)
		}
	}
	mustSet("get", func(key string) interface{} { return vars.get(key) })
	mustSet("set", func(key string, value interface{}) { vars.set(key, value) })
	if err := rt.Set("vars", varsObj); err != nil {
		panic(err)
	}

	if err := rt.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		e.logs = append(e.logs, strings.Join(parts, " "))
		return goja.Undefined()
	}); err != nil {
		panic(err)
	}

	if err := rt.Set("httpGet", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		cb, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(rt.NewTypeError("httpGet(url, callback) requires a string URL and a callback function"))
		}
		e.startHTTP(url, func(res httpResult) {
			_, err := cb(goja.Undefined(), res.errValue(rt), res.value(rt))
			if err != nil && e.runErr == nil {
				e.runErr = fmt.Errorf("httpGet callback: %w", err)
			}
		})
		return goja.Undefined()
	}); err != nil {
		panic(err)
	}

	if err := rt.Set("httpGetAsync", func(call goja.FunctionCall) goja.Value {
		url := call.Argument(0).String()
		promise, resolve, reject := rt.NewPromise()
		e.startHTTP(url, func(res httpResult) {
			if res.err != nil {
				if err := reject(res.err.Error()); err != nil {
					e.setRunErr(err)
				}
				return
			}
			if err := resolve(res.value(rt)); err != nil {
				e.setRunErr(err)
			}
		})
		return rt.ToValue(promise)
	}); err != nil {
		panic(err)
	}
}

// varStore is the spike's JS-to-Go variable adapter.
type varStore struct {
	mu sync.Mutex
	m  map[string]interface{}
}

func newVarStore() *varStore {
	return &varStore{m: make(map[string]interface{})}
}

func (s *varStore) get(key string) interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[key]
}

func (s *varStore) set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
}
