// Package model defines the persisted workspace data types and their
// validation. The JSON shapes are the native schema from IMPLEMENTATION.md
// section 4; decoding rejects unknown fields so edited files cannot lose
// data silently.
package model

// SchemaVersion is the current native collection schema. Files declaring a
// different version fail validation and are never rewritten.
const SchemaVersion = 1

// Collection is one persisted collection file: its complete item tree plus
// collection-level variables, auth and scripts.
type Collection struct {
	SchemaVersion int                    `json:"schema_version"`
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	Auth          *Auth                  `json:"auth,omitempty"`
	Scripts       *Scripts               `json:"scripts,omitempty"`
	Items         []Item                 `json:"items"`
}

// Item is one node in the collection tree: a folder or a request, tagged by
// Type, carrying a stable ID and a name unique among its siblings.
type Item struct {
	Type    string   `json:"type"` // "folder" or "request"
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Folder  *Folder  `json:"folder,omitempty"`
	Request *Request `json:"request,omitempty"`
}

// Folder holds nested items plus optional inherited auth and scripts.
type Folder struct {
	Children []Item   `json:"children"`
	Auth     *Auth    `json:"auth,omitempty"`
	Scripts  *Scripts `json:"scripts,omitempty"`
}

// Request is one saved request definition. Values may contain {{variable}}
// references; they are stored verbatim and resolved at execution time.
type Request struct {
	Method  string   `json:"method"`
	URL     string   `json:"url"`
	Query   []Entry  `json:"query,omitempty"`
	Headers []Entry  `json:"headers,omitempty"`
	Auth    *Auth    `json:"auth,omitempty"`
	Body    *Body    `json:"body,omitempty"`
	Scripts *Scripts `json:"scripts,omitempty"`
}

// Entry is one ordered {key, value, enabled} header or query entry;
// duplicate keys are preserved.
type Entry struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// Auth is a tagged authentication definition. Nil means inherit from the
// nearest ancestor; "none" disables inherited auth explicitly. Secret
// fields are stored verbatim, including {{references}}.
type Auth struct {
	Type     string `json:"type"` // "inherit", "none", "bearer" or "basic"
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Scripts are ordered pre-request and post-response script lists.
type Scripts struct {
	PreRequest   []Script `json:"pre_request,omitempty"`
	PostResponse []Script `json:"post_response,omitempty"`
}

// Script is one stored script entry with optional import provenance.
type Script struct {
	ID         string      `json:"id"`
	Source     string      `json:"source"`
	Enabled    bool        `json:"enabled"`
	Provenance *Provenance `json:"provenance,omitempty"`
}

// Provenance records where an imported script came from.
type Provenance struct {
	Source     string `json:"source,omitempty"`
	Path       string `json:"path,omitempty"`
	OriginalID string `json:"original_id,omitempty"`
}

// Body is a tagged request body. Raw and JSON bodies carry inline text or
// exactly one file reference, never both; the remaining modes are structured
// entry lists.
type Body struct {
	Type       string           `json:"type"` // "none", "raw", "json", "urlencoded" or "multipart"
	Text       string           `json:"text,omitempty"`
	File       string           `json:"file,omitempty"`
	URLEncoded []Entry          `json:"urlencoded,omitempty"`
	Multipart  []MultipartField `json:"multipart,omitempty"`
}

// MultipartField is one multipart entry: a text value or a file reference.
type MultipartField struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	File        string `json:"file,omitempty"`
	Enabled     bool   `json:"enabled"`
	ContentType string `json:"content_type,omitempty"`
	Filename    string `json:"filename,omitempty"`
}
