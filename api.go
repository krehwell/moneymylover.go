package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	webAPI   = "https://web.moneylover.me/api"
	oauthAPI = "https://oauth.moneylover.me"
	// Cloudflare drops non-browser user agents before Money Lover sees the request.
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type Wallet struct {
	ID       string              `json:"_id"`
	Name     string              `json:"name"`
	Currency int                 `json:"currency_id"`
	Archived bool                `json:"archived"`
	Balance  []map[string]string `json:"balance"`
}

type Category struct {
	ID       string `json:"_id"`
	Name     string `json:"name"`
	Type     int    `json:"type"` // 1 income, 2 expense
	Metadata string `json:"metadata"`
	Parent   string `json:"parent"`
}

type Transaction struct {
	ID          string  `json:"_id"`
	Amount      float64 `json:"amount"`
	Note        string  `json:"note"`
	DisplayDate string  `json:"displayDate"`
	Account     Wallet  `json:"account"`
	Category    struct {
		ID   string `json:"_id"`
		Name string `json:"name"`
		Type int    `json:"type"`
	} `json:"category"`
	ExcludeReport bool     `json:"exclude_report"`
	With          []string `json:"with"`
	Campaign      idList   `json:"campaign"`
	Images        []string `json:"images"`
	Latitude      float64  `json:"latitude"`
	Longtitude    float64  `json:"longtitude"`
	Remind        int64    `json:"remind"`
}

// idList reads a list of ids that list-all sends as strings and
// transaction/list sends as {_id} objects.
type idList []string

func (l *idList) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*l = idList{}
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			*l = append(*l, s)
			continue
		}
		var o struct {
			ID string `json:"_id"`
		}
		if err := json.Unmarshal(r, &o); err != nil {
			return err
		}
		*l = append(*l, o.ID)
	}
	return nil
}

type AddInput struct {
	WalletID      string
	CategoryID    string
	Amount        float64
	Note          string
	Date          string // YYYY-MM-DD
	Event         string
	ExcludeReport bool
	With          []string
}

type Client struct {
	tokens Tokens
	path   string
}

func tokenPath() string {
	if p := os.Getenv("MONEYLOVER_TOKEN_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "moneylover", "token.json")
}

func post(rawURL string, headers map[string]string, body any, form url.Values) ([]byte, error) {
	var payload []byte
	contentType := "application/json; charset=utf-8"
	if form != nil {
		payload = []byte(form.Encode())
		contentType = "application/x-www-form-urlencoded"
	} else {
		payload, _ = json.Marshal(body)
	}
	// Reads of the full ledger can take a minute when the API is slow; a write
	// with a wrong id hangs ~120s server side and never commits, so writes
	// give up at 30s and are never retried (a retry could double-post).
	// Delete is safe to retry: a second delete of the same id cannot remove more.
	write := strings.Contains(rawURL, "/add") || strings.Contains(rawURL, "/edit") || strings.Contains(rawURL, "/delete")
	timeout, attempts := 90*time.Second, 4
	if write {
		timeout, attempts = 30*time.Second, 1
	}
	if strings.Contains(rawURL, "/delete") {
		attempts = 4
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			time.Sleep(time.Duration(attempt*attempt) * time.Second)
		}
		req, err := http.NewRequest("POST", rawURL, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("User-Agent", userAgent)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := (&http.Client{Timeout: timeout}).Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		out, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			lastErr = fmt.Errorf("%s: HTTP %d: %.200s", rawURL, res.StatusCode, out)
			continue
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("%s: HTTP %d: %.200s", rawURL, res.StatusCode, out)
		}
		return out, nil
	}
	return nil, lastErr
}

