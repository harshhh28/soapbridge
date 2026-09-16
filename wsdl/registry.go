package wsdl

import "github.com/harshhh28/soapbridge/model"

// typeRegistry holds *model.Type pointers keyed by qualified name, for both
// named XSD types and global elements. Entries are created empty with
// stub() during an initial pass over the whole document, then populated in
// place with fill() during a second pass. Because every reference to a
// not-yet-resolved name captures the same pointer, forward references and
// reference cycles between complex types resolve correctly without special
// cycle-detection logic.
type typeRegistry struct {
	entries map[string]*model.Type
}

func newTypeRegistry() *typeRegistry {
	return &typeRegistry{entries: map[string]*model.Type{}}
}

func (r *typeRegistry) stub(q model.QName) *model.Type {
	key := q.String()
	if t, ok := r.entries[key]; ok {
		return t
	}
	t := &model.Type{Name: q}
	r.entries[key] = t
	return t
}

func (r *typeRegistry) lookup(q model.QName) (*model.Type, bool) {
	t, ok := r.entries[q.String()]
	return t, ok
}

// fill populates the stub for q in place with the fields of resolved,
// preserving the stub's pointer identity and Name.
func (r *typeRegistry) fill(q model.QName, resolved *model.Type) {
	stub := r.stub(q)
	name := stub.Name
	*stub = *resolved
	stub.Name = name
}
