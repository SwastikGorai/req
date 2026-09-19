// Package importer converts the supported Postman export shapes into the
// native model. Parsing is side-effect free; callers persist only a fully
// normalized and validated result.
package importer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/store"
)

// Options controls a Postman import. Source is a display/provenance path and
// Name is an explicit destination identity; both are optional for callers
// using the parser directly.
type Options struct {
	Strict bool
	Name   string
	Source string
}

// Warning describes a representational loss or unsupported source feature.
// Path is a JSON-like source path (for example item[1].request.body).
type Warning struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func (w Warning) Error() string {
	if w.Path == "" {
		return w.Message
	}
	return w.Path + ": " + w.Message
}

// StrictError reports warnings that prevented a strict import from writing.
type StrictError struct {
	Warnings []Warning
}

func (e *StrictError) Error() string {
	if len(e.Warnings) == 0 {
		return "strict import rejected lossy conversion"
	}
	parts := make([]string, len(e.Warnings))
	for i, warning := range e.Warnings {
		parts[i] = warning.Error()
	}
	return "strict import rejected lossy conversion: " + strings.Join(parts, "; ")
}

// Unwrap lets CLI callers classify strict failures as import/usage errors.
func (e *StrictError) Unwrap() error { return ErrStrict }

var ErrStrict = errors.New("strict import rejected lossy conversion")

// Counts reports the normalized data in an import result.
type Counts struct {
	Collections  int `json:"collections"`
	Environments int `json:"environments"`
	Folders      int `json:"folders"`
	Requests     int `json:"requests"`
	Scripts      int `json:"scripts"`
	Variables    int `json:"variables"`
}

// CollectionResult is a parsed or persisted collection import.
type CollectionResult struct {
	Collection model.Collection
	Warnings   []Warning
	Counts     Counts
}

// EnvironmentResult is a parsed or persisted environment import.
type EnvironmentResult struct {
	Environment model.Environment
	Warnings    []Warning
	Counts      Counts
}

type postmanCollection struct {
	Info     postmanInfo       `json:"info"`
	Items    []json.RawMessage `json:"item"`
	Variable []postmanVariable `json:"variable"`
	Auth     json.RawMessage   `json:"auth"`
	Event    []postmanEvent    `json:"event"`
}

type postmanInfo struct {
	ID     string `json:"_postman_id"`
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

type postmanItem struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Disabled bool            `json:"disabled"`
	Children json.RawMessage `json:"item"`
	Request  json.RawMessage `json:"request"`
	Auth     json.RawMessage `json:"auth"`
	Event    []postmanEvent  `json:"event"`
}

type postmanRequest struct {
	Method string          `json:"method"`
	URL    json.RawMessage `json:"url"`
	Header []postmanEntry  `json:"header"`
	Auth   json.RawMessage `json:"auth"`
	Body   *postmanBody    `json:"body"`
	Event  []postmanEvent  `json:"event"`
}

type postmanEntry struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Disabled    bool            `json:"disabled"`
	Type        string          `json:"type"`
	Src         json.RawMessage `json:"src"`
	ContentType string          `json:"contentType"`
}

type postmanVariable struct {
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Enabled  *bool           `json:"enabled"`
	Disabled bool            `json:"disabled"`
	Type     string          `json:"type"`
}

type postmanBody struct {
	Mode       string          `json:"mode"`
	Raw        json.RawMessage `json:"raw"`
	URLEncoded []postmanEntry  `json:"urlencoded"`
	FormData   []postmanEntry  `json:"formdata"`
	File       json.RawMessage `json:"file"`
	Options    postmanBodyOpts `json:"options"`
}

type postmanBodyOpts struct {
	Raw struct {
		Language string `json:"language"`
	} `json:"raw"`
}

type postmanEvent struct {
	ID       string        `json:"id"`
	Listen   string        `json:"listen"`
	Disabled bool          `json:"disabled"`
	Script   postmanScript `json:"script"`
}

type postmanScript struct {
	ID   string          `json:"id"`
	Type []string        `json:"type"`
	Exec json.RawMessage `json:"exec"`
}

type postmanURL struct {
	Raw      string         `json:"raw"`
	Protocol string         `json:"protocol"`
	Host     []string       `json:"host"`
	Port     string         `json:"port"`
	Path     []string       `json:"path"`
	Query    []postmanEntry `json:"query"`
	Variable []postmanEntry `json:"variable"`
}

type parseState struct {
	opts     Options
	warnings []Warning
	counts   Counts
	usedIDs  map[string]bool
}

