package admob

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func signedQuery(t *testing.T, key *ecdsa.PrivateKey, keyID, body string) string {
	t.Helper()
	d := sha256.Sum256([]byte(body))
	sig, err := ecdsa.SignASN1(rand.Reader, key, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return body + "&signature=" + base64.RawURLEncoding.EncodeToString(sig) + "&key_id=" + keyID
}

const body = "ad_network=5450213213286189855&ad_unit=1234567890&custom_data=abc&reward_amount=1&reward_item=Reward&timestamp=1700000000000&transaction_id=txn123&user_id=u1"

func TestVerify(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	v := NewVerifierWithKeys(map[int64]*ecdsa.PublicKey{42: &key.PublicKey})

	cb, err := v.Verify(context.Background(), signedQuery(t, key, "42", body))
	if err != nil || cb.TransactionID != "txn123" || cb.UserID != "u1" || cb.CustomData != "abc" || cb.AdUnit != "1234567890" || cb.RewardedAt.UnixMilli() != 1700000000000 {
		t.Fatalf("valid callback: %+v %v", cb, err)
	}
	cases := map[string]string{
		"tampered field": strings.Replace(signedQuery(t, key, "42", body), "user_id=u1", "user_id=u2", 1),
		"wrong key":      signedQuery(t, other, "42", body),
	}
	for name, q := range cases {
		if _, err := v.Verify(context.Background(), q); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: err = %v, want bad signature", name, err)
		}
	}
	if _, err := v.Verify(context.Background(), signedQuery(t, key, "7", body)); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("unknown key id: %v", err)
	}
	for _, q := range []string{body, "signature=x&key_id=42", body + "&signature=!!&key_id=42", body + "&signature=abc&key_id=notanumber"} {
		if _, err := v.Verify(context.Background(), q); !errors.Is(err, ErrMalformed) {
			t.Errorf("malformed %q: %v", q, err)
		}
	}
}

func TestKeysAreFetchedAndCached(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"keyId": 3335741209, "base64": base64.StdEncoding.EncodeToString(der)}}})
	}))
	defer srv.Close()
	v := NewVerifier(srv.URL)
	for i := 0; i < 3; i++ {
		if _, err := v.Verify(context.Background(), signedQuery(t, key, "3335741209", body)); err != nil {
			t.Fatalf("verify %d: %v", i, err)
		}
	}
	if hits != 1 {
		t.Errorf("keys fetched %d times, want 1 (cached)", hits)
	}
	if _, err := ParseKeys([]byte(`{"keys":[]}`)); err == nil {
		t.Error("an empty key list must be rejected")
	}
}
