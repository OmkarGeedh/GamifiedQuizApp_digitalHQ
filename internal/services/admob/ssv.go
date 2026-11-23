// Package admob verifies AdMob rewarded-ad server-side verification (SSV)
// callbacks. Google signs every callback with ECDSA (P-256, SHA-256) over the
// query string; the public keys are published at a fixed URL and rotate. A
// reward is paid only for a callback whose signature checks out, so a player
// (or anyone else) cannot fake "I watched an ad".
package admob

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	// ErrMalformed means the callback is not a well-formed SSV request.
	ErrMalformed = errors.New("malformed SSV callback")
	// ErrBadSignature means the signature does not verify with Google's key.
	ErrBadSignature = errors.New("SSV signature does not verify")
	// ErrUnknownKey means no published key has the callback's key_id.
	ErrUnknownKey = errors.New("unknown SSV key id")
)

// Callback is a verified SSV callback.
type Callback struct {
	AdNetwork     string
	AdUnit        string
	CustomData    string
	RewardAmount  int64
	RewardItem    string
	RewardedAt    time.Time
	TransactionID string
	UserID        string
	KeyID         int64
}

const (
	keysTTL        = 24 * time.Hour
	refetchMinWait = time.Minute
	fetchTimeout   = 5 * time.Second
	maxKeysBody    = 64 << 10
)

// Verifier checks callbacks against Google's published keys, cached in memory.
type Verifier struct {
	keysURL string
	client  *http.Client

	mu      sync.Mutex
	keys    map[int64]*ecdsa.PublicKey
	fetched time.Time
	lastTry time.Time
	fixed   bool // keys installed by a test; never fetched
}

// NewVerifier creates a verifier that loads keys from keysURL on first use.
func NewVerifier(keysURL string) *Verifier {
	return &Verifier{keysURL: keysURL, client: &http.Client{Timeout: fetchTimeout}}
}

// NewVerifierWithKeys creates a verifier with fixed keys (tests).
func NewVerifierWithKeys(keys map[int64]*ecdsa.PublicKey) *Verifier {
	return &Verifier{keys: keys, fixed: true}
}

// Verify checks the signature of a raw (still URL-encoded) SSV query string and
// returns its fields. The signed message is everything before "&signature=";
// signature and key_id are always the last two parameters.
func (v *Verifier) Verify(ctx context.Context, rawQuery string) (*Callback, error) {
	idx := strings.Index(rawQuery, "&signature=")
	if idx <= 0 {
		return nil, ErrMalformed
	}
	message := rawQuery[:idx]
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, ErrMalformed
	}
	sigText := values.Get("signature")
	keyID, err := strconv.ParseInt(values.Get("key_id"), 10, 64)
	if sigText == "" || err != nil {
		return nil, ErrMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(sigText, "="))
	if err != nil {
		return nil, ErrMalformed
	}
	key, err := v.key(ctx, keyID)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(message))
	if !ecdsa.VerifyASN1(key, digest[:], sig) {
		return nil, ErrBadSignature
	}

	cb := &Callback{
		AdNetwork:     values.Get("ad_network"),
		AdUnit:        values.Get("ad_unit"),
		CustomData:    values.Get("custom_data"),
		RewardItem:    values.Get("reward_item"),
		TransactionID: values.Get("transaction_id"),
		UserID:        values.Get("user_id"),
		KeyID:         keyID,
	}
	if n, err := strconv.ParseInt(values.Get("reward_amount"), 10, 64); err == nil {
		cb.RewardAmount = n
	}
	if ms, err := strconv.ParseInt(values.Get("timestamp"), 10, 64); err == nil {
		cb.RewardedAt = time.UnixMilli(ms).UTC()
	}
	if cb.TransactionID == "" {
		return nil, ErrMalformed
	}
	return cb, nil
}

// key returns the public key for keyID, fetching Google's key list when the
// cache is empty, stale, or does not have the id (rate limited).
func (v *Verifier) key(ctx context.Context, keyID int64) (*ecdsa.PublicKey, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[keyID]; ok && (v.fixed || time.Since(v.fetched) < keysTTL) {
		return k, nil
	}
	if v.fixed {
		return nil, ErrUnknownKey
	}
	if time.Since(v.lastTry) < refetchMinWait {
		if k, ok := v.keys[keyID]; ok {
			return k, nil // stale but known: better than failing
		}
		return nil, ErrUnknownKey
	}
	v.lastTry = time.Now()
	keys, err := fetchKeys(ctx, v.client, v.keysURL)
	if err != nil {
		if k, ok := v.keys[keyID]; ok {
			return k, nil
		}
		return nil, fmt.Errorf("load SSV keys: %w", err)
	}
	v.keys, v.fetched = keys, time.Now()
	if k, ok := v.keys[keyID]; ok {
		return k, nil
	}
	return nil, ErrUnknownKey
}

type keyList struct {
	Keys []struct {
		KeyID  int64  `json:"keyId"`
		PEM    string `json:"pem"`
		Base64 string `json:"base64"`
	} `json:"keys"`
}

func fetchKeys(ctx context.Context, client *http.Client, keysURL string) (map[int64]*ecdsa.PublicKey, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, keysURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("keys endpoint returned %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxKeysBody))
	if err != nil {
		return nil, err
	}
	return ParseKeys(body)
}

// ParseKeys parses Google's verifier-keys JSON.
func ParseKeys(body []byte) (map[int64]*ecdsa.PublicKey, error) {
	var list keyList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, err
	}
	out := make(map[int64]*ecdsa.PublicKey, len(list.Keys))
	for _, k := range list.Keys {
		der, err := base64.StdEncoding.DecodeString(k.Base64)
		if err != nil || len(der) == 0 {
			block, _ := pem.Decode([]byte(k.PEM))
			if block == nil {
				continue
			}
			der = block.Bytes
		}
		pub, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			continue
		}
		if ec, ok := pub.(*ecdsa.PublicKey); ok {
			out[k.KeyID] = ec
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no usable SSV keys")
	}
	return out, nil
}
