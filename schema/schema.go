// Package schema converts the internal model (model package) produced by
// the WSDL parser into JSON Schema (draft 2020-12-compatible, but kept to
// the widely-supported subset also used by OpenAPI 3.0). This is a pure
// function of the model — gateway, mcpgen and openapigen all call it
// rather than deriving JSON shapes themselves, so the three surfaces can't
// drift apart.
package schema

import "github.com/harshhh28/soapbridge/model"

// JSONSchema is a minimal JSON Schema / OpenAPI Schema Object
// representation, sufficient for the constructs the mapper emits.
type JSONSchema struct {
	Ref         string                 `json:"$ref,omitempty"`
	Defs        map[string]*JSONSchema `json:"$defs,omitempty"` // only ever set on a document root
	Type        string                 `json:"type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Description string                 `json:"description,omitempty"`
	Properties  map[string]*JSONSchema `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Items       *JSONSchema            `json:"items,omitempty"`
	Enum        []string               `json:"enum,omitempty"`
	Nullable    bool                   `json:"nullable,omitempty"`
}

// FromType converts a resolved *model.Type into a single, self-contained
// JSON Schema document, suitable on its own for REST request validation or
// an MCP tool's inputSchema.
//
// The root is always inlined (type/properties directly at the top level),
// even though operation input/output types are themselves always named
// (they're global WSDL elements) and would otherwise be promoted to a bare
// top-level "$ref" like any other named type — that's the right shape for
// a schema nested inside a bigger document (see openapigen, which wants
// exactly that), but a validation schema or an MCP inputSchema is used on
// its own, where callers reasonably expect "type":"object" up front. Only
// types reached *from* the root recurse through the normal $ref/$defs
// promotion — including, for a self-referential root type, the root's own
// name, so a cycle back to the root still terminates in a $ref rather than
// re-inlining (and, worse, re-registering) it.
func FromType(t *model.Type) *JSONSchema {
	b := NewBuilder()
	root := b.Schema(t)
	if root.Ref != "" {
		if def, ok := b.defs[refName(root.Ref, b.refPath)]; ok {
			cp := *def // shallow copy: root gets its own Defs without aliasing the def a self-reference $refs back to
			root = &cp
		}
	}
	if defs := b.Defs(); len(defs) > 0 {
		root.Defs = defs
	}
	return root
}

func refName(ref, prefix string) string {
	return ref[len(prefix):]
}

// Builder accumulates named-type definitions across one or more calls to
// Schema, emitting a "$ref" instead of re-inlining a type it has already
// converted (or is in the middle of converting, which is how a genuinely
// self-referential XSD type becomes a valid, finite $ref cycle instead of
// infinite recursion).
//
// This exists because real-world WSDLs reuse named types heavily — the
// same "AddressType" might be reachable from dozens of different fields.
// Naively inlining every occurrence turns a handful of distinct types into
// an object tree whose size is the *product* of the branching factor at
// each level: one enterprise WSDL in this repo's test fixtures has a
// single operation whose naively-inlined input schema did not finish
// building after a minute and 2GB of RAM. Promoting named types to a
// shared "$defs" map, referenced everywhere else by "$ref", makes the
// work (and the output size) linear in the number of *distinct* named
// types instead — openapigen shares one Builder across every operation
// for exactly this reason, so a type reused across many operations is
// still defined exactly once in the resulting document.
type Builder struct {
	defs     map[string]*JSONSchema
	names    map[*model.Type]string
	used     map[string]bool
	refPath  string // e.g. "#/$defs/" or "#/components/schemas/"
}

// NewBuilder returns a Builder that promotes named types into a local
// "$defs" map (refs of the form "#/$defs/Name") — the right choice for a
// single self-contained document like one operation's request schema.
func NewBuilder() *Builder {
	return &Builder{defs: map[string]*JSONSchema{}, names: map[*model.Type]string{}, used: map[string]bool{}, refPath: "#/$defs/"}
}

// NewComponentsBuilder returns a Builder that refs named types as
// "#/components/schemas/Name" — for assembling one OpenAPI document out of
// many operations that should share a single schema registry rather than
// each getting their own copy of every shared type.
func NewComponentsBuilder() *Builder {
	return &Builder{defs: map[string]*JSONSchema{}, names: map[*model.Type]string{}, used: map[string]bool{}, refPath: "#/components/schemas/"}
}

// Defs returns the accumulated named-type definitions, keyed by the name
// used in their "$ref"s.
func (b *Builder) Defs() map[string]*JSONSchema { return b.defs }

// Schema converts t. A named object type is promoted into the builder's
// shared definitions (on first encounter) and this call returns only a
// "$ref" to it; every later call (for the same or a different root type)
// that reaches the same *model.Type again gets the same "$ref" instead of
// re-expanding it.
func (b *Builder) Schema(t *model.Type) *JSONSchema {
	if t == nil {
		return &JSONSchema{}
	}
	if t.Kind == model.KindObject && t.Name.Local != "" {
		return &JSONSchema{Ref: b.refFor(t)}
	}
	return b.inline(t)
}

// refFor returns the (possibly newly-assigned) "$ref" string for a named
// type, building its definition into b.defs on first encounter. The name
// is registered *before* recursing into the type's fields, so a field that
// refers back to t (directly or transitively) finds the name already
// assigned and just emits the same $ref — that's what turns a cycle into
// a finite structure instead of infinite recursion.
func (b *Builder) refFor(t *model.Type) string {
	if name, ok := b.names[t]; ok {
		return b.refPath + name
	}
	name := b.assignName(t.Name.Local)
	b.names[t] = name
	b.defs[name] = b.inline(t) // t.Name.Local != "" here, so this can't recurse back through refFor for t itself without hitting the names[] cache above
	return b.refPath + name
}

func (b *Builder) assignName(base string) string {
	if base == "" {
		base = "Type"
	}
	if !b.used[base] {
		b.used[base] = true
		return base
	}
	for i := 2; ; i++ {
		candidate := base + "_" + itoa(i)
		if !b.used[candidate] {
			b.used[candidate] = true
			return candidate
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := [20]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

// inline builds s's schema body directly, without the $ref promotion
// Schema/refFor apply to named object types — used both for anonymous
// types and, via refFor, to build a named type's one definition.
func (b *Builder) inline(t *model.Type) *JSONSchema {
	s := &JSONSchema{Description: t.Doc}
	switch t.Kind {
	case model.KindString:
		s.Type = "string"
		s.Enum = t.Enum
	case model.KindInteger:
		s.Type = "integer"
	case model.KindNumber:
		s.Type = "number"
	case model.KindBoolean:
		s.Type = "boolean"
	case model.KindDateTime:
		s.Type = "string"
		s.Format = "date-time"
	case model.KindDate:
		s.Type = "string"
		s.Format = "date"
	case model.KindArray:
		s.Type = "array"
		s.Items = b.Schema(t.Items)
	case model.KindObject:
		s.Type = "object"
		if len(t.Properties) > 0 {
			s.Properties = make(map[string]*JSONSchema, len(t.Properties))
			for _, f := range t.Properties {
				fs := b.Schema(f.Type)
				if f.Nillable {
					fs.Nullable = true
				}
				s.Properties[f.Name] = fs
				if f.Required {
					s.Required = append(s.Required, f.Name)
				}
			}
		}
	default: // model.KindAny
		// No "type" constrains nothing — deliberately permissive for
		// constructs we couldn't resolve (see wsdl.Result.Warnings).
	}
	return s
}
