package mcpgen

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Overrides lets the person running a gateway state, per operation, what the
// name heuristic can't know. An override always wins over the guess, in both
// directions: it can promote an unannotated operation (CountryName ->
// readOnly) and demote a wrongly guessed one (GetNextTicket -> mutating).
//
// In config.yaml:
//
//	annotations:
//	  readOnly:    [CountryName, CurrencyName]  # no side effects
//	  mutating:    [GetNextTicket, CreateUser]  # changes state, not destructive
//	  destructive: [PurgeCache]                 # deletes or irreversibly changes data
type Overrides struct {
	ReadOnly    []string `yaml:"readOnly,omitempty"`
	Mutating    []string `yaml:"mutating,omitempty"`
	Destructive []string `yaml:"destructive,omitempty"`
}

// IsZero reports whether no override is set (used by yaml omitempty).
func (o Overrides) IsZero() bool {
	return len(o.ReadOnly) == 0 && len(o.Mutating) == 0 && len(o.Destructive) == 0
}

// ParseOverrides extracts the top-level "annotations:" section from a
// gateway config.yaml. Other keys are ignored, but unknown keys *inside*
// annotations are an error, so a typo like "readonly:" can't silently leave
// an operation unannotated.
func ParseOverrides(configYAML []byte) (Overrides, error) {
	var top struct {
		Annotations yaml.Node `yaml:"annotations"`
	}
	if err := yaml.Unmarshal(configYAML, &top); err != nil {
		return Overrides{}, fmt.Errorf("parsing config: %w", err)
	}
	if top.Annotations.Kind == 0 {
		return Overrides{}, nil // no annotations section
	}
	section, err := yaml.Marshal(&top.Annotations)
	if err != nil {
		return Overrides{}, fmt.Errorf("reading annotations: %w", err)
	}
	var o Overrides
	dec := yaml.NewDecoder(bytes.NewReader(section))
	dec.KnownFields(true)
	if err := dec.Decode(&o); err != nil {
		return Overrides{}, fmt.Errorf("invalid annotations section (valid keys: readOnly, mutating, destructive): %w", err)
	}
	return o, nil
}

// ApplyOverrides applies o to tools in place. It returns an error if an
// operation is listed under more than one category (ambiguous), and
// warnings for names that match no operation (almost always a typo).
func ApplyOverrides(tools []Tool, o Overrides) (warnings []string, err error) {
	byName := make(map[string]*Tool, len(tools))
	for i := range tools {
		byName[tools[i].Name] = &tools[i]
	}

	category := map[string]string{}
	type group struct {
		label string
		names []string
		set   func(*Tool)
	}
	t, f := true, false
	groups := []group{
		{"readOnly", o.ReadOnly, func(tl *Tool) {
			tl.Annotations = &Annotations{ReadOnlyHint: &t, IdempotentHint: &t, OpenWorldHint: &t}
		}},
		{"mutating", o.Mutating, func(tl *Tool) {
			tl.Annotations = &Annotations{ReadOnlyHint: &f, DestructiveHint: &f, OpenWorldHint: &t}
		}},
		{"destructive", o.Destructive, func(tl *Tool) {
			tl.Annotations = &Annotations{ReadOnlyHint: &f, DestructiveHint: &t, OpenWorldHint: &t}
		}},
	}

	for _, g := range groups {
		for _, name := range g.names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if prev, dup := category[name]; dup && prev != g.label {
				return nil, fmt.Errorf("annotations: operation %q is listed under both %s and %s", name, prev, g.label)
			}
			category[name] = g.label
		}
	}

	for _, g := range groups {
		for _, name := range g.names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			tool, ok := byName[name]
			if !ok {
				warnings = append(warnings, fmt.Sprintf("annotations.%s lists %q, which matches no operation (names are case-sensitive)", g.label, name))
				continue
			}
			g.set(tool)
		}
	}
	return warnings, nil
}

// Unannotated returns the names of tools with no read-only hint, i.e. ones
// neither the name heuristic nor an override could classify.
func Unannotated(tools []Tool) []string {
	var names []string
	for _, t := range tools {
		if t.Annotations == nil || t.Annotations.ReadOnlyHint == nil {
			names = append(names, t.Name)
		}
	}
	sort.Strings(names)
	return names
}

// Abbreviate returns names, or its first max entries followed by "and N more"
// when it is longer, so a message about a large WSDL stays readable.
func Abbreviate(names []string, max int) []string {
	if max <= 0 || len(names) <= max {
		return names
	}
	out := append([]string{}, names[:max]...)
	return append(out, fmt.Sprintf("and %d more", len(names)-max))
}