func newState(opts Options) *parseState {
	return &parseState{opts: opts, usedIDs: make(map[string]bool)}
}

func (s *parseState) warning(code, path, message string) {
	s.warnings = append(s.warnings, Warning{Code: code, Path: path, Message: message})
}

func (s *parseState) metadata(path, originalID string, raw []byte, unsupported, notes []string) *model.ImportMetadata {
	m := &model.ImportMetadata{Source: s.opts.Source, Path: path, OriginalID: originalID}
	if len(unsupported) > 0 {
		m.Unsupported = append([]string(nil), unsupported...)
	}
	if len(notes) > 0 {
		m.Warnings = append([]string(nil), notes...)
	}
	if len(raw) > 0 {
		m.Original = append(json.RawMessage(nil), raw...)
	}
	return m
}

// ParsePostmanCollection parses one Postman v2.1 collection without writing.
func ParsePostmanCollection(data []byte, opts Options) (CollectionResult, error) {
	var doc postmanCollection
	if err := decodeDocument(data, &doc); err != nil {
		return CollectionResult{}, fmt.Errorf("postman collection: %w", err)
	}
	if doc.Info.Name == "" {
		return CollectionResult{}, errors.New("postman collection: info.name is required")
	}
	if doc.Info.Schema == "" || !strings.Contains(strings.ToLower(doc.Info.Schema), "v2.1") {
		return CollectionResult{}, fmt.Errorf("postman collection: unsupported schema %q (want v2.1)", doc.Info.Schema)
	}
	if opts.Name != "" && !model.ValidName(opts.Name) {
		return CollectionResult{}, fmt.Errorf("postman collection: import name %q is invalid", opts.Name)
	}

	s := newState(opts)
	name := opts.Name
	if name == "" {
		name = s.name(doc.Info.Name, "collection", "info.name", nil)
	}
	c := model.Collection{
		SchemaVersion: model.SchemaVersion,
		ID:            s.id("col", doc.Info.ID, "collection"),
		Name:          name,
		Variables:     map[string]any{},
		Items:         []model.Item{},
	}
	collectionBlocked := false
	var collectionReasons []string
	if len(doc.Variable) > 0 {
		c.Variables, c.DisabledVariables = s.variables(doc.Variable, "variable")
		s.counts.Variables = len(doc.Variable)
	}
	if len(doc.Auth) > 0 && !isNull(doc.Auth) {
		auth, unsupported, _ := s.auth(doc.Auth, "auth")
		c.Auth = auth
		if len(unsupported) > 0 {
			c.Auth = &model.Auth{Type: "none"}
			collectionBlocked = true
			collectionReasons = append(collectionReasons, unsupported...)
		}
	}
	c.Scripts = s.events(doc.Event, "event")
	usedNames := make(map[string]bool, len(doc.Items))
	for i, raw := range doc.Items {
		item, err := s.item(raw, fmt.Sprintf("item[%d]", i), collectionBlocked, collectionReasons, usedNames)
		if err != nil {
			return CollectionResult{}, err
		}
		c.Items = append(c.Items, item)
	}
	c.Import = s.metadata("", doc.Info.ID, data, nil, nil)
	if collectionBlocked {
		c.Import.Unsupported = append([]string(nil), collectionReasons...)
	}
	for _, warning := range s.warnings {
		c.Import.Warnings = append(c.Import.Warnings, warning.Error())
	}
	if err := c.Validate(); err != nil {
		return CollectionResult{}, fmt.Errorf("postman collection: normalized data is invalid: %w", err)
	}
	s.counts.Collections = 1
	result := CollectionResult{Collection: c, Warnings: append([]Warning(nil), s.warnings...), Counts: s.counts}
	if opts.Strict && len(result.Warnings) > 0 {
		return result, &StrictError{Warnings: result.Warnings}
	}
	return result, nil
}

