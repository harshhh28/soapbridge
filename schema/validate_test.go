package schema

import (
	"testing"

	"github.com/harshhh28/soapbridge/model"
)

func ticketType() *model.Type {
	return &model.Type{
		Kind: model.KindObject,
		Properties: []model.Field{
			{Name: "subject", Type: &model.Type{Kind: model.KindString}, Required: true},
			{Name: "priority", Type: &model.Type{Kind: model.KindString, Enum: []string{"LOW", "MEDIUM", "HIGH"}}, Required: true},
			{Name: "assignee", Type: &model.Type{Kind: model.KindString}, Required: false, Nillable: true},
		},
	}
}

func TestValidate(t *testing.T) {
	s := FromType(ticketType())

	cases := []struct {
		name    string
		value   interface{}
		wantErr bool
	}{
		{"valid", map[string]interface{}{"subject": "hi", "priority": "LOW"}, false},
		{"missing required", map[string]interface{}{"priority": "LOW"}, true},
		{"bad enum", map[string]interface{}{"subject": "hi", "priority": "URGENT"}, true},
		{"wrong type", map[string]interface{}{"subject": 5, "priority": "LOW"}, true},
		{"nullable ok", map[string]interface{}{"subject": "hi", "priority": "LOW", "assignee": nil}, false},
		{"not an object", "oops", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := Validate(s, c.value)
			if c.wantErr && len(errs) == 0 {
				t.Errorf("expected validation errors, got none")
			}
			if !c.wantErr && len(errs) != 0 {
				t.Errorf("unexpected validation errors: %v", errs)
			}
		})
	}
}

// TestValidate_ResolvesRef checks Validate follows a "$ref" into the root
// schema's "$defs" — exercised here through a *named* nested type, which is
// exactly what a real request body validates against once a field's type
// is shared/named (see TestFromType_NamedRoot).
func TestValidate_ResolvesRef(t *testing.T) {
	address := &model.Type{Name: model.QName{Space: "urn:x", Local: "Address"}, Kind: model.KindObject}
	address.Properties = []model.Field{{Name: "city", Type: &model.Type{Kind: model.KindString}, Required: true}}
	order := &model.Type{
		Kind: model.KindObject,
		Properties: []model.Field{
			{Name: "shipTo", Type: address, Required: true},
		},
	}
	s := FromType(order)
	if s.Properties["shipTo"].Ref == "" {
		t.Fatalf("test setup: expected shipTo to be a $ref, got %+v", s.Properties["shipTo"])
	}

	valid := map[string]interface{}{"shipTo": map[string]interface{}{"city": "Springfield"}}
	if errs := Validate(s, valid); len(errs) != 0 {
		t.Errorf("unexpected errors validating through a $ref: %v", errs)
	}

	invalid := map[string]interface{}{"shipTo": map[string]interface{}{}}
	errs := Validate(s, invalid)
	if len(errs) == 0 {
		t.Fatalf("expected a missing-required-field error resolved through shipTo's $ref, got none")
	}
}
