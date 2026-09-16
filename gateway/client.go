package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/harshhh28/soapbridge/model"
)

// Client calls one SOAP endpoint. Operations from the same WSDL binding
// share a Client (and its http.Client, so connections are reused).
type Client struct {
	HTTPClient *http.Client
	Endpoint   string
}

func NewClient(endpoint string) *Client {
	return &Client{HTTPClient: &http.Client{Timeout: 30 * time.Second}, Endpoint: endpoint}
}

// Call builds the SOAP envelope for op from body, POSTs it to c.Endpoint,
// and parses the response. A non-nil *Fault means the SOAP call completed
// but the service reported an application-level fault; err is reserved for
// transport/protocol failures (network errors, unparseable responses).
func (c *Client) Call(ctx context.Context, op model.Operation, body map[string]interface{}, creds Credentials) (interface{}, *Fault, error) {
	envelope, err := BuildEnvelope(op, body, creds)
	if err != nil {
		return nil, nil, fmt.Errorf("building SOAP request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(envelope))
	if err != nil {
		return nil, nil, fmt.Errorf("building HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"`+op.SOAPAction+`"`)
	// Some SOAP endpoints sit behind bot-filtering that rejects Go's
	// default "Go-http-client/x.y" User-Agent outright (observed live
	// against a public demo service); identify honestly instead.
	req.Header.Set("User-Agent", "github.com/harshhh28/soapbridge/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("calling SOAP endpoint %s: %w", c.Endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("reading SOAP response: %w", err)
	}

	value, fault, perr := ParseResponse(data, op)
	if perr != nil {
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("SOAP endpoint returned HTTP %d with an unparseable body: %w", resp.StatusCode, perr)
		}
		return nil, nil, perr
	}
	return value, fault, nil
}