// ImportPostmanCollection parses and atomically stores a collection. A
// default name collision gets a deterministic suffix; an explicit --name
// collision fails without writing.
func ImportPostmanCollection(ctx context.Context, ws *store.Workspace, data []byte, opts Options) (CollectionResult, error) {
	result, err := ParsePostmanCollection(data, opts)
	if err != nil {
		return result, err
	}
	existing, err := ws.ListCollections(ctx)
	if err != nil {
		return CollectionResult{}, err
	}
	usedNames := make(map[string]bool, len(existing))
	usedIDs := make(map[string]bool, len(existing))
	for _, c := range existing {
		usedNames[c.Name] = true
		usedIDs[c.ID] = true
	}
	if opts.Name != "" {
		if usedNames[result.Collection.Name] {
			return CollectionResult{}, fmt.Errorf("collection %q already exists: %w", result.Collection.Name, store.ErrDuplicateName)
		}
	} else if usedNames[result.Collection.Name] {
		base := result.Collection.Name
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s (%d)", base, n)
			if !usedNames[candidate] {
				result.Collection.Name = candidate
				result.Warnings = append(result.Warnings, Warning{Code: "name-collision", Path: "info.name", Message: fmt.Sprintf("collection name %q already exists; imported as %q", base, candidate)})
				break
			}
		}
	}
	if usedIDs[result.Collection.ID] {
		result.Collection.ID = model.NewID("col")
	}
	if opts.Strict && len(result.Warnings) > 0 {
		return result, &StrictError{Warnings: result.Warnings}
	}
	if err := ws.SaveCollection(ctx, result.Collection, ""); err != nil {
		return CollectionResult{}, err
	}
	return result, nil
}

func (s *parseState) item(raw []byte, path string, inheritedBlocked bool, inheritedReasons []string, siblingNames map[string]bool) (model.Item, error) {
	var src postmanItem
	if err := json.Unmarshal(raw, &src); err != nil {
		return model.Item{}, fmt.Errorf("postman %s: invalid item: %w", path, err)
	}
	name := s.name(src.Name, "item", path+".name", siblingNames)
	id := s.id("itm", src.ID, path)
	item := model.Item{ID: id, Name: name, Disabled: src.Disabled}
	if len(src.Children) > 0 && !isNull(src.Children) && len(src.Request) > 0 && !isNull(src.Request) {
		return model.Item{}, fmt.Errorf("postman %s: item cannot contain both request and item", path)
	}
	if len(src.Children) > 0 && !isNull(src.Children) {
		var children []json.RawMessage
		if err := json.Unmarshal(src.Children, &children); err != nil {
			return model.Item{}, fmt.Errorf("postman %s.item: want an array: %w", path, err)
		}
		folder := &model.Folder{Children: []model.Item{}}
		childNames := make(map[string]bool, len(children))
		blocked := inheritedBlocked
		reasons := append([]string(nil), inheritedReasons...)
		if len(src.Auth) > 0 && !isNull(src.Auth) {
			auth, unsupported, notes := s.auth(src.Auth, path+".auth")
			folder.Auth = auth
			if auth != nil && len(unsupported) == 0 {
				blocked = false
				reasons = nil
			}
			if len(unsupported) > 0 {
				folder.Auth = &model.Auth{Type: "none"}
				blocked = true
				reasons = append(reasons, unsupported...)
			}
			if len(unsupported) > 0 || len(notes) > 0 {
				folder.Import = s.metadata(path+".auth", src.ID, src.Auth, unsupported, notes)
			}
		} else {
			folder.Auth = nil
		}
		folder.Scripts = s.events(src.Event, path+".event")
		for i, childRaw := range children {
			child, err := s.item(childRaw, fmt.Sprintf("%s.item[%d]", path, i), blocked, reasons, childNames)
			if err != nil {
				return model.Item{}, err
			}
			folder.Children = append(folder.Children, child)
		}
		item.Type, item.Folder = "folder", folder
		s.counts.Folders++
		return item, nil
	}
	if len(src.Request) == 0 || isNull(src.Request) {
		return model.Item{}, fmt.Errorf("postman %s: item must contain request or item", path)
	}
	var requestSrc postmanRequest
	if err := json.Unmarshal(src.Request, &requestSrc); err != nil {
		return model.Item{}, fmt.Errorf("postman %s.request: invalid request: %w", path, err)
	}
	request, unsupported, notes, err := s.request(requestSrc, append(append([]postmanEvent(nil), src.Event...), requestSrc.Event...), path+".request")
	if err != nil {
		return model.Item{}, err
	}
	if len(unsupported) == 0 && inheritedBlocked {
		if request.Auth != nil {
			inheritedBlocked = false
			inheritedReasons = nil
		}
	}
	if len(unsupported) == 0 && inheritedBlocked {
		unsupported = append(unsupported, inheritedReasons...)
		notes = append(notes, "inherited unsupported authentication blocks execution")
	}
	if len(unsupported) > 0 {
		request.Import = s.metadata(path+".request", src.ID, src.Request, unsupported, notes)
	}
	item.Type, item.Request = "request", &request
	s.counts.Requests++
	return item, nil
}

