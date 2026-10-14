// Package mpesa is the Safaricom Daraja client: STK push collection, STK status query and B2C payouts.
// A fake implementation keeps development and tests off the network; the callbacks it would trigger
// are posted to the webhook endpoints by the test or by hand.
package mpesa

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type STKPush struct {
	MSISDN      string // 2547XXXXXXXX
	AmountKES   int64  // whole shillings: Daraja does not move cents
	Reference   string // shown on the customer's statement, e.g. the invoice reference
	Description string
	CallbackURL string
}

type STKResult struct{ MerchantRequestID, CheckoutRequestID string }

type STKStatus struct {
	Done       bool
	Success    bool
	ResultDesc string
}

type B2C struct {
	MSISDN         string
	AmountKES      int64
	Remarks        string
	OriginatorConv string // our idempotency id for this payout
	ResultURL      string
	TimeoutURL     string
}

type B2CResult struct{ ConversationID, OriginatorConversationID string }

type Client interface {
	STKPush(ctx context.Context, r STKPush) (STKResult, error)
	STKQuery(ctx context.Context, checkoutRequestID string) (STKStatus, error)
	B2C(ctx context.Context, r B2C) (B2CResult, error)
}

type Config struct {
	Mode           string // fake | sandbox | production
	ConsumerKey    string
	ConsumerSecret string
	ShortCode      string // paybill / till collecting STK payments
	PassKey        string
	Initiator      string // B2C initiator name
	SecurityCred   string // B2C encrypted initiator password
	B2CShortCode   string
}

func New(c Config) (Client, error) {
	switch c.Mode {
	case "", "fake":
		return &Fake{}, nil
	case "sandbox", "production":
		if c.ConsumerKey == "" || c.ConsumerSecret == "" || c.ShortCode == "" || c.PassKey == "" {
			return nil, errors.New("mpesa: MPESA_CONSUMER_KEY, MPESA_CONSUMER_SECRET, MPESA_SHORTCODE and MPESA_PASSKEY are required")
		}
		base := "https://sandbox.safaricom.co.ke"
		if c.Mode == "production" {
			base = "https://api.safaricom.co.ke"
		}
		return &Daraja{cfg: c, base: base, http: &http.Client{Timeout: 20 * time.Second}}, nil
	}
	return nil, fmt.Errorf("mpesa: unknown MPESA_MODE %q", c.Mode)
}

// ---- Fake

// Fake records requests and hands back sequential ids.
type Fake struct {
	n    atomic.Int64
	mu   sync.Mutex
	STKs []STKPush
	B2Cs []B2C
	// Status is what STKQuery reports; zero value means still pending.
	Status STKStatus
}

func (f *Fake) STKPush(_ context.Context, r STKPush) (STKResult, error) {
	n := f.n.Add(1)
	f.mu.Lock()
	f.STKs = append(f.STKs, r)
	f.mu.Unlock()
	return STKResult{MerchantRequestID: fmt.Sprintf("fake-merchant-%d", n), CheckoutRequestID: fmt.Sprintf("ws_CO_fake_%d", n)}, nil
}

func (f *Fake) STKQuery(context.Context, string) (STKStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Status, nil
}

func (f *Fake) B2C(_ context.Context, r B2C) (B2CResult, error) {
	n := f.n.Add(1)
	f.mu.Lock()
	f.B2Cs = append(f.B2Cs, r)
	f.mu.Unlock()
	return B2CResult{ConversationID: fmt.Sprintf("AG_fake_%d", n), OriginatorConversationID: r.OriginatorConv}, nil
}

// ---- Daraja

type Daraja struct {
	cfg  Config
	base string
	http *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

func (d *Daraja) bearer(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.token != "" && time.Now().Before(d.expiry) {
		return d.token, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, d.base+"/oauth/v1/generate?grant_type=client_credentials", nil)
	req.SetBasicAuth(d.cfg.ConsumerKey, d.cfg.ConsumerSecret)
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   string `json:"expires_in"`
	}
	if err := d.do(req, &out); err != nil {
		return "", err
	}
	d.token, d.expiry = out.AccessToken, time.Now().Add(50*time.Minute)
	return d.token, nil
}

