package wsdl

import (
	"os"
	"testing"

	"github.com/harshhh28/soapbridge/model"
)

func loadFixture(t *testing.T, name string) *Result {
	t.Helper()
	data, err := os.ReadFile("../testdata/wsdl/" + name)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	res, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	for _, w := range res.Warnings {
		t.Errorf("unexpected warning for %s: %s", name, w)
	}
	return res
}

func findOp(t *testing.T, def *model.Definition, name string) model.Operation {
	t.Helper()
	for _, op := range def.Operations {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("operation %q not found", name)
	return model.Operation{}
}

func findField(t *testing.T, typ *model.Type, name string) model.Field {
	t.Helper()
	for _, f := range typ.Properties {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not found among %d properties", name, len(typ.Properties))
	return model.Field{}
}

// TestNumberConversion covers a WSDL where the wsdl: namespace is the
// *default* xmlns (no prefix) and the schema namespace uses the
// conventional "xs:" prefix.
func TestNumberConversion(t *testing.T) {
	def := loadFixture(t, "numberconversion.wsdl").Definition

	if def.TargetNamespace != "http://www.dataaccess.com/webservicesserver/" {
		t.Errorf("TargetNamespace = %q", def.TargetNamespace)
	}
	if len(def.Operations) != 2 {
		t.Fatalf("got %d operations, want 2", len(def.Operations))
	}

	op := findOp(t, def, "NumberToWords")
	if op.Style != "document" {
		t.Errorf("Style = %q, want document", op.Style)
	}
	if op.Documentation == "" {
		t.Errorf("expected documentation pulled from <documentation>")
	}
	if op.Input.Type.Kind != model.KindObject {
		t.Fatalf("Input.Type.Kind = %v, want object", op.Input.Type.Kind)
	}
	f := findField(t, op.Input.Type, "ubiNum")
	if f.Type.Kind != model.KindInteger {
		t.Errorf("ubiNum kind = %v, want integer (xsd:unsignedLong)", f.Type.Kind)
	}
	if !f.Required {
		t.Errorf("ubiNum should be required (minOccurs defaults to 1)")
	}

	out := findField(t, op.Output.Type, "NumberToWordsResult")
	if out.Type.Kind != model.KindString {
		t.Errorf("NumberToWordsResult kind = %v, want string", out.Type.Kind)
	}

	pb := def.PrimaryBinding()
	if pb == nil || pb.SOAPVersion != "1.1" {
		t.Fatalf("PrimaryBinding = %+v, want SOAP 1.1", pb)
	}
	if pb.EndpointURL != "https://www.dataaccess.com/webservicesserver/NumberConversion.wso" {
		t.Errorf("EndpointURL = %q", pb.EndpointURL)
	}
}

// TestCalculator covers a WSDL that uses the "wsdl:" prefix explicitly for
// the WSDL namespace and "s:" (not "xs:") for the schema namespace — the
// namespace resolution must not hardcode either prefix.
func TestCalculator(t *testing.T) {
	def := loadFixture(t, "calculator.wsdl").Definition

	if len(def.Operations) != 4 {
		t.Fatalf("got %d operations, want 4", len(def.Operations))
	}
	op := findOp(t, def, "Add")
	if op.SOAPAction != "http://tempuri.org/Add" {
		t.Errorf("SOAPAction = %q", op.SOAPAction)
	}
	if len(op.Input.Type.Properties) != 2 {
		t.Fatalf("Add input has %d properties, want 2", len(op.Input.Type.Properties))
	}
	a := findField(t, op.Input.Type, "intA")
	if a.Type.Kind != model.KindInteger {
		t.Errorf("intA kind = %v, want integer", a.Type.Kind)
	}
	result := findField(t, op.Output.Type, "AddResult")
	if result.Type.Kind != model.KindInteger {
		t.Errorf("AddResult kind = %v, want integer", result.Type.Kind)
	}
}

// TestCountryInfo covers named complexTypes referenced from multiple call
// sites (must resolve to the same *model.Type, not diverging copies), and
// the "wrapper element containing repeated children" array idiom.
func TestCountryInfo(t *testing.T) {
	def := loadFixture(t, "countryinfo.wsdl").Definition

	op := findOp(t, def, "FullCountryInfoAllCountries")
	result := findField(t, op.Output.Type, "FullCountryInfoAllCountriesResult")
	// ArrayOftCountryInfo is itself an object wrapping one repeated field.
	if result.Type.Kind != model.KindObject {
		t.Fatalf("ArrayOftCountryInfo kind = %v, want object", result.Type.Kind)
	}
	inner := findField(t, result.Type, "tCountryInfo")
	if inner.Type.Kind != model.KindArray {
		t.Fatalf("tCountryInfo kind = %v, want array", inner.Type.Kind)
	}
	if inner.Type.Items.Kind != model.KindObject {
		t.Fatalf("array item kind = %v, want object", inner.Type.Items.Kind)
	}
	countryInfoType := inner.Type.Items

	// Languages is typed as the named wrapper complexType ArrayOftLanguage
	// (object), which in turn wraps a repeated "tLanguage" element (array)
	// — the same wrapper-element array idiom one level down.
	langs := findField(t, countryInfoType, "Languages")
	if langs.Type.Kind != model.KindObject {
		t.Fatalf("Languages kind = %v, want object (ArrayOftLanguage)", langs.Type.Kind)
	}
	langsArr := findField(t, langs.Type, "tLanguage")
	if langsArr.Type.Kind != model.KindArray {
		t.Fatalf("Languages.tLanguage kind = %v, want array", langsArr.Type.Kind)
	}

	// The same named type (ArrayOftLanguage) reached through a different
	// operation must be the identical pointer, proving the registry
	// resolves named types once rather than duplicating them.
	op2 := findOp(t, def, "ListOfLanguagesByName")
	result2 := findField(t, op2.Output.Type, "ListOfLanguagesByNameResult")
	if result2.Type != langs.Type {
		t.Errorf("ArrayOftLanguage resolved to different pointers via different operations; named types should be shared")
	}
}

// TestEnumSample exercises constructs not present in any of the live
// fixtures above: xsd:simpleType enumeration, an explicitly nillable
// optional element, and a named complexType used by both the request and
// the response (must resolve to one shared pointer).
func TestEnumSample(t *testing.T) {
	def := loadFixture(t, "enumsample.wsdl").Definition

	op := findOp(t, def, "CreateTicket")
	ticketIn := findField(t, op.Input.Type, "ticket")
	ticketOut := findField(t, op.Output.Type, "ticket")
	if ticketIn.Type != ticketOut.Type {
		t.Errorf("Ticket type should be the same shared pointer in request and response")
	}

	priority := findField(t, ticketIn.Type, "priority")
	if priority.Type.Kind != model.KindString {
		t.Fatalf("priority kind = %v, want string", priority.Type.Kind)
	}
	wantEnum := []string{"LOW", "MEDIUM", "HIGH"}
	if len(priority.Type.Enum) != len(wantEnum) {
		t.Fatalf("priority enum = %v, want %v", priority.Type.Enum, wantEnum)
	}
	for i, v := range wantEnum {
		if priority.Type.Enum[i] != v {
			t.Errorf("priority enum[%d] = %q, want %q", i, priority.Type.Enum[i], v)
		}
	}

	assignee := findField(t, ticketIn.Type, "assignee")
	if !assignee.Nillable {
		t.Errorf("assignee should be nillable")
	}
	if assignee.Required {
		t.Errorf("assignee should not be required (minOccurs=0)")
	}

	dueAt := findField(t, ticketIn.Type, "dueAt")
	if dueAt.Type.Kind != model.KindDateTime {
		t.Errorf("dueAt kind = %v, want dateTime", dueAt.Type.Kind)
	}
}

// TestWSSecurityDetection checks the raw-document heuristic picks up a
// WS-Policy UsernameToken assertion attached to a binding (how real
// enterprise WSDLs attach WS-Security), and that a WSDL with none of the
// three other fixtures' plain WSDLs is NOT flagged.
func TestWSSecurityDetection(t *testing.T) {
	secure := loadFixture(t, "wssecuritysample.wsdl").Definition
	if !secure.WSSecurity {
		t.Errorf("expected WSSecurity=true for a WSDL carrying a UsernameToken policy")
	}

	plain := loadFixture(t, "calculator.wsdl").Definition
	if plain.WSSecurity {
		t.Errorf("expected WSSecurity=false for a plain WSDL")
	}
}

// TestNamespacePrefixIndependence is a minimal, deliberately adversarial
// document proving QName resolution follows declared xmlns bindings rather
// than assuming any particular prefix string. It reuses "xs" to mean the
// WSDL namespace and an arbitrary "zzz" prefix for XML Schema — the
// opposite of every real-world convention.
func TestNamespacePrefixIndependence(t *testing.T) {
	doc := []byte(`<?xml version="1.0"?>
<xs:definitions xmlns:xs="http://schemas.xmlsoap.org/wsdl/"
                xmlns:zzz="http://www.w3.org/2001/XMLSchema"
                xmlns:soap="http://schemas.xmlsoap.org/wsdl/soap/"
                xmlns:tns="urn:test"
                name="Weird" targetNamespace="urn:test">
  <xs:types>
    <zzz:schema targetNamespace="urn:test" elementFormDefault="qualified">
      <zzz:element name="Ping">
        <zzz:complexType>
          <zzz:sequence>
            <zzz:element name="msg" type="zzz:string"/>
          </zzz:sequence>
        </zzz:complexType>
      </zzz:element>
      <zzz:element name="PingResponse">
        <zzz:complexType>
          <zzz:sequence>
            <zzz:element name="ok" type="zzz:boolean"/>
          </zzz:sequence>
        </zzz:complexType>
      </zzz:element>
    </zzz:schema>
  </xs:types>
  <xs:message name="PingIn"><xs:part name="parameters" element="tns:Ping"/></xs:message>
  <xs:message name="PingOut"><xs:part name="parameters" element="tns:PingResponse"/></xs:message>
  <xs:portType name="PT">
    <xs:operation name="Ping">
      <xs:input message="tns:PingIn"/>
      <xs:output message="tns:PingOut"/>
    </xs:operation>
  </xs:portType>
  <xs:binding name="B" type="tns:PT">
    <soap:binding style="document" transport="http://schemas.xmlsoap.org/soap/http"/>
    <xs:operation name="Ping">
      <soap:operation soapAction="urn:test/Ping" style="document"/>
      <xs:input><soap:body use="literal"/></xs:input>
      <xs:output><soap:body use="literal"/></xs:output>
    </xs:operation>
  </xs:binding>
  <xs:service name="S">
    <xs:port name="P" binding="tns:B"><soap:address location="http://localhost/ping"/></xs:port>
  </xs:service>
</xs:definitions>`)

	res, err := Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, w := range res.Warnings {
		t.Errorf("unexpected warning: %s", w)
	}
	op := findOp(t, res.Definition, "Ping")
	msg := findField(t, op.Input.Type, "msg")
	if msg.Type.Kind != model.KindString {
		t.Errorf("msg kind = %v, want string (from zzz:string, zzz bound to the XSD namespace)", msg.Type.Kind)
	}
	ok := findField(t, op.Output.Type, "ok")
	if ok.Type.Kind != model.KindBoolean {
		t.Errorf("ok kind = %v, want boolean", ok.Type.Kind)
	}
}