func (s *parseState) request(src postmanRequest, events []postmanEvent, path string) (model.Request, []string, []string, error) {
	method := strings.ToUpper(strings.TrimSpace(src.Method))
	var unsupported, notes []string
	if method == "" {
		return model.Request{}, nil, nil, fmt.Errorf("postman %s.method is required", path)
	}
	if !validMethod(method) {
		s.warning("unsupported-method", path+".method", fmt.Sprintf("method %q is not a valid HTTP token; execution is blocked", src.Method))
		unsupported = append(unsupported, fmt.Sprintf("unsupported HTTP method %q", src.Method))
		method = "GET"
	}
	rawURL, query, err := s.url(src.URL, path+".url")
	if err != nil {
		return model.Request{}, nil, nil, err
	}
	if rawURL == "" {
		return model.Request{}, nil, nil, fmt.Errorf("postman %s.url is required", path)
	}
	request := model.Request{Method: method, URL: rawURL, Query: query}
	request.Headers = s.entries(src.Header, path+".header", "header")
	if len(src.Auth) > 0 && !isNull(src.Auth) {
		auth, authUnsupported, authNotes := s.auth(src.Auth, path+".auth")
		request.Auth = auth
		unsupported = append(unsupported, authUnsupported...)
		notes = append(notes, authNotes...)
		if len(authUnsupported) > 0 {
			request.Auth = &model.Auth{Type: "none"}
		}
	}
	if src.Body != nil {
		body, bodyUnsupported, bodyNotes := s.body(*src.Body, path+".body")
		request.Body = body
		unsupported = append(unsupported, bodyUnsupported...)
		notes = append(notes, bodyNotes...)
	}
	request.Scripts = s.events(events, path+".event")
	return request, unsupported, notes, nil
}

