package wsdl

import "github.com/harshhh28/soapbridge/model"

// builtinKind maps an XSD built-in type's local name (always in the
// http://www.w3.org/2001/XMLSchema namespace) to the model.Kind it maps to.
// See the mapping rules in README's Schema mapper section.
var builtinKinds = map[string]model.Kind{
	// string family
	"string":            model.KindString,
	"token":              model.KindString,
	"normalizedString":   model.KindString,
	"anyURI":             model.KindString,
	"QName":               model.KindString,
	"NOTATION":            model.KindString,
	"NMTOKEN":              model.KindString,
	"NMTOKENS":             model.KindString,
	"Name":                 model.KindString,
	"NCName":               model.KindString,
	"ID":                   model.KindString,
	"IDREF":                model.KindString,
	"IDREFS":               model.KindString,
	"ENTITY":               model.KindString,
	"ENTITIES":             model.KindString,
	"language":             model.KindString,
	"hexBinary":            model.KindString,
	"base64Binary":         model.KindString,
	"duration":             model.KindString,
	"gYear":                model.KindString,
	"gYearMonth":           model.KindString,
	"gMonth":               model.KindString,
	"gMonthDay":            model.KindString,
	"gDay":                 model.KindString,
	"time":                 model.KindString,

	// integer family
	"int":                model.KindInteger,
	"integer":            model.KindInteger,
	"long":                model.KindInteger,
	"short":               model.KindInteger,
	"byte":                model.KindInteger,
	"unsignedLong":        model.KindInteger,
	"unsignedInt":         model.KindInteger,
	"unsignedShort":       model.KindInteger,
	"unsignedByte":        model.KindInteger,
	"negativeInteger":     model.KindInteger,
	"nonNegativeInteger":  model.KindInteger,
	"positiveInteger":     model.KindInteger,
	"nonPositiveInteger":  model.KindInteger,

	// number family
	"decimal": model.KindNumber,
	"double":  model.KindNumber,
	"float":   model.KindNumber,

	"boolean":  model.KindBoolean,
	"dateTime": model.KindDateTime,
	"date":     model.KindDate,

	"anyType":       model.KindAny,
	"anySimpleType": model.KindAny,
}

func builtinKind(local string) (model.Kind, bool) {
	k, ok := builtinKinds[local]
	return k, ok
}