func (d *Daraja) post(ctx context.Context, path string, body, out any) error {
	tok, err := d.bearer(ctx)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, d.base+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	return d.do(req, out)
}

func (d *Daraja) do(req *http.Request, out any) error {
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("daraja %s: %d %s", req.URL.Path, resp.StatusCode, bytes.TrimSpace(raw))
	}
	return json.Unmarshal(raw, out)
}

func (d *Daraja) password(ts string) string {
	return base64.StdEncoding.EncodeToString([]byte(d.cfg.ShortCode + d.cfg.PassKey + ts))
}

func (d *Daraja) STKPush(ctx context.Context, r STKPush) (STKResult, error) {
	ts := time.Now().Format("20060102150405")
	var out struct {
		MerchantRequestID   string `json:"MerchantRequestID"`
		CheckoutRequestID   string `json:"CheckoutRequestID"`
		ResponseCode        string `json:"ResponseCode"`
		ResponseDescription string `json:"ResponseDescription"`
	}
	err := d.post(ctx, "/mpesa/stkpush/v1/processrequest", map[string]any{
		"BusinessShortCode": d.cfg.ShortCode, "Password": d.password(ts), "Timestamp": ts,
		"TransactionType": "CustomerPayBillOnline", "Amount": r.AmountKES, "PartyA": r.MSISDN, "PartyB": d.cfg.ShortCode,
		"PhoneNumber": r.MSISDN, "CallBackURL": r.CallbackURL, "AccountReference": r.Reference, "TransactionDesc": r.Description,
	}, &out)
	if err != nil {
		return STKResult{}, err
	}
	if out.ResponseCode != "0" {
		return STKResult{}, fmt.Errorf("daraja stk push rejected: %s", out.ResponseDescription)
	}
	return STKResult{MerchantRequestID: out.MerchantRequestID, CheckoutRequestID: out.CheckoutRequestID}, nil
}

func (d *Daraja) STKQuery(ctx context.Context, checkoutRequestID string) (STKStatus, error) {
	ts := time.Now().Format("20060102150405")
	var out struct {
		ResultCode string `json:"ResultCode"`
		ResultDesc string `json:"ResultDesc"`
	}
	err := d.post(ctx, "/mpesa/stkpushquery/v1/query", map[string]any{
		"BusinessShortCode": d.cfg.ShortCode, "Password": d.password(ts), "Timestamp": ts, "CheckoutRequestID": checkoutRequestID,
	}, &out)
	if err != nil {
		// Daraja answers 500 while the prompt is still open on the handset.
		return STKStatus{}, nil
	}
	return STKStatus{Done: true, Success: out.ResultCode == "0", ResultDesc: out.ResultDesc}, nil
}

func (d *Daraja) B2C(ctx context.Context, r B2C) (B2CResult, error) {
	var out struct {
		ConversationID           string `json:"ConversationID"`
		OriginatorConversationID string `json:"OriginatorConversationID"`
		ResponseCode             string `json:"ResponseCode"`
		ResponseDescription      string `json:"ResponseDescription"`
	}
	err := d.post(ctx, "/mpesa/b2c/v3/paymentrequest", map[string]any{
		"OriginatorConversationID": r.OriginatorConv, "InitiatorName": d.cfg.Initiator, "SecurityCredential": d.cfg.SecurityCred,
		"CommandID": "BusinessPayment", "Amount": r.AmountKES, "PartyA": d.cfg.B2CShortCode, "PartyB": r.MSISDN,
		"Remarks": r.Remarks, "QueueTimeOutURL": r.TimeoutURL, "ResultURL": r.ResultURL, "Occassion": "",
	}, &out)
	if err != nil {
		return B2CResult{}, err
	}
	if out.ResponseCode != "0" {
		return B2CResult{}, fmt.Errorf("daraja b2c rejected: %s", out.ResponseDescription)
	}
	return B2CResult{ConversationID: out.ConversationID, OriginatorConversationID: out.OriginatorConversationID}, nil
}