func (s *parseState) auth(raw []byte, path string) (*model.Auth, []string, []string) {
	var src struct {
		Type   string         `json:"type"`
		Bearer []postmanEntry `json:"bearer"`
		Basic  []postmanEntry `json:"basic"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		s.warning("malformed-auth", path, "authentication object is invalid")
		return &model.Auth{Type: "none"}, []string{"malformed authentication object"}, nil
	}
	switch strings.ToLower(src.Type) {
	case "", "noauth":
		if src.Type == "" {
			s.warning("malformed-auth", path, "authentication type is missing")
			return &model.Auth{Type: "none"}, []string{"authentication type is missing"}, nil
		}
		return &model.Auth{Type: "none"}, nil, nil
	case "inherit":
		return nil, nil, nil
	case "bearer":
		token, ok := entryString(src.Bearer, "token")
		if !ok {
			s.warning("malformed-auth", path, "bearer token is missing")
			return &model.Auth{Type: "none"}, []string{"bearer token is missing"}, nil
		}
		return &model.Auth{Type: "bearer", Token: token}, nil, nil
	case "basic":
		user, userOK := entryString(src.Basic, "username")
		password, passwordOK := entryString(src.Basic, "password")
		if !userOK || !passwordOK {
			s.warning("malformed-auth", path, "basic authentication needs username and password")
			return &model.Auth{Type: "none"}, []string{"basic authentication needs username and password"}, nil
		}
		return &model.Auth{Type: "basic", Username: user, Password: password}, nil, nil
	default:
		message := fmt.Sprintf("authentication type %q is unsupported; execution is blocked", src.Type)
		s.warning("unsupported-auth", path, message)
		return &model.Auth{Type: "none"}, []string{message}, nil
	}
}

func (s *parseState) body(src postmanBody, path string) (*model.Body, []string, []string) {
	switch strings.ToLower(src.Mode) {
	case "", "none":
		if src.Mode == "" {
			return nil, nil, nil
		}
		return &model.Body{Type: "none"}, nil, nil
	case "raw":
		text, ok := rawString(src.Raw)
		if !ok {
			s.warning("malformed-body", path+".raw", "raw body is not a string; execution is blocked")
			return &model.Body{Type: "none"}, []string{"raw body is not a string"}, nil
		}
		typ := "raw"
		if strings.EqualFold(src.Options.Raw.Language, "json") {
			typ = "json"
		}
		return &model.Body{Type: typ, Text: &text}, nil, nil
	case "urlencoded":
		entries := s.entries(src.URLEncoded, path+".urlencoded", "urlencoded")
		return &model.Body{Type: "urlencoded", URLEncoded: entries}, nil, nil
	case "formdata":
		fields := make([]model.MultipartField, 0, len(src.FormData))
		for i, entry := range src.FormData {
			key := s.entryKey(entry.Key, fmt.Sprintf("%s.formdata[%d].key", path, i), "field", i)
			field := model.MultipartField{Key: key, Enabled: !entry.Disabled, ContentType: entry.ContentType}
			if strings.EqualFold(entry.Type, "file") || len(entry.Src) > 0 {
				file, ok := rawString(entry.Src)
				if !ok || file == "" {
					s.warning("unsupported-body", fmt.Sprintf("%s.formdata[%d]", path, i), "file field has no usable source; execution is blocked")
					return &model.Body{Type: "none"}, []string{"formdata file field has no usable source"}, nil
				}
				field.File, field.FileUntrusted = file, true
			} else {
				value, ok := rawString(entry.Value)
				if !ok {
					s.warning("malformed-body", fmt.Sprintf("%s.formdata[%d].value", path, i), "formdata value is not a string; execution is blocked")
					return &model.Body{Type: "none"}, []string{"formdata value is not a string"}, nil
				}
				field.Value = &value
			}
			fields = append(fields, field)
		}
		return &model.Body{Type: "multipart", Multipart: fields}, nil, nil
	case "file", "binary":
		file := src.File
		if len(file) > 0 && !isNull(file) {
			var object struct {
				Src json.RawMessage `json:"src"`
			}
			if json.Unmarshal(file, &object) == nil && len(object.Src) > 0 {
				file = object.Src
			}
		}
		pathValue, ok := rawString(file)
		if !ok || pathValue == "" {
			message := "file body has no usable source; execution is blocked"
			s.warning("unsupported-body", path, message)
			return &model.Body{Type: "none"}, []string{message}, nil
		}
		return &model.Body{Type: "raw", File: pathValue, FileUntrusted: true}, nil, nil
	default:
		message := fmt.Sprintf("body mode %q is unsupported; execution is blocked", src.Mode)
		s.warning("unsupported-body", path+".mode", message)
		return &model.Body{Type: "none"}, []string{message}, nil
	}
}

func (s *parseState) entries(entries []postmanEntry, path, kind string) []model.Entry {
	out := make([]model.Entry, 0, len(entries))
	for i, entry := range entries {
		key := s.entryKey(entry.Key, fmt.Sprintf("%s[%d].key", path, i), kind, i)
		value, ok := rawString(entry.Value)
		if !ok {
			s.warning("malformed-entry", fmt.Sprintf("%s[%d].value", path, i), "entry value is not a string; JSON value was compacted")
			value = compactRaw(entry.Value)
		}
		out = append(out, model.Entry{Key: key, Value: value, Enabled: !entry.Disabled})
	}
	return out
}

func (s *parseState) entryKey(key, path, kind string, index int) string {
	if key != "" && !strings.ContainsAny(key, "\r\n") && (kind != "header" || validMethod(key)) {
		return key
	}
	name := fmt.Sprintf("imported_%s_%d", kind, index+1)
	if key != "" {
		if kind == "header" {
			name = sanitizeHeaderKey(key)
		} else {
			name = sanitizeSimple(key)
		}
		if name == "" {
			name = fmt.Sprintf("imported_%s_%d", kind, index+1)
		}
	}
	s.warning("invalid-entry-key", path, fmt.Sprintf("invalid key %q was normalized to %q", key, name))
	return name
}

func (s *parseState) variables(entries []postmanVariable, path string) (map[string]any, map[string]bool) {
	values := make(map[string]any, len(entries))
	disabled := make(map[string]bool)
	for i, entry := range entries {
		key := entry.Key
		if !model.ValidName(key) || strings.ContainsAny(key, "\r\n") || strings.HasPrefix(key, "env:") {
			original := key
			key = sanitizeSimple(key)
			if key == "" || strings.HasPrefix(key, "env:") {
				key = fmt.Sprintf("imported_variable_%d", i+1)
			}
			s.warning("invalid-variable", fmt.Sprintf("%s[%d].key", path, i), fmt.Sprintf("invalid variable name %q was normalized to %q", original, key))
		}
		if _, exists := values[key]; exists {
			s.warning("duplicate-variable", fmt.Sprintf("%s[%d].key", path, i), fmt.Sprintf("duplicate variable %q uses the last value", key))
		}
		value := any(nil)
		if len(entry.Value) > 0 && !isNull(entry.Value) {
			if err := json.Unmarshal(entry.Value, &value); err != nil {
				s.warning("malformed-variable", fmt.Sprintf("%s[%d].value", path, i), "variable value is invalid JSON")
				value = compactRaw(entry.Value)
			}
		}
		values[key] = value
		isEnabled := entry.Enabled == nil || *entry.Enabled
		if entry.Disabled {
			isEnabled = false
		}
		if !isEnabled {
			disabled[key] = true
		} else {
			delete(disabled, key)
		}
	}
	return values, disabled
}

func (s *parseState) events(events []postmanEvent, path string) *model.Scripts {
	var pre, post []model.Script
	used := make(map[string]bool)
	for i, event := range events {
		var target *[]model.Script
		switch strings.ToLower(event.Listen) {
		case "prerequest":
			target = &pre
		case "test":
			target = &post
		default:
			s.warning("unsupported-script-event", fmt.Sprintf("%s[%d].listen", path, i), fmt.Sprintf("script listener %q was not imported", event.Listen))
			continue
		}
		source, ok := scriptSource(event.Script.Exec)
		if len(event.Script.Exec) == 0 {
			s.warning("malformed-script", fmt.Sprintf("%s[%d].script.exec", path, i), "script exec is required")
		} else if !ok {
			s.warning("malformed-script", fmt.Sprintf("%s[%d].script.exec", path, i), "script exec must be a string or string array")
			source = compactRaw(event.Script.Exec)
		}
		if len(event.Script.Type) > 0 && !hasTextScriptType(event.Script.Type) {
			s.warning("unsupported-script", fmt.Sprintf("%s[%d].script.type", path, i), fmt.Sprintf("script type %q is unsupported; source was retained", strings.Join(event.Script.Type, ", ")))
		}
		id := event.Script.ID
		if id == "" {
			id = event.ID
		}
		id = s.scriptID(id, fmt.Sprintf("%s[%d]", path, i), used)
		notes := scriptWarnings(source)
		for _, note := range notes {
			s.warning("unsupported-script", fmt.Sprintf("%s[%d].script", path, i), note)
		}
		originalScriptID := event.Script.ID
		if originalScriptID == "" {
			originalScriptID = event.ID
		}
		entry := model.Script{ID: id, Source: source, Enabled: !event.Disabled, Provenance: &model.Provenance{Source: "postman", Path: fmt.Sprintf("%s[%d]", path, i), OriginalID: originalScriptID}}
		*target = append(*target, entry)
		s.counts.Scripts++
	}
	if len(pre) == 0 && len(post) == 0 {
		return nil
	}
	return &model.Scripts{PreRequest: pre, PostResponse: post}
}

func (s *parseState) scriptID(original, path string, used map[string]bool) string {
	id := original
	if !model.ValidID(id) || used[id] {
		if original != "" {
			s.warning("invalid-script-id", path+".script.id", fmt.Sprintf("script id %q was replaced", original))
		}
		id = s.stableID("scr", path+"\x00"+original)
	}
	for n := 2; used[id]; n++ {
		id = fmt.Sprintf("%s-%d", id, n)
	}
	used[id] = true
	return id
}

func (s *parseState) url(raw json.RawMessage, path string) (string, []model.Entry, error) {
	if len(raw) == 0 || isNull(raw) {
		return "", nil, nil
	}
	if text, ok := rawString(raw); ok {
		return text, nil, nil
	}
	var src postmanURL
	if err := json.Unmarshal(raw, &src); err != nil {
		return "", nil, fmt.Errorf("postman %s: URL must be a string or object: %w", path, err)
	}
	queries := s.entries(src.Query, path+".query", "query")
	if src.Raw != "" {
		rawURL := replaceColonVariables(src.Raw, src.Variable)
		counts := rawQueryCounts(rawURL)
		filtered := queries[:0]
		for _, entry := range queries {
			key := entry.Key + "\x00" + entry.Value
			if counts[key] > 0 {
				continue
			}
			filtered = append(filtered, entry)
		}
		return rawURL, filtered, nil
	}
	if src.Protocol == "" || len(src.Host) == 0 {
		return "", nil, fmt.Errorf("postman %s: structured URL needs protocol, host or raw", path)
	}
	base := src.Protocol + "://" + strings.Join(src.Host, ".")
	if src.Port != "" {
		base += ":" + src.Port
	}
	if len(src.Path) > 0 {
		parts := make([]string, len(src.Path))
		for i, part := range src.Path {
			parts[i] = replaceColonVariables(part, src.Variable)
		}
		base += "/" + strings.Join(parts, "/")
	}
	return base, queries, nil
}

func rawQueryCounts(raw string) map[string]int {
	counts := make(map[string]int)
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery == "" {
		return counts
	}
	for _, pair := range strings.Split(u.RawQuery, "&") {
		key, value, _ := strings.Cut(pair, "=")
		key, _ = url.QueryUnescape(key)
		value, _ = url.QueryUnescape(value)
		counts[key+"\x00"+value]++
	}
	return counts
}

var colonVariable = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_-]*)([/\?#]|$)`)

func replaceColonVariables(text string, vars []postmanEntry) string {
	values := make(map[string]string, len(vars))
	for _, variable := range vars {
		if value, ok := rawString(variable.Value); ok && variable.Key != "" {
			values[variable.Key] = value
		}
	}
	return colonVariable.ReplaceAllStringFunc(text, func(match string) string {
		parts := colonVariable.FindStringSubmatch(match)
		if value, ok := values[parts[2]]; ok {
			return parts[1] + value + parts[3]
		}
		return parts[1] + "{{" + parts[2] + "}}" + parts[3]
	})
}

func (s *parseState) name(original, kind, path string, used map[string]bool) string {
	name := original
	if !model.ValidName(name) {
		name = sanitizeName(original, kind)
		s.warning("invalid-name", path, fmt.Sprintf("invalid %s name %q was normalized to %q", kind, original, name))
	}
	if used != nil && used[name] {
		base := name
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s (%d)", base, n)
			if !used[candidate] {
				name = candidate
				s.warning("duplicate-name", path, fmt.Sprintf("duplicate %s name %q was normalized to %q", kind, original, name))
				break
			}
		}
	}
	if used != nil {
		used[name] = true
	}
	return name
}

