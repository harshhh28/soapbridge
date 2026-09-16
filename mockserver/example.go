// Package mockserver runs a fake SOAP service from the same WSDL a
// soapbridge gateway was generated from, answering every operation with
// synthetic, schema-shaped example data. It exists so a generated
// gateway can be exercised end to end — REST/MCP in, real SOAP envelopes
// out and back — without a reachable real backend.
package mockserver

import "github.com/harshhh28/soapbridge/model"

// exampleBudget bounds the total number of type nodes a single
// ExampleValue call will expand, independent of the shape of t. Without
// this, a schema with a wide, heavily-shared type graph and several
// *required* shared fields could reproduce the same combinatorial blowup
// schema.Builder was fixed for (see schema/schema.go's Builder doc) — this
// is the same class of risk, applied defensively before it bites here too,
// rather than after.
const exampleBudget = 2000

// ExampleValue generates a synthetic, JSON-ready value for t: a
// map[string]interface{} / []interface{} / string / int64 / float64 /
// bool / nil tree, shaped like real data but populated with placeholder
// values. Only required fields are populated (optional fields are simply
// omitted, the way a minimal-but-valid real payload would look), and
// arrays get exactly one example item. A self-referential or
// mutually-recursive named type stops descending on a revisit and yields
// nil there instead of recursing forever — real WSDLs have these (a
// "Category" containing "subcategories" of itself is unremarkable XSD).
func ExampleValue(t *model.Type) interface{} {
	g := &generator{seen: map[*model.Type]bool{}, budget: exampleBudget}
	return g.value(t)
}

type generator struct {
	seen   map[*model.Type]bool
	budget int
}

func (g *generator) value(t *model.Type) interface{} {
	if t == nil || g.budget <= 0 {
		return nil
	}
	g.budget--

	switch t.Kind {
	case model.KindString:
		if len(t.Enum) > 0 {
			return t.Enum[0]
		}
		return "example"
	case model.KindInteger:
		return int64(1)
	case model.KindNumber:
		return 1.0
	case model.KindBoolean:
		return true
	case model.KindDateTime:
		return "2026-01-01T00:00:00Z"
	case model.KindDate:
		return "2026-01-01"
	case model.KindArray:
		item := g.value(t.Items)
		if item == nil {
			return []interface{}{}
		}
		return []interface{}{item}
	case model.KindObject:
		if t.Name.Local != "" {
			if g.seen[t] {
				return nil // cycle: stop here rather than recurse forever
			}
			g.seen[t] = true
			defer delete(g.seen, t)
		}
		obj := map[string]interface{}{}
		for _, f := range t.Properties {
			if !f.Required || g.budget <= 0 {
				continue
			}
			obj[f.Name] = g.value(f.Type)
		}
		return obj
	default: // model.KindAny
		return nil
	}
}
