package wsdl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/harshhh28/soapbridge/model"
)

// resolver turns a parsed rawDefinitions tree into a model.Definition. See
// registry.go for how forward/cyclic type references are handled, and
// scope.go for how QName attribute values are resolved to namespace URIs.
type resolver struct {
	types    *typeRegistry // named complexType/simpleType, keyed by QName
	elements *typeRegistry // global <xsd:element>, keyed by QName
	warnings []string
}

func (r *resolver) warnf(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func resolve(def *rawDefinitions) (*model.Definition, []string, error) {
	r := &resolver{types: newTypeRegistry(), elements: newTypeRegistry()}
	rootScope := newScope(nil, def.Attrs)

	// Pass 1: stub every named type and global element so references in
	// any order, including forward references and cycles, resolve to a
	// stable pointer.
	for _, t := range def.Types {
		for _, sch := range t.Schemas {
			tns := sch.TargetNamespace
			for _, imp := range sch.Imports {
				r.warnf("xsd:import (namespace=%q) not supported in v1; types from imported schemas are unavailable", imp.Namespace)
			}
			for _, inc := range sch.Includes {
				r.warnf("xsd:include (schemaLocation=%q) not supported in v1; included types are unavailable", inc.SchemaLocation)
			}
			for _, ct := range sch.ComplexTypes {
				r.types.stub(model.QName{Space: tns, Local: ct.Name})
			}
			for _, st := range sch.SimpleTypes {
				r.types.stub(model.QName{Space: tns, Local: st.Name})
			}
			for _, el := range sch.Elements {
				r.elements.stub(model.QName{Space: tns, Local: el.Name})
			}
		}
	}

	// Pass 2: fill named types (may reference each other and global
	// elements via ref=, both already stubbed).
	for _, t := range def.Types {
		for _, sch := range t.Schemas {
			schemaScope := newScope(rootScope, sch.Attrs)
			tns := sch.TargetNamespace
			for _, ct := range sch.ComplexTypes {
				resolved := r.resolveComplexType(ct, schemaScope, tns)
				r.types.fill(model.QName{Space: tns, Local: ct.Name}, resolved)
			}
			for _, st := range sch.SimpleTypes {
				resolved := r.resolveSimpleType(st)
				r.types.fill(model.QName{Space: tns, Local: st.Name}, resolved)
			}
		}
	}

	// Pass 3: fill global elements (their body is either an inline type
	// or a type= reference into the now-fully-resolved type registry).
	for _, t := range def.Types {
		for _, sch := range t.Schemas {
			schemaScope := newScope(rootScope, sch.Attrs)
			tns := sch.TargetNamespace
			for _, el := range sch.Elements {
				resolved := r.resolveElementBody(el, schemaScope, tns)
				r.elements.fill(model.QName{Space: tns, Local: el.Name}, resolved)
			}
		}
	}

	// Messages: name -> resolved Part (element/type + concrete Type).
	messages := map[string]*model.Part{}
	for _, m := range def.Messages {
		if len(m.Parts) == 0 {
			messages[m.Name] = &model.Part{Name: m.Name, Type: &model.Type{Kind: model.KindObject}}
			continue
		}
		p := m.Parts[0]
		if len(m.Parts) > 1 {
			r.warnf("message %q has %d parts; only the first (%q) is used (RPC-style multi-part messages are a v1 non-goal)", m.Name, len(m.Parts), p.Name)
		}
		part := &model.Part{Name: p.Name}
		switch {
		case p.Element != "":
			space, local := rootScope.resolveQName(p.Element)
			q := model.QName{Space: space, Local: local}
			part.Element = q
			if t, ok := r.elements.lookup(q); ok {
				part.Type = t
			} else {
				r.warnf("message %q part %q references unknown element %q", m.Name, p.Name, p.Element)
				part.Type = &model.Type{Kind: model.KindAny}
			}
		case p.Type != "":
			space, local := rootScope.resolveQName(p.Type)
			part.Type = r.resolveTypeRef(space, local)
		default:
			part.Type = &model.Type{Kind: model.KindAny}
		}
		messages[m.Name] = part
	}

	lookupMessage := func(ref string) (*model.Part, string) {
		_, local := rootScope.resolveQName(ref)
		if p, ok := messages[local]; ok {
			return p, local
		}
		return nil, local
	}

	// portType -> operation name -> {input, output, doc}
	type ptOp struct {
		doc            string
		input, output  *model.Part
	}
	portTypeOps := map[string]map[string]ptOp{}
	for _, pt := range def.PortTypes {
		ops := map[string]ptOp{}
		for _, op := range pt.Operations {
			var in, out *model.Part
			if op.Input != nil {
				p, name := lookupMessage(op.Input.Message)
				if p == nil {
					r.warnf("operation %q input references unknown message %q", op.Name, name)
				}
				in = p
			}
			if op.Output != nil {
				p, name := lookupMessage(op.Output.Message)
				if p == nil {
					r.warnf("operation %q output references unknown message %q", op.Name, name)
				}
				out = p
			}
			ops[op.Name] = ptOp{doc: strings.TrimSpace(op.Documentation), input: in, output: out}
		}
		portTypeOps[pt.Name] = ops
	}

	// Bindings: pick a primary binding's operation metadata (soapAction,
	// style) per operation name, preferring whichever binding is used by
	// a SOAP 1.1 port; also detect WS-Security.
	type bindOpMeta struct {
		soapAction, style string
	}
	bindings := []model.Binding{}
	bindOpsByBinding := map[string]map[string]bindOpMeta{}
	bindingPortType := map[string]string{}
	for _, b := range def.Bindings {
		_, ptLocal := rootScope.resolveQName(b.Type)
		bindingPortType[b.Name] = ptLocal

		var version, transport, defaultStyle string
		switch {
		case b.SOAPBind11 != nil:
			version, transport, defaultStyle = "1.1", b.SOAPBind11.Transport, b.SOAPBind11.Style
		case b.SOAPBind12 != nil:
			version, transport, defaultStyle = "1.2", b.SOAPBind12.Transport, b.SOAPBind12.Style
		default:
			r.warnf("binding %q has no soap:binding/soap12:binding element; skipping (HTTP/MIME bindings are unsupported)", b.Name)
			continue
		}
		if defaultStyle == "" {
			defaultStyle = "document"
		}

		ops := map[string]bindOpMeta{}
		for _, bo := range b.Operations {
			meta := bindOpMeta{style: defaultStyle}
			so := bo.SOAPOp11
			if so == nil {
				so = bo.SOAPOp12
			}
			if so != nil {
				meta.soapAction = so.SOAPAction
				if so.Style != "" {
					meta.style = so.Style
				}
			}
			ops[bo.Name] = meta
		}
		bindOpsByBinding[b.Name] = ops

		bindings = append(bindings, model.Binding{Name: b.Name, SOAPVersion: version, Transport: transport})
	}

	// Services/ports: attach endpoint URLs to the matching binding by name.
	endpointByBinding := map[string]string{}
	for _, svc := range def.Services {
		for _, p := range svc.Ports {
			_, bindingLocal := rootScope.resolveQName(p.Binding)
			addr := p.Address11
			if addr == nil {
				addr = p.Address12
			}
			if addr != nil {
				endpointByBinding[bindingLocal] = addr.Location
			}
		}
	}
	for i := range bindings {
		bindings[i].EndpointURL = endpointByBinding[bindings[i].Name]
	}

	// Build the flat operation list from the primary binding (SOAP 1.1
	// preferred), joined against its portType for input/output/doc.
	var primaryBindingName string
	for _, b := range def.Bindings {
		if _, ok := bindOpsByBinding[b.Name]; !ok {
			continue
		}
		if primaryBindingName == "" {
			primaryBindingName = b.Name
		}
		// prefer a binding whose soap:binding was version 1.1
		for _, bm := range bindings {
			if bm.Name == b.Name && bm.SOAPVersion == "1.1" {
				primaryBindingName = b.Name
			}
		}
	}

	var operations []model.Operation
	if primaryBindingName != "" {
		ptName := bindingPortType[primaryBindingName]
		ops := portTypeOps[ptName]
		metas := bindOpsByBinding[primaryBindingName]
		for _, pt := range def.PortTypes {
			if pt.Name != ptName {
				continue
			}
			for _, op := range pt.Operations {
				info := ops[op.Name]
				meta := metas[op.Name]
				operations = append(operations, model.Operation{
					Name:          op.Name,
					Documentation: info.doc,
					SOAPAction:    meta.soapAction,
					Style:         meta.style,
					Input:         info.input,
					Output:        info.output,
				})
			}
		}
	}

	// WSSecurity is set by Parse (see detectWSSecurity), which scans the
	// raw document rather than the parsed WSDL structs: real WS-Security
	// is attached via WS-Policy extensibility elements on bindings, whose
	// shape varies enough across toolchains that a raw substring/namespace
	// scan is more robust than modeling WS-Policy itself (out of scope).
	result := &model.Definition{
		Name:            def.Name,
		TargetNamespace: def.TargetNamespace,
		Documentation:   strings.TrimSpace(def.Documentation),
		Operations:      operations,
		Bindings:        bindings,
	}
	return result, r.warnings, nil
}

// resolveTypeRef resolves a (namespace, local) pair that appeared as an
// XSD type="" attribute value into a concrete *model.Type: a builtin, a
// registered named type, or KindAny with a warning if neither matches.
func (r *resolver) resolveTypeRef(space, local string) *model.Type {
	if space == NSXSD {
		if k, ok := builtinKind(local); ok {
			return &model.Type{Kind: k}
		}
		r.warnf("unknown XSD builtin type %q; treating as opaque", local)
		return &model.Type{Kind: model.KindAny}
	}
	q := model.QName{Space: space, Local: local}
	if t, ok := r.types.lookup(q); ok {
		return t
	}
	r.warnf("unresolved type reference %q (namespace %q); treating as opaque", local, space)
	return &model.Type{Kind: model.KindAny}
}

func (r *resolver) resolveComplexType(ct rawComplexType, scope *nsScope, tns string) *model.Type {
	ctScope := newScope(scope, ct.Attrs)
	seq := ct.Sequence
	if seq == nil {
		seq = ct.All
	}
	if ct.Choice != nil {
		r.warnf("xsd:choice in complexType %q not supported in v1; alternative branches are ignored", ct.Name)
		if seq == nil {
			seq = &rawSequence{}
		}
	}
	result := &model.Type{Kind: model.KindObject, Doc: annotationDoc(ct.Annotation)}
	if seq == nil {
		return result
	}
	for _, el := range seq.Elements {
		field := r.resolveField(el, ctScope, tns)
		result.Properties = append(result.Properties, field)
	}
	return result
}

func (r *resolver) resolveField(el rawElement, scope *nsScope, tns string) model.Field {
	name := el.Name
	base := r.resolveElementBaseType(el, scope, tns)
	if el.Ref != "" {
		space, local := scope.resolveQName(el.Ref)
		if name == "" {
			name = local
		}
		if t, ok := r.elements.lookup(model.QName{Space: space, Local: local}); ok {
			base = t
		} else {
			r.warnf("element ref %q does not match any global element", el.Ref)
		}
	}

	maxOccurs := el.MaxOccurs
	isArray := maxOccurs == "unbounded"
	if !isArray && maxOccurs != "" {
		if n, err := strconv.Atoi(maxOccurs); err == nil && n > 1 {
			isArray = true
		}
	}
	fieldType := base
	if isArray {
		fieldType = &model.Type{Kind: model.KindArray, Items: base}
	}

	minOccurs := el.MinOccurs
	required := minOccurs != "0"

	return model.Field{
		Name:     name,
		Type:     fieldType,
		Required: required,
		Nillable: el.Nillable == "true",
		Doc:      annotationDoc(el.Annotation),
	}
}

// resolveElementBaseType resolves the *scalar* (pre-array-wrapping) type of
// a local <xsd:element>: inline complexType/simpleType, or a type=
// reference. It does not handle ref= (see resolveField) since a ref target
// is looked up by name, not resolved structurally here.
func (r *resolver) resolveElementBaseType(el rawElement, scope *nsScope, tns string) *model.Type {
	switch {
	case el.ComplexType != nil:
		return r.resolveComplexType(*el.ComplexType, scope, tns)
	case el.SimpleType != nil:
		return r.resolveSimpleType(*el.SimpleType)
	case el.Type != "":
		space, local := scope.resolveQName(el.Type)
		return r.resolveTypeRef(space, local)
	case el.Ref != "":
		// Resolved by the caller (resolveField); placeholder until then.
		return &model.Type{Kind: model.KindAny}
	default:
		return &model.Type{Kind: model.KindAny}
	}
}

// resolveElementBody resolves a *global* <xsd:element>'s type (used to fill
// the elements registry in pass 3).
func (r *resolver) resolveElementBody(el rawElement, scope *nsScope, tns string) *model.Type {
	t := r.resolveElementBaseType(el, scope, tns)
	if el.Annotation != nil && t.Doc == "" {
		t.Doc = annotationDoc(el.Annotation)
	}
	return t
}

func (r *resolver) resolveSimpleType(st rawSimpleType) *model.Type {
	if st.Restriction == nil {
		return &model.Type{Kind: model.KindString}
	}
	base := st.Restriction.Base
	local := base
	if i := strings.IndexByte(base, ':'); i >= 0 {
		local = base[i+1:]
	}
	kind, ok := builtinKind(local)
	if !ok {
		kind = model.KindString
	}
	t := &model.Type{Kind: kind}
	for _, e := range st.Restriction.Enumerations {
		t.Enum = append(t.Enum, e.Value)
	}
	return t
}

func annotationDoc(a *rawAnnotation) string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.Documentation)
}