func sanitizeName(name, kind string) string {
	name = strings.TrimSpace(strings.NewReplacer("/", "_", "\\", "_").Replace(name))
	if name == "" || name == "." || name == ".." {
		name = "imported-" + kind
	}
	return name
}

func sanitizeSimple(value string) string {
	value = strings.TrimSpace(strings.NewReplacer("/", "_", "\\", "_").Replace(value))
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, value)
	return value
}

func (s *parseState) id(kind, original, path string) string {
	id := original
	if !model.ValidID(id) || s.usedIDs[id] {
		if original != "" {
			s.warning("invalid-id", path, fmt.Sprintf("%s id %q was replaced", kind, original))
		}
		id = s.stableID(kind, path+"\x00"+original)
	}
	for n := 2; s.usedIDs[id]; n++ {
		id = fmt.Sprintf("%s-%d", id, n)
	}
	s.usedIDs[id] = true
	return id
}

func (s *parseState) stableID(kind, seed string) string {
	hash := sha256.Sum256([]byte(s.opts.Source + "\x00" + seed))
	return kind + "-" + hex.EncodeToString(hash[:])[:24]
}

func entryString(entries []postmanEntry, key string) (string, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			if len(entry.Value) == 0 || isNull(entry.Value) {
				return "", false
			}
			return rawString(entry.Value)
		}
	}
	return "", false
}

func rawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || isNull(raw) {
		return "", true
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	return "", false
}

func compactRaw(raw json.RawMessage) string {
	if len(raw) == 0 || isNull(raw) {
		return ""
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) == nil {
		return compact.String()
	}
	return string(raw)
}

func scriptSource(raw json.RawMessage) (string, bool) {
	if text, ok := rawString(raw); ok {
		return text, true
	}
	var lines []string
	if len(raw) > 0 && json.Unmarshal(raw, &lines) == nil {
		return strings.Join(lines, "\n"), true
	}
	return "", false
}

func scriptWarnings(source string) []string {
	checks := []struct {
		needle string
		text   string
	}{
		{"require(", "script uses require(), which is unavailable at runtime"},
		{"setTimeout(", "script uses setTimeout(), which is unavailable at runtime"},
		{"setInterval(", "script uses setInterval(), which is unavailable at runtime"},
		{"pm.globals", "script uses pm.globals, which is outside the supported API"},
		{"pm.iterationData", "script uses pm.iterationData, which is outside the supported API"},
	}
	var notes []string
	for _, check := range checks {
		if strings.Contains(source, check.needle) {
			notes = append(notes, check.text)
		}
	}
	return notes
}

func hasTextScriptType(types []string) bool {
	for _, typ := range types {
		if strings.EqualFold(typ, "text/javascript") || strings.EqualFold(typ, "javascript") {
			return true
		}
	}
	return false
}

