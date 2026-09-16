package schema

import (
	"os"
	"testing"
	"time"

	"github.com/harshhh28/soapbridge/model"
	"github.com/harshhh28/soapbridge/wsdl"
)

func mustParse(t *testing.T, path string) *model.Definition {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := wsdl.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return res.Definition
}

func op(t *testing.T, def *model.Definition, name string) model.Operation {
	t.Helper()
	for _, o := range def.Operations {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("operation %q not found", name)
	return model.Operation{}
}

func TestFromType_Scalars(t *testing.T) {
	cases := []struct {
		kind       model.Kind
		wantType   string
		wantFormat string
	}{
		{model.KindString, "string", ""},
		{model.KindInteger, "integer", ""},
		{model.KindNumber, "number", ""},
		{model.KindBoolean, "boolean", ""},
		{model.KindDateTime, "string", "date-time"},
		{model.KindDate, "string", "date"},
	}
	for _, c := range cases {
		s := FromType(&model.Type{Kind: c.kind})
		if s.Type != c.wantType || s.Format != c.wantFormat {
			t.Errorf("kind %v: got type=%q format=%q, want type=%q format=%q", c.kind, s.Type, s.Format, c.wantType, c.wantFormat)
		}
	}
}

func TestFromType_Enum(t *testing.T) {
	s := FromType(&model.Type{Kind: model.KindString, Enum: []string{"LOW", "MEDIUM", "HIGH"}})
	if s.Type != "string" || len(s.Enum) != 3 || s.Enum[2] != "HIGH" {
		t.Errorf("got %+v", s)
	}
}

func TestFromType_ObjectRequiredAndNullable(t *testing.T) {
	// Anonymous (unnamed) object type: inlined directly, same as any
	// nested anonymous type would be.
	typ := &model.Type{
		Kind: model.KindObject,
		Properties: []model.Field{
			{Name: "subject", Type: &model.Type{Kind: model.KindString}, Required: true},
			{Name: "assignee", Type: &model.Type{Kind: model.KindString}, Required: false, Nillable: true},
		},
	}
	s := FromType(typ)
	if s.Type != "object" {
		t.Fatalf("Type = %q, want object", s.Type)
	}
	if len(s.Required) != 1 || s.Required[0] != "subject" {
		t.Errorf("Required = %v, want [subject]", s.Required)
	}
	if s.Properties["assignee"].Nullable != true {
		t.Errorf("assignee.Nullable = false, want true")
	}
}

func TestFromType_Array(t *testing.T) {
	typ := &model.Type{Kind: model.KindArray, Items: &model.Type{Kind: model.KindInteger}}
	s := FromType(typ)
	if s.Type != "array" || s.Items == nil || s.Items.Type != "integer" {
		t.Errorf("got %+v", s)
	}
}

// TestFromType_NamedRoot checks that even though the root type itself is
// named (operation input/output types always are — see FromType's doc),
// FromType inlines it at the top level rather than returning a bare
// top-level "$ref". A nested occurrence of a *different* named type still
// gets promoted normally.
func TestFromType_NamedRoot(t *testing.T) {
	addr := &model.Type{Name: model.QName{Space: "urn:x", Local: "Address"}, Kind: model.KindObject}
	addr.Properties = []model.Field{{Name: "city", Type: &model.Type{Kind: model.KindString}, Required: true}}

	root := &model.Type{Name: model.QName{Space: "urn:x", Local: "Customer"}, Kind: model.KindObject}
	root.Properties = []model.Field{
		{Name: "name", Type: &model.Type{Kind: model.KindString}, Required: true},
		{Name: "billTo", Type: addr, Required: true},
		{Name: "shipTo", Type: addr, Required: true}, // same *model.Type as billTo
	}

	s := FromType(root)
	if s.Ref != "" {
		t.Fatalf("root should be inlined, got a bare $ref: %+v", s)
	}
	if s.Type != "object" || s.Properties["name"].Type != "string" {
		t.Fatalf("root not inlined correctly: %+v", s)
	}

	billTo := s.Properties["billTo"]
	shipTo := s.Properties["shipTo"]
	if billTo.Ref == "" || shipTo.Ref == "" {
		t.Fatalf("nested named type should be a $ref, got billTo=%+v shipTo=%+v", billTo, shipTo)
	}
	if billTo.Ref != shipTo.Ref {
		t.Errorf("the same Address type reached via two fields should produce the same $ref: %q vs %q", billTo.Ref, shipTo.Ref)
	}
	// Two defs, not one: Address (shared by billTo/shipTo) plus Customer
	// itself — the root's own name is always registered too, so that if
	// anything inside it ever refers back to the root, that reference
	// resolves to a $ref instead of re-inlining (see the cycle test below).
	if len(s.Defs) != 2 {
		t.Fatalf("expected two definitions (Address, Customer), got %d: %v", len(s.Defs), s.Defs)
	}
	if def, ok := s.Defs["Address"]; !ok || def.Properties["city"].Type != "string" {
		t.Fatalf("Defs[\"Address\"] = %+v", s.Defs["Address"])
	}
}

// TestFromType_SharedTypeIsNotDuplicated is the regression test for the
// real bug this rework fixes: a wide, non-cyclic DAG of shared named types
// (routine in large enterprise WSDLs — see testdata/wsdl's Transportation
// Manager-shaped fixtures) must cost work linear in the number of
// *distinct* types, not the number of paths to them. Ten fields all
// pointing at the same 50-field leaf type used to inline the leaf ten
// times over; it must now appear once.
func TestFromType_SharedTypeIsNotDuplicated(t *testing.T) {
	leaf := &model.Type{Name: model.QName{Space: "urn:x", Local: "Leaf"}, Kind: model.KindObject}
	for i := 0; i < 50; i++ {
		leaf.Properties = append(leaf.Properties, model.Field{Name: itoa(i), Type: &model.Type{Kind: model.KindString}})
	}

	root := &model.Type{Kind: model.KindObject}
	for i := 0; i < 10; i++ {
		root.Properties = append(root.Properties, model.Field{Name: "f" + itoa(i), Type: leaf})
	}

	s := FromType(root)
	if len(s.Properties) != 10 {
		t.Fatalf("expected 10 fields, got %d", len(s.Properties))
	}
	for _, f := range s.Properties {
		if f.Ref == "" {
			t.Fatalf("expected every field to be a $ref to the shared Leaf type, got %+v", f)
		}
	}
	if len(s.Defs) != 1 {
		t.Fatalf("expected exactly one definition for the shared Leaf type, got %d", len(s.Defs))
	}
}

// TestFromType_SelfReferentialCycle ensures a complexType that (directly or
// transitively) contains itself is represented as a genuine, finite $ref
// cycle rather than causing infinite recursion.
func TestFromType_SelfReferentialCycle(t *testing.T) {
	category := &model.Type{Name: model.QName{Space: "urn:x", Local: "Category"}, Kind: model.KindObject}
	category.Properties = []model.Field{
		{Name: "name", Type: &model.Type{Kind: model.KindString}, Required: true},
		{Name: "children", Type: &model.Type{Kind: model.KindArray, Items: category}},
	}

	done := make(chan *JSONSchema, 1)
	go func() { done <- FromType(category) }()
	var s *JSONSchema
	select {
	case s = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("FromType did not return within 2s (possible infinite recursion)")
	}
	if s == nil || s.Type != "object" {
		t.Fatalf("got %+v", s)
	}
	children := s.Properties["children"]
	if children.Type != "array" || children.Items.Ref == "" {
		t.Fatalf("children.items should be a $ref back to Category, got %+v", children)
	}
	if _, ok := s.Defs["Category"]; !ok {
		t.Fatalf("expected Category itself in $defs (the root was inlined but must still be registered, since it refers to itself): %v", s.Defs)
	}
}

// TestRealFixtures round-trips the schema mapper over the three real-world
// WSDLs used by the parser tests, as a smoke check that nothing panics and
// that shapes look right end to end (namespaces -> types -> JSON Schema).
func TestRealFixtures(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/numberconversion.wsdl")
	o := op(t, def, "NumberToWords")
	in := FromType(o.Input.Type)
	if in.Type != "object" || in.Properties["ubiNum"].Type != "integer" {
		t.Errorf("NumberToWords input schema = %+v", in)
	}
	if got := len(in.Required); got != 1 {
		t.Errorf("required = %v, want [ubiNum]", in.Required)
	}

	def = mustParse(t, "../testdata/wsdl/countryinfo.wsdl")
	o = op(t, def, "FullCountryInfoAllCountries")
	out := FromType(o.Output.Type)
	result := out.Properties["FullCountryInfoAllCountriesResult"]
	if result.Ref == "" {
		t.Fatalf("expected the named ArrayOftCountryInfo type to be a $ref, got %+v", result)
	}
	def1, ok := out.Defs[refBaseName(result.Ref)]
	if !ok {
		t.Fatalf("ArrayOftCountryInfo definition missing from $defs: %v", out.Defs)
	}
	arr := def1.Properties["tCountryInfo"]
	if arr.Type != "array" || arr.Items.Ref == "" {
		t.Fatalf("expected an array of $ref'd tCountryInfo objects, got %+v", arr)
	}
	countryInfoDef, ok := out.Defs[refBaseName(arr.Items.Ref)]
	if !ok || countryInfoDef.Properties["sISOCode"] == nil {
		t.Errorf("expected sISOCode property on the tCountryInfo definition, got %+v", countryInfoDef)
	}
}

func TestEnumSampleFixture(t *testing.T) {
	def := mustParse(t, "../testdata/wsdl/enumsample.wsdl")
	o := op(t, def, "CreateTicket")
	in := FromType(o.Input.Type)
	ticketRef := in.Properties["ticket"]
	if ticketRef.Ref == "" {
		t.Fatalf("ticket = %+v, want a $ref to the named Ticket type", ticketRef)
	}
	ticket, ok := in.Defs[refBaseName(ticketRef.Ref)]
	if !ok {
		t.Fatalf("Ticket definition missing from $defs: %v", in.Defs)
	}

	priority := ticket.Properties["priority"]
	if len(priority.Enum) != 3 {
		t.Errorf("priority.Enum = %v", priority.Enum)
	}
	assignee := ticket.Properties["assignee"]
	if !assignee.Nullable {
		t.Errorf("assignee should be nullable")
	}
	for _, r := range ticket.Required {
		if r == "assignee" {
			t.Errorf("assignee should not be in required")
		}
	}
	dueAt := ticket.Properties["dueAt"]
	if dueAt.Type != "string" || dueAt.Format != "date-time" {
		t.Errorf("dueAt = %+v", dueAt)
	}
}

func refBaseName(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '/' {
			return ref[i+1:]
		}
	}
	return ref
}