// unwrap reads Money Lover's envelope. HTTP is always 200; the real result is
// in the body, and a rejected token uses abbreviated keys (s, e).
func unwrap(raw []byte, out any) error {
	var env struct {
		Error int             `json:"error"`
		E     int             `json:"e"`
		Msg   string          `json:"msg"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("non-JSON reply: %.200s", raw)
	}
	if env.Error != 0 || env.E != 0 {
		return fmt.Errorf("money lover: %s (error %d)", env.Msg, env.Error+env.E)
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func Login(email, password string) (Tokens, error) {
	raw, err := post(webAPI+"/user/login-url", nil, map[string]any{}, nil)
	if err != nil {
		return Tokens{}, err
	}
	var init struct {
		RequestToken string `json:"request_token"`
		LoginURL     string `json:"login_url"`
	}
	if err := unwrap(raw, &init); err != nil {
		return Tokens{}, err
	}
	u, err := url.Parse(init.LoginURL)
	if err != nil || u.Query().Get("client") == "" {
		return Tokens{}, fmt.Errorf("login_url has no client: %s", init.LoginURL)
	}
	raw, err = post(oauthAPI+"/token",
		map[string]string{"Authorization": "Bearer " + init.RequestToken, "Client": u.Query().Get("client")},
		nil, url.Values{"email": {email}, "password": {password}})
	if err != nil {
		return Tokens{}, err
	}
	var t struct {
		Tokens
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &t); err != nil || t.AccessToken == "" {
		return Tokens{}, fmt.Errorf("login failed: %s %.200s", t.Message, raw)
	}
	return t.Tokens, nil
}

func refresh(refreshToken string) (Tokens, error) {
	raw, err := post(webAPI+"/user/refresh-token", nil, map[string]string{"refreshToken": refreshToken}, nil)
	if err != nil {
		return Tokens{}, err
	}
	var t Tokens
	if err := unwrap(raw, &t); err != nil {
		return Tokens{}, err
	}
	if t.AccessToken == "" {
		return Tokens{}, errors.New("refresh returned no access_token")
	}
	return t, nil
}

func jwtExpired(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return true
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return true
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if json.Unmarshal(b, &claims) != nil {
		return true
	}
	return claims.Exp-60 < time.Now().Unix()
}

func SaveTokens(t Tokens) error {
	p := tokenPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(t, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// NewClient loads the cached token and renews it when expired. Renewing keeps
// the same device slot; a fresh login takes a new one (limit 5 per account).
func NewClient() (*Client, error) {
	p := tokenPath()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no token at %s, run: moneylover login", p)
	}
	var t Tokens
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if jwtExpired(t.AccessToken) {
		if t, err = refresh(t.RefreshToken); err != nil {
			return nil, fmt.Errorf("token expired and refresh failed (%v), run: moneylover login", err)
		}
		if err := SaveTokens(t); err != nil {
			return nil, err
		}
	}
	return &Client{tokens: t, path: p}, nil
}

func (c *Client) call(path string, body any, out any) error {
	raw, err := post(webAPI+path, map[string]string{"Authorization": "AuthJWT " + c.tokens.AccessToken}, body, nil)
	if err != nil {
		return err
	}
	return unwrap(raw, out)
}

func (c *Client) Wallets() ([]Wallet, error) {
	var w []Wallet
	return w, c.call("/wallet/list", map[string]any{}, &w)
}

// Categories uses list-all: those are the ids transaction/add accepts.
// category/list?walletId ids make the write hang until Cloudflare times out.
func (c *Client) Categories() ([]Category, error) {
	var cs []Category
	return cs, c.call("/category/list-all", map[string]any{}, &cs)
}

func (c *Client) Transactions(walletID, from, to string) ([]Transaction, error) {
	var res struct {
		Transactions []Transaction `json:"transactions"`
	}
	err := c.call("/transaction/list", map[string]string{"walletId": walletID, "startDate": from, "endDate": to}, &res)
	return res.Transactions, err
}

// All returns every transaction of every wallet, archived ones included.
func (c *Client) All() ([]Transaction, error) {
	var txs []Transaction
	return txs, c.call("/transaction/list-all", map[string]any{}, &txs)
}

// Edit is a full replace: every field left out is cleared server side, so the
// whole row is rebuilt from what list-all returned. catID must come from
// category/list-all; the row's own category id is wallet-scoped and hangs.
func (c *Client) Edit(t Transaction, catID string) error {
	event, image := "", ""
	if len(t.Campaign) > 0 {
		event = t.Campaign[0]
	}
	if len(t.Images) > 0 {
		image = t.Images[0]
	}
	with := t.With
	if with == nil {
		with = []string{}
	}
	return c.call("/transaction/edit", map[string]any{
		"_id": t.ID, "account": t.Account.ID, "category": catID, "amount": t.Amount,
		"note": t.Note, "displayDate": t.DisplayDate[:10], "event": event, "exclude_report": t.ExcludeReport,
		"with": with, "latitude": t.Latitude, "longtitude": t.Longtitude, "remind": t.Remind,
		"addressName": "", "addressDetails": "", "addressIcon": "", "image": image,
	}, nil)
}

func (c *Client) Add(in AddInput) (string, error) {
	var res struct {
		ID string `json:"_id"`
	}
	with := in.With
	if with == nil {
		with = []string{}
	}
	err := c.call("/transaction/add", map[string]any{
		"account": in.WalletID, "category": in.CategoryID, "amount": in.Amount,
		"note": in.Note, "displayDate": in.Date, "event": in.Event, "exclude_report": in.ExcludeReport,
		"with": with, "latitude": 0, "longtitude": 0,
		"addressName": "", "addressDetails": "", "addressIcon": "", "image": "",
	}, &res)
	return res.ID, err
}

func (c *Client) Delete(id string) error {
	return c.call("/transaction/delete", map[string]any{"_id": id, "delRelated": false}, nil)
}