func sanitizeHeaderKey(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if !(r == '!' || r == '#' || r == '$' || r == '%' || r == '&' || r == '\'' || r == '*' || r == '+' || r == '-' || r == '.' || r == '^' || r == '_' || r == '`' || r == '|' || r == '~' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func decodeDocument(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err == nil {
		return errors.New("trailing data after JSON document")
	} else if err != io.EOF {
		return fmt.Errorf("trailing data after JSON document: %w", err)
	}
	return nil
}

func isNull(raw []byte) bool { return strings.EqualFold(strings.TrimSpace(string(raw)), "null") }

// ParsePostmanEnvironment parses a basic Postman environment export without
// writing it.
func ParsePostmanEnvironment(data []byte, opts Options) (EnvironmentResult, error) {
	var doc struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Values []postmanVariable `json:"values"`
	}
	if err := decodeDocument(data, &doc); err != nil {
		return EnvironmentResult{}, fmt.Errorf("postman environment: %w", err)
	}
	name := opts.Name
	if name == "" {
		name = doc.Name
	}
	if !model.ValidEnvironmentName(name) {
		clean := sanitizeSimple(name)
		clean = strings.NewReplacer(" ", "-", "/", "-", "\\", "-").Replace(clean)
		if clean == "" {
			clean = "imported"
		}
		if !model.ValidEnvironmentName(clean) {
			clean = "imported-env"
		}
		if opts.Name != "" {
			return EnvironmentResult{}, fmt.Errorf("postman environment: import name %q is invalid", opts.Name)
		}
		name = clean
	}
	s := newState(opts)
	values, disabled := s.variables(doc.Values, "values")
	e := model.Environment{SchemaVersion: model.SchemaVersion, Name: name, Variables: values, DisabledVariables: disabled}
	e.Import = s.metadata("", doc.ID, data, nil, nil)
	if doc.Name != "" && doc.Name != name {
		s.warning("invalid-environment-name", "name", fmt.Sprintf("environment name %q was normalized to %q", doc.Name, name))
	}
	for _, warning := range s.warnings {
		e.Import.Warnings = append(e.Import.Warnings, warning.Error())
	}
	if err := e.Validate(); err != nil {
		return EnvironmentResult{}, fmt.Errorf("postman environment: normalized data is invalid: %w", err)
	}
	result := EnvironmentResult{Environment: e, Warnings: append([]Warning(nil), s.warnings...), Counts: Counts{Environments: 1, Variables: len(doc.Values)}}
	if opts.Strict && len(result.Warnings) > 0 {
		return result, &StrictError{Warnings: result.Warnings}
	}
	return result, nil
}

// ImportPostmanEnvironment parses and atomically stores a basic environment.
func ImportPostmanEnvironment(ctx context.Context, ws *store.Workspace, data []byte, opts Options) (EnvironmentResult, error) {
	result, err := ParsePostmanEnvironment(data, opts)
	if err != nil {
		return result, err
	}
	existing, err := ws.ListEnvironments(ctx)
	if err != nil {
		return EnvironmentResult{}, err
	}
	used := make(map[string]bool, len(existing))
	for _, e := range existing {
		used[e.Name] = true
	}
	if opts.Name != "" {
		if usedEnvironmentName(used, result.Environment.Name) {
			return EnvironmentResult{}, fmt.Errorf("environment %q already exists: %w", result.Environment.Name, store.ErrDuplicateName)
		}
	} else if usedEnvironmentName(used, result.Environment.Name) {
		base := result.Environment.Name
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s-%d", base, n)
			if model.ValidEnvironmentName(candidate) && !usedEnvironmentName(used, candidate) {
				result.Environment.Name = candidate
				result.Warnings = append(result.Warnings, Warning{Code: "name-collision", Path: "name", Message: fmt.Sprintf("environment name %q already exists; imported as %q", base, candidate)})
				break
			}
		}
	}
	if opts.Strict && len(result.Warnings) > 0 {
		return result, &StrictError{Warnings: result.Warnings}
	}
	if err := ws.SaveEnvironment(ctx, result.Environment, ""); err != nil {
		return EnvironmentResult{}, err
	}
	return result, nil
}

func usedEnvironmentName(used map[string]bool, name string) bool {
	for existing := range used {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}

// SortedWarnings returns a stable copy for deterministic CLI output and tests.
func SortedWarnings(warnings []Warning) []Warning {
	out := append([]Warning(nil), warnings...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Code < out[j].Code
		}
		return out[i].Path < out[j].Path
	})
	return out
}
