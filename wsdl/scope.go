package wsdl

import (
	"encoding/xml"
	"strings"
)

// nsScope is a namespace-prefix scope: the xmlns declarations in effect at
// one point in the document, chained to its ancestors. This is what lets
// resolveQName turn an attribute value like "tns:Foo" or an unprefixed
// "string" into a namespace URI + local name, regardless of which prefixes
// the document chose (xs: vs s:, a default xmlns vs an explicit wsdl:
// prefix, etc).
type nsScope struct {
	parent    *nsScope
	prefixes  map[string]string // prefix -> URI, declared at this level
	hasDef    bool
	defaultNS string
}

func newScope(parent *nsScope, attrs []xml.Attr) *nsScope {
	s := &nsScope{parent: parent}
	for _, a := range attrs {
		switch {
		case a.Name.Space == "xmlns":
			if s.prefixes == nil {
				s.prefixes = map[string]string{}
			}
			s.prefixes[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			s.hasDef = true
			s.defaultNS = a.Value
		}
	}
	return s
}

func (s *nsScope) resolvePrefix(prefix string) (string, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.prefixes != nil {
			if uri, ok := cur.prefixes[prefix]; ok {
				return uri, true
			}
		}
	}
	return "", false
}

func (s *nsScope) resolveDefault() string {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.hasDef {
			return cur.defaultNS
		}
	}
	return ""
}

// resolveQName resolves a lexical QName string (as found in an XSD/WSDL
// attribute value, e.g. type="tns:PurchaseOrder" or type="string") into a
// namespace-qualified model.QName using this scope's in-effect prefix
// mappings. An empty input yields a zero QName.
func (s *nsScope) resolveQName(qname string) (space, local string) {
	qname = strings.TrimSpace(qname)
	if qname == "" {
		return "", ""
	}
	if i := strings.IndexByte(qname, ':'); i >= 0 {
		prefix, local := qname[:i], qname[i+1:]
		uri, _ := s.resolvePrefix(prefix)
		return uri, local
	}
	return s.resolveDefault(), qname
}
