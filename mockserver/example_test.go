package mockserver

import (
	"testing"
	"time"

	"github.com/harshhh28/soapbridge/model"
)

func TestExampleValue_Scalars(t *testing.T) {
	cases := []struct {
		kind model.Kind
		want interface{}
	}{
		{model.KindString, "example"},
		{model.KindInteger, int64(1)},
		{model.KindNumber, 1.0},
		{model.KindBoolean, true},
	}
	for _, c := range cases {
		got := ExampleValue(&model.Type{Kind: c.kind})
		if got != c.want {
			t.Errorf("kind %v: got %#v, want %#v", c.kind, got, c.want)
		}
	}
}

func TestExampleValue_EnumPicksFirstValue(t *testing.T) {
	got := ExampleValue(&model.Type{Kind: model.KindString, Enum: []string{"MEDIUM", "LOW", "HIGH"}})
	if got != "MEDIUM" {
		t.Errorf("got %v, want the first enum value", got)
	}
}

func TestExampleValue_ObjectOnlyPopulatesRequired(t *testing.T) {
	typ := &model.Type{
		Kind: model.KindObject,
		Properties: []model.Field{
			{Name: "subject", Type: &model.Type{Kind: model.KindString}, Required: true},
			{Name: "notes", Type: &model.Type{Kind: model.KindString}, Required: false},
		},
	}
	got, ok := ExampleValue(typ).(map[string]interface{})
	if !ok {
		t.Fatalf("got %#v, want a map", got)
	}
	if _, present := got["subject"]; !present {
		t.Errorf("required field \"subject\" missing: %v", got)
	}
	if _, present := got["notes"]; present {
		t.Errorf("optional field \"notes\" should be omitted: %v", got)
	}
}

func TestExampleValue_Array(t *testing.T) {
	typ := &model.Type{Kind: model.KindArray, Items: &model.Type{Kind: model.KindInteger}}
	got, ok := ExampleValue(typ).([]interface{})
	if !ok || len(got) != 1 || got[0] != int64(1) {
		t.Errorf("got %#v, want a single-item array", got)
	}
}

// TestExampleValue_SelfReferentialCycle mirrors
// schema.TestFromType_SelfReferentialCycle: a required field pointing back
// at its own type (or, transitively, at an ancestor) must terminate rather
// than recurse forever.
func TestExampleValue_SelfReferentialCycle(t *testing.T) {
	category := &model.Type{Name: model.QName{Space: "urn:x", Local: "Category"}, Kind: model.KindObject}
	category.Properties = []model.Field{
		{Name: "name", Type: &model.Type{Kind: model.KindString}, Required: true},
		{Name: "parent", Type: category, Required: true}, // required self-reference
	}

	done := make(chan interface{}, 1)
	go func() { done <- ExampleValue(category) }()
	select {
	case v := <-done:
		obj, ok := v.(map[string]interface{})
		if !ok || obj["name"] != "example" {
			t.Fatalf("got %#v", v)
		}
		if obj["parent"] != nil {
			t.Errorf("the cyclic-back reference should terminate as nil, got %#v", obj["parent"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExampleValue did not return within 2s (possible infinite recursion)")
	}
}

// TestExampleValue_WideRequiredSharingIsBounded is the same class of
// regression test as schema.TestFromType_SharedTypeIsNotDuplicated: many
// *required* fields all sharing one large type must not blow up example
// generation the way it once did for schema.FromType (see schema/schema.go
// and mockserver/example.go's budget for why this matters here too).
func TestExampleValue_WideRequiredSharingIsBounded(t *testing.T) {
	leaf := &model.Type{Name: model.QName{Space: "urn:x", Local: "Leaf"}, Kind: model.KindObject}
	for i := 0; i < 20; i++ {
		leaf.Properties = append(leaf.Properties, model.Field{Name: "f" + itoaForTest(i), Type: &model.Type{Kind: model.KindString}, Required: true})
	}
	root := &model.Type{Kind: model.KindObject}
	for i := 0; i < 20; i++ {
		root.Properties = append(root.Properties, model.Field{Name: "g" + itoaForTest(i), Type: leaf, Required: true})
	}

	done := make(chan interface{}, 1)
	go func() { done <- ExampleValue(root) }()
	select {
	case v := <-done:
		obj := v.(map[string]interface{})
		if len(obj) != 20 {
			t.Errorf("got %d top-level fields, want 20", len(obj))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExampleValue did not return within 2s on a wide shared-type graph")
	}
}

func itoaForTest(n int) string {
	return string(rune('a' + n))
}
