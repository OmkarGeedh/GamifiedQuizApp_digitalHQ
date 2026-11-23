package services

// Google Play Billing. The rule everything here follows: the backend is the
// only thing that grants paid value, and only after the Google Play Developer
// API confirms the purchase. The app sends nothing but a product id and the
// purchase token; whether it was paid, who owns it, how much it gives and when
// it expires all come from Google and from billing_products.
//
// See docs/billing.md for the flow, the environment variables and the Play
// Console / Pub/Sub setup.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/config"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/dto"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/models"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/repo"
	"github.com/OmkarGeedh/GamifiedQuizApp_digitalHQ/internal/services/playstore"
	"google.golang.org/api/idtoken"
)

// ErrBillingDisabled is returned while billing is not (fully) configured.
// Billing fails closed: there is no mode that accepts purchases unverified.
var ErrBillingDisabled = errors.New("payments are not available right now")

// maxPurchaseQuantity bounds multi-quantity purchases (Play allows up to 99 if enabled).
const maxPurchaseQuantity = 99

type billingRuntime struct {
	play      playstore.Client
	secret    []byte
	allowTest bool
	pkg       string
}

var (
	billingMu sync.Mutex
	billingRT *billingRuntime
	// billingNow is the clock for Premium expiry checks (tests pin it).
	billingNow = func() time.Time { return time.Now().UTC() }
	// rtdnTokenValidator checks Pub/Sub push OIDC tokens (tests replace it).
	rtdnTokenValidator = idtoken.Validate
)

// getBilling returns the configured billing runtime, building the Play client
// on first use (not at boot, so cold starts stay fast).
func getBilling() (*billingRuntime, error) {
	billingMu.Lock()
	defer billingMu.Unlock()
	if billingRT != nil {
		return billingRT, nil
	}
	cfg := config.AppConfig
	if cfg == nil || len(cfg.Billing.VerifyProblems()) > 0 {
		return nil, ErrBillingDisabled
	}
	client, err := playstore.NewClient(context.Background(), cfg.Billing.PackageName, cfg.Billing.ServiceAccountJSON)
	if err != nil {
		log.Printf("[Billing] could not create Google Play client: %v", err)
		return nil, ErrBillingDisabled
	}
	billingRT = &billingRuntime{
		play:      client,
		secret:    []byte(cfg.Billing.AccountSecret),
		allowTest: cfg.Billing.AllowTestPurchases,
		pkg:       cfg.Billing.PackageName,
	}
	return billingRT, nil
}

// SetBillingForTest installs a fake Play client and secret, and returns a
// function that restores the previous state. Tests only.
func SetBillingForTest(play playstore.Client, secret string, allowTest bool) func() {
	billingMu.Lock()
	prev := billingRT
	billingRT = &billingRuntime{play: play, secret: []byte(secret), allowTest: allowTest, pkg: "com.skillverse.skillverse_app"}
	billingMu.Unlock()
	return func() {
		billingMu.Lock()
		billingRT = prev
		billingMu.Unlock()
	}
}

// LogBillingStatus reports at startup whether billing is enabled, naming what
// is missing (never the secret values).
func LogBillingStatus() {
	if config.AppConfig == nil {
		return
	}
	b := config.AppConfig.Billing
	if p := b.VerifyProblems(); len(p) > 0 {
		log.Printf("[Billing] disabled (purchase endpoints return 503): %s", strings.Join(p, "; "))
		return
	}
	log.Printf("[Billing] enabled for package %s (test purchases allowed: %v)", b.PackageName, b.AllowTestPurchases)
	if p := b.RTDNProblems(); len(p) > 0 {
		log.Printf("[Billing] Real-time Developer Notifications disabled: %s", strings.Join(p, "; "))
	}
}

// ObfuscatedAccountID is the opaque per-player id passed to Google Play at
// checkout. Google returns it with the purchase, so a token can only be
// redeemed by the player who bought it. It is an HMAC, so it reveals nothing
// about the player and cannot be forged without the server secret. 64 hex
// characters, the most Play accepts.
func ObfuscatedAccountID(secret []byte, clientID int) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("skillverse-play-account:" + strconv.Itoa(clientID)))
	return hex.EncodeToString(m.Sum(nil))
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// audit writes a billing_audit row. Best effort: a failed audit write is
// logged but never changes the outcome.
func audit(ctx context.Context, a repo.BillingAudit) {
	if err := repo.InsertBillingAudit(ctx, a); err != nil {
		log.Printf("[Billing] audit write failed (%s/%s token %.8s): %v", a.Action, a.Result, a.TokenHash, err)
	}
	if a.Result == "fraud" || a.Result == "shortfall" || a.Result == "error" {
		log.Printf("[Billing] %s %s client=%d product=%s token=%.8s: %s", a.Action, a.Result, a.ClientID, a.ProductID, a.TokenHash, a.Reason)
	}
}

// ---- Catalog and account ----------------------------------------------------

// ListBillingProducts returns what the app may sell to this player. A topic
// pack is listed only when it unlocks at least one subject, with Owned set
// when the player already has it.
func ListBillingProducts(ctx context.Context, clientID int) ([]dto.BillingProductDTO, int, error) {
	if _, err := getBilling(); err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	rows, err := repo.ListActiveBillingProducts(ctx)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load products: %w", err)
	}
	var owned map[string]bool
	out := make([]dto.BillingProductDTO, 0, len(rows))
	for _, p := range rows {
		item := dto.BillingProductDTO{ProductID: p.ProductID, Kind: p.Kind, Title: p.Title, Coins: p.Coins, Gems: p.Gems}
		if p.Kind == models.BillingKindNonConsumable {
			item.Subjects = packSubjects(ctx, p.ProductID)
			if len(item.Subjects) == 0 {
				continue // never sell an empty pack
			}
			if owned == nil {
				owned = map[string]bool{}
				ids, err := repo.OwnedProductIDs(ctx, clientID)
				if err != nil {
					return nil, http.StatusInternalServerError, fmt.Errorf("failed to load purchases: %w", err)
				}
				for _, id := range ids {
					owned[id] = true
				}
			}
			item.Owned = owned[p.ProductID]
		}
		out = append(out, item)
	}
	return out, http.StatusOK, nil
}

// GetBillingAccount returns the obfuscated account id the app must pass to Play.
func GetBillingAccount(_ context.Context, clientID int) (*dto.BillingAccountDTO, int, error) {
	rt, err := getBilling()
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	return &dto.BillingAccountDTO{ObfuscatedAccountID: ObfuscatedAccountID(rt.secret, clientID)}, http.StatusOK, nil
}

// ---- Verification -------------------------------------------------------------

// VerifyPurchase checks a purchase with Google Play and, if it is genuine,
// paid and this player's, grants it exactly once. Repeating the call with the
// same token is safe and returns the current state.
func VerifyPurchase(ctx context.Context, clientID int, req *dto.VerifyPurchaseRequestDTO) (*dto.VerifyPurchaseResponseDTO, int, error) {
	if err := req.Validate(); err != nil {
		return nil, http.StatusBadRequest, err
	}
	rt, err := getBilling()
	if err != nil {
		return nil, http.StatusServiceUnavailable, err
	}
	th := tokenHash(req.PurchaseToken)
	product, err := repo.GetBillingProduct(ctx, req.ProductID)
	if errors.Is(err, repo.ErrBillingProductNotFound) {
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: req.ProductID, Action: "verify", Result: "rejected", Reason: "unknown product"})
		return nil, http.StatusBadRequest, errors.New("unknown product")
	}
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to load product: %w", err)
	}

	if product.Kind == models.BillingKindSubscription {
		return verifySubscription(ctx, rt, clientID, product, req.PurchaseToken, th)
	}
	// Gem packs (consumable) and topic packs (non-consumable) are one-time purchases.
	return verifyConsumable(ctx, rt, clientID, product, req.PurchaseToken, th)
}

// reject audits a refused verification and returns the client-facing error.
func reject(ctx context.Context, clientID int, th, productID, result, reason string, status int, msg string) (*dto.VerifyPurchaseResponseDTO, int, error) {
	audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: productID, Action: "verify", Result: result, Reason: reason})
	return nil, status, errors.New(msg)
}

// playLookupFailed maps a failed Google lookup to a response.
func playLookupFailed(ctx context.Context, clientID int, th, productID string, err error) (*dto.VerifyPurchaseResponseDTO, int, error) {
	if errors.Is(err, playstore.ErrInvalidToken) {
		return reject(ctx, clientID, th, productID, "rejected", err.Error(), http.StatusBadRequest, "purchase could not be verified")
	}
	audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: productID, Action: "verify", Result: "error", Reason: err.Error()})
	// The purchase stays pending in the app, which retries later.
	return nil, http.StatusBadGateway, errors.New("could not reach Google Play, please try again")
}

// checkOwner verifies the purchase carries this player's obfuscated account id.
// A different non-empty id means someone is redeeming another player's token.
func checkOwner(ctx context.Context, rt *billingRuntime, clientID int, th, productID, got string) (*dto.VerifyPurchaseResponseDTO, int, error) {
	want := ObfuscatedAccountID(rt.secret, clientID)
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
		return nil, 0, nil
	}
	if got == "" {
		return reject(ctx, clientID, th, productID, "rejected", "purchase has no account id", http.StatusForbidden, "this purchase is not linked to your account")
	}
	return reject(ctx, clientID, th, productID, "fraud", "account id belongs to another player", http.StatusForbidden, "this purchase belongs to a different account")
}

func verifyConsumable(ctx context.Context, rt *billingRuntime, clientID int, product *models.BillingProduct, token, th string) (*dto.VerifyPurchaseResponseDTO, int, error) {
	// A topic pack is kept for good: acknowledged with Google, never consumed.
	isPack := product.Kind == models.BillingKindNonConsumable
	p, err := rt.play.GetProduct(ctx, product.ProductID, token)
	if err != nil {
		return playLookupFailed(ctx, clientID, th, product.ProductID, err)
	}
	if p.ProductID != "" && p.ProductID != product.ProductID {
		return reject(ctx, clientID, th, product.ProductID, "rejected", "product mismatch: "+p.ProductID, http.StatusBadRequest, "purchase does not match the product")
	}
	switch p.PurchaseState {
	case playstore.PurchaseStatePurchased:
	case playstore.PurchaseStatePending:
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "verify", Result: "pending"})
		return &dto.VerifyPurchaseResponseDTO{Status: dto.VerifyStatusPending, ProductID: product.ProductID, Kind: product.Kind}, http.StatusAccepted, nil
	default:
		return reject(ctx, clientID, th, product.ProductID, "rejected", fmt.Sprintf("purchase state %d", p.PurchaseState), http.StatusBadRequest, "this purchase was cancelled")
	}
	if res, status, err := checkOwner(ctx, rt, clientID, th, product.ProductID, p.ObfuscatedAccountID); err != nil {
		return res, status, err
	}
	if p.IsTest && !rt.allowTest {
		return reject(ctx, clientID, th, product.ProductID, "rejected", "test purchase", http.StatusForbidden, "test purchases are not accepted")
	}
	if p.Consumed || (isPack && p.Acknowledged) {
		// Only this backend consumes or acknowledges. Done at Google but never
		// recorded here means it was redeemed some other way; never pay it out.
		if _, found, err := repo.PurchaseOwner(ctx, token); err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("failed to check purchase: %w", err)
		} else if !found {
			return reject(ctx, clientID, th, product.ProductID, "fraud", "consumed but never granted", http.StatusConflict, "this purchase was already used")
		}
	}

	qty := int(p.Quantity)
	if qty < 1 {
		qty = 1
	}
	if qty > maxPurchaseQuantity || (isPack && qty != 1) {
		return reject(ctx, clientID, th, product.ProductID, "rejected", fmt.Sprintf("quantity %d", qty), http.StatusBadRequest, "purchase quantity is not supported")
	}
	ref := p.OrderID
	if ref == "" {
		ref = "iap:" + th[:16]
	}
	grant := repo.ConsumableGrant{
		ClientID: clientID, ProductID: product.ProductID, PurchaseToken: token, OrderID: p.OrderID,
		Quantity: qty, Coins: product.Coins * qty, Gems: product.Gems * qty, IsTest: p.IsTest,
		PurchasedAt: p.PurchaseTime, LedgerRef: ref,
	}
	grant.Audit = &repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "grant", Result: "ok",
		Reason: fmt.Sprintf("kind=%s coins=%d gems=%d qty=%d test=%v order=%s", product.Kind, grant.Coins, grant.Gems, qty, p.IsTest, truncateStr(p.OrderID, 40))}
	res, err := repo.GrantConsumable(ctx, grant)
	if errors.Is(err, repo.ErrProfileNotFound) {
		if _, pErr := GetOrCreateProfile(ctx, clientID); pErr == nil {
			res, err = repo.GrantConsumable(ctx, grant)
		}
	}
	if errors.Is(err, repo.ErrPurchaseOwnedByOther) {
		return reject(ctx, clientID, th, product.ProductID, "fraud", "token already granted to another player", http.StatusConflict, "this purchase belongs to a different account")
	}
	if err != nil {
		// Nothing was granted and nothing consumed: the app keeps the purchase and retries.
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "grant", Result: "error", Reason: err.Error()})
		return nil, http.StatusInternalServerError, errors.New("failed to record purchase, please try again")
	}

	// Consume with Google only after the grant is committed. If this fails the
	// grant stands and the sweeper retries (Play refunds unconsumed purchases
	// after 3 days, so it must eventually succeed).
	// "consumed" on our row means "completed with Google": consumed for a pack
	// of gems, acknowledged for a topic pack.
	doneAtGoogle := p.Consumed || (isPack && p.Acknowledged)
	if !res.Revoked {
		switch {
		case doneAtGoogle && !res.Consumed:
			_ = repo.MarkPurchaseConsumed(ctx, token)
		case !doneAtGoogle && !res.Consumed:
			if err := completeWithGoogle(ctx, rt, isPack, product.ProductID, token); err != nil {
				audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "consume", Result: "error", Reason: err.Error()})
			} else if err := repo.MarkPurchaseConsumed(ctx, token); err != nil {
				log.Printf("[Billing] mark consumed failed token=%.8s: %v", th, err)
			}
		}
	}

	out := &dto.VerifyPurchaseResponseDTO{
		Status: dto.VerifyStatusGranted, ProductID: product.ProductID, Kind: product.Kind,
		Balance: &dto.WalletBalanceDTO{Coins: res.Coins, Gems: res.Gems},
	}
	if isPack && !res.Revoked {
		out.UnlockedSubjects = packSubjects(ctx, product.ProductID)
	}
	if res.AlreadyGranted {
		out.Status = dto.VerifyStatusAlreadyGranted
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "verify", Result: "already_granted"})
	} else {
		out.CoinsAdded, out.GemsAdded = grant.Coins, grant.Gems
	}
	return out, http.StatusOK, nil
}

// completeWithGoogle consumes a gem pack or acknowledges a topic pack.
func completeWithGoogle(ctx context.Context, rt *billingRuntime, isPack bool, productID, token string) error {
	if isPack {
		return rt.play.AcknowledgeProduct(ctx, productID, token)
	}
	return rt.play.ConsumeProduct(ctx, productID, token)
}

// mapSubscriptionState turns Google's state into ours. Unknown states map to
// expired: when in doubt, no access.
func mapSubscriptionState(state string) string {
	switch state {
	case "SUBSCRIPTION_STATE_ACTIVE":
		return models.SubStatusActive
	case "SUBSCRIPTION_STATE_CANCELED":
		return models.SubStatusCanceled
	case "SUBSCRIPTION_STATE_IN_GRACE_PERIOD":
		return models.SubStatusInGrace
	case "SUBSCRIPTION_STATE_ON_HOLD":
		return models.SubStatusOnHold
	case "SUBSCRIPTION_STATE_PAUSED":
		return models.SubStatusPaused
	case "SUBSCRIPTION_STATE_PENDING":
		return models.SubStatusPending
	default: // EXPIRED, PENDING_PURCHASE_CANCELED, unspecified
		return models.SubStatusExpired
	}
}

func verifySubscription(ctx context.Context, rt *billingRuntime, clientID int, product *models.BillingProduct, token, th string) (*dto.VerifyPurchaseResponseDTO, int, error) {
	s, err := rt.play.GetSubscription(ctx, token)
	if err != nil {
		return playLookupFailed(ctx, clientID, th, product.ProductID, err)
	}
	if !s.HasProduct(product.ProductID) {
		return reject(ctx, clientID, th, product.ProductID, "rejected", "product mismatch: "+strings.Join(s.ProductIDs, ","), http.StatusBadRequest, "purchase does not match the product")
	}
	if res, status, err := checkOwner(ctx, rt, clientID, th, product.ProductID, s.ObfuscatedAccountID); err != nil {
		return res, status, err
	}
	if s.IsTest && !rt.allowTest {
		return reject(ctx, clientID, th, product.ProductID, "rejected", "test purchase", http.StatusForbidden, "test purchases are not accepted")
	}
	status := mapSubscriptionState(s.State)
	if status == models.SubStatusPending {
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "verify", Result: "pending"})
		return &dto.VerifyPurchaseResponseDTO{Status: dto.VerifyStatusPending, ProductID: product.ProductID, Kind: product.Kind}, http.StatusAccepted, nil
	}

	existing, err := repo.GetSubscriptionByToken(ctx, token)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to check subscription: %w", err)
	}
	if err := storeSubscription(ctx, rt, clientID, product.ProductID, token, th, s, status); err != nil {
		if errors.Is(err, repo.ErrPurchaseOwnedByOther) {
			return reject(ctx, clientID, th, product.ProductID, "fraud", "subscription token already linked to another player", http.StatusConflict, "this purchase belongs to a different account")
		}
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "grant", Result: "error", Reason: err.Error()})
		return nil, http.StatusInternalServerError, errors.New("failed to record subscription, please try again")
	}

	st, err := subscriptionStatus(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	out := &dto.VerifyPurchaseResponseDTO{Status: dto.VerifyStatusGranted, ProductID: product.ProductID, Kind: product.Kind, Subscription: st}
	if existing != nil {
		out.Status = dto.VerifyStatusAlreadyGranted
	}
	audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: product.ProductID, Action: "grant", Result: "ok", Reason: "subscription " + status})
	return out, http.StatusOK, nil
}

// storeSubscription saves Google's view of the subscription and acknowledges
// it with Google (after the save) when it is paying and not yet acknowledged.
func storeSubscription(ctx context.Context, rt *billingRuntime, clientID int, productID, token, th string, s *playstore.SubscriptionPurchase, status string) error {
	row := models.ClientSubscription{
		PurchaseToken: token, ClientID: clientID, ProductID: productID,
		LinkedPurchaseToken: s.LinkedPurchaseToken, Status: status, ExpiresAt: s.ExpiresAt,
		AutoRenewing: s.AutoRenewing, Acknowledged: s.Acknowledged, LatestOrderID: s.LatestOrderID, TestPurchase: s.IsTest,
	}
	if err := repo.UpsertSubscription(ctx, row); err != nil {
		return err
	}
	if !s.Acknowledged && models.SubscriptionGrantsAccess(status) {
		if err := rt.play.AcknowledgeSubscription(ctx, productID, token); err != nil {
			audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: productID, Action: "acknowledge", Result: "error", Reason: err.Error()})
		} else if err := repo.MarkSubscriptionAcknowledged(ctx, token); err != nil {
			log.Printf("[Billing] mark acknowledged failed token=%.8s: %v", th, err)
		}
	}
	return nil
}

// ---- Premium status --------------------------------------------------------------

func subscriptionStatus(ctx context.Context, clientID int) (*dto.SubscriptionStatusDTO, error) {
	subs, err := repo.ListClientSubscriptions(ctx, clientID)
	if err != nil {
		return nil, err
	}
	now := billingNow()
	var best, latest *models.ClientSubscription
	for i := range subs {
		s := &subs[i]
		if s.Status == models.SubStatusSuperseded {
			continue
		}
		if latest == nil || (s.ExpiresAt != nil && (latest.ExpiresAt == nil || s.ExpiresAt.After(*latest.ExpiresAt))) {
			latest = s
		}
		if models.SubscriptionGrantsAccess(s.Status) && s.ExpiresAt != nil && s.ExpiresAt.After(now) {
			if best == nil || s.ExpiresAt.After(*best.ExpiresAt) {
				best = s
			}
		}
	}
	out := &dto.SubscriptionStatusDTO{}
	pick := best
	if pick == nil {
		pick = latest
	}
	if pick != nil {
		out.ProductID, out.Status, out.AutoRenewing = pick.ProductID, pick.Status, pick.AutoRenewing
		if pick.ExpiresAt != nil {
			t := pick.ExpiresAt.UTC()
			out.ExpiresAt = &t
		}
	}
	out.IsPremium = best != nil
	return out, nil
}

// GetSubscriptionStatus reports whether the player has Premium now.
func GetSubscriptionStatus(ctx context.Context, clientID int) (*dto.SubscriptionStatusDTO, int, error) {
	st, err := subscriptionStatus(ctx, clientID)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("failed to read subscription: %w", err)
	}
	return st, http.StatusOK, nil
}

// IsPremium is the server-side entitlement check for Premium perks. Always use
// this, never a flag sent by the app.
func IsPremium(ctx context.Context, clientID int) (bool, error) {
	st, err := subscriptionStatus(ctx, clientID)
	if err != nil {
		return false, err
	}
	return st.IsPremium, nil
}

// ---- Real-time Developer Notifications (Pub/Sub push) ----------------------------

// ErrRTDNUnauthorized means a notification failed authentication.
var ErrRTDNUnauthorized = errors.New("unauthorized")

// AuthenticateRTDN checks a Pub/Sub push request: the shared ?token= secret and
// the Google-signed OIDC token in the Authorization header (audience and
// service-account email must match the configuration).
func AuthenticateRTDN(ctx context.Context, authHeader, queryToken string) (int, error) {
	cfg := config.AppConfig
	if cfg == nil || len(cfg.Billing.RTDNProblems()) > 0 {
		return http.StatusServiceUnavailable, ErrBillingDisabled
	}
	b := cfg.Billing
	if subtle.ConstantTimeCompare([]byte(queryToken), []byte(b.RTDNToken)) != 1 {
		return http.StatusUnauthorized, ErrRTDNUnauthorized
	}
	jwt, ok := strings.CutPrefix(authHeader, "Bearer ")
	if !ok || strings.TrimSpace(jwt) == "" {
		return http.StatusUnauthorized, ErrRTDNUnauthorized
	}
	payload, err := rtdnTokenValidator(ctx, strings.TrimSpace(jwt), b.RTDNAudience)
	if err != nil {
		log.Printf("[Billing] RTDN token rejected: %v", err)
		return http.StatusUnauthorized, ErrRTDNUnauthorized
	}
	if payload.Issuer != "accounts.google.com" && payload.Issuer != "https://accounts.google.com" {
		return http.StatusUnauthorized, ErrRTDNUnauthorized
	}
	email, _ := payload.Claims["email"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	if !verified || !strings.EqualFold(email, b.RTDNServiceAccountEmail) {
		return http.StatusUnauthorized, ErrRTDNUnauthorized
	}
	return http.StatusOK, nil
}

type pubsubPush struct {
	Message struct {
		Data       string `json:"data"`
		MessageID  string `json:"messageId"`
		MessageID2 string `json:"message_id"`
	} `json:"message"`
}

type developerNotification struct {
	PackageName    string `json:"packageName"`
	OneTimeProduct *struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		Sku              string `json:"sku"`
	} `json:"oneTimeProductNotification"`
	Subscription *struct {
		NotificationType int    `json:"notificationType"`
		PurchaseToken    string `json:"purchaseToken"`
		SubscriptionID   string `json:"subscriptionId"`
	} `json:"subscriptionNotification"`
	Voided *struct {
		PurchaseToken string `json:"purchaseToken"`
		OrderID       string `json:"orderId"`
		ProductType   int    `json:"productType"`
	} `json:"voidedPurchaseNotification"`
	Test *struct {
		Version string `json:"version"`
	} `json:"testNotification"`
}

// HandleRTDN processes one authenticated Pub/Sub push. The notification body
// is only a hint: every change is re-read from the Play API before it is applied.
// A 2xx tells Pub/Sub the message is done; a 5xx makes it retry.
func HandleRTDN(ctx context.Context, body []byte) (int, error) {
	var push pubsubPush
	if err := json.Unmarshal(body, &push); err != nil {
		return http.StatusBadRequest, errors.New("invalid push body")
	}
	msgID := push.Message.MessageID
	if msgID == "" {
		msgID = push.Message.MessageID2
	}
	if msgID == "" || len(msgID) > 100 {
		return http.StatusBadRequest, errors.New("missing message id")
	}
	data, err := base64.StdEncoding.DecodeString(push.Message.Data)
	if err != nil {
		return http.StatusBadRequest, errors.New("invalid message data")
	}
	var n developerNotification
	if err := json.Unmarshal(data, &n); err != nil {
		return http.StatusBadRequest, errors.New("invalid notification")
	}

	rt, err := getBilling()
	if err != nil {
		return http.StatusServiceUnavailable, err
	}
	if n.PackageName != rt.pkg {
		audit(ctx, repo.BillingAudit{Action: "rtdn", Result: "rejected", Reason: "wrong package " + truncateStr(n.PackageName, 80)})
		return http.StatusOK, nil // acknowledge: retrying cannot fix it
	}
	seen, err := repo.RTDNMessageSeen(ctx, msgID)
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if seen {
		return http.StatusOK, nil
	}

	switch {
	case n.Voided != nil:
		err = revokePurchase(ctx, n.Voided.PurchaseToken, "rtdn")
	case n.Subscription != nil:
		err = refreshSubscription(ctx, rt, n.Subscription.PurchaseToken)
	case n.OneTimeProduct != nil:
		// The app verifies its own one-time purchases (and retries on launch);
		// a notification cannot say which player bought it, so just record it.
		audit(ctx, repo.BillingAudit{TokenHash: tokenHash(n.OneTimeProduct.PurchaseToken), ProductID: n.OneTimeProduct.Sku,
			Action: "rtdn", Result: "ok", Reason: fmt.Sprintf("one-time notification %d", n.OneTimeProduct.NotificationType)})
	case n.Test != nil:
		audit(ctx, repo.BillingAudit{Action: "rtdn", Result: "ok", Reason: "test notification"})
	}
	if err != nil {
		log.Printf("[Billing] RTDN %s failed: %v", msgID, err)
		return http.StatusInternalServerError, errors.New("notification not processed")
	}
	if err := repo.RecordRTDNMessage(ctx, msgID); err != nil {
		log.Printf("[Billing] RTDN %s dedupe write failed: %v", msgID, err)
	}
	return http.StatusOK, nil
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// refreshSubscription re-reads a subscription from Google and stores it for
// the player who owns it (known token, or the token it replaced). Unknown
// tokens wait for the app to verify them.
func refreshSubscription(ctx context.Context, rt *billingRuntime, token string) error {
	if token == "" {
		return nil
	}
	th := tokenHash(token)
	s, err := rt.play.GetSubscription(ctx, token)
	if errors.Is(err, playstore.ErrInvalidToken) {
		audit(ctx, repo.BillingAudit{TokenHash: th, Action: "rtdn", Result: "rejected", Reason: err.Error()})
		return nil
	}
	if err != nil {
		return err
	}
	existing, err := repo.GetSubscriptionByToken(ctx, token)
	if err != nil {
		return err
	}
	clientID, productID := 0, ""
	if existing != nil {
		clientID, productID = existing.ClientID, existing.ProductID
	} else if s.LinkedPurchaseToken != "" {
		old, err := repo.GetSubscriptionByToken(ctx, s.LinkedPurchaseToken)
		if err != nil {
			return err
		}
		if old != nil {
			clientID = old.ClientID
		}
	}
	if clientID == 0 {
		audit(ctx, repo.BillingAudit{TokenHash: th, Action: "rtdn", Result: "ignored", Reason: "unknown subscription token"})
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(s.ObfuscatedAccountID), []byte(ObfuscatedAccountID(rt.secret, clientID))) != 1 {
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, Action: "rtdn", Result: "fraud", Reason: "account id does not match owner"})
		return nil
	}
	if s.IsTest && !rt.allowTest {
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, Action: "rtdn", Result: "rejected", Reason: "test purchase"})
		return nil
	}
	if productID == "" || !s.HasProduct(productID) {
		productID = ""
		for _, id := range s.ProductIDs {
			if p, err := repo.GetBillingProduct(ctx, id); err == nil && p.Kind == models.BillingKindSubscription {
				productID = id
				break
			}
		}
		if productID == "" {
			audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, Action: "rtdn", Result: "rejected", Reason: "no known subscription product"})
			return nil
		}
	}
	status := mapSubscriptionState(s.State)
	if err := storeSubscription(ctx, rt, clientID, productID, token, th, s, status); err != nil {
		if errors.Is(err, repo.ErrPurchaseOwnedByOther) {
			audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: productID, Action: "rtdn", Result: "fraud", Reason: "token linked to another player"})
			return nil
		}
		return err
	}
	audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, ProductID: productID, Action: "rtdn", Result: "ok", Reason: "subscription " + status})
	return nil
}

// revokePurchase takes back a refunded or charged-back purchase (pack or
// subscription). Idempotent.
func revokePurchase(ctx context.Context, token, source string) error {
	if token == "" {
		return nil
	}
	th := tokenHash(token)
	// Gem packs lose what they gave and topic packs their access; the refund
	// and any shortfall are audited in the same transaction.
	r, err := repo.RevokeConsumable(ctx, token, th, source)
	if err != nil {
		return err
	}
	if r.Found {
		if r.CoinsShort > 0 || r.GemsShort > 0 {
			log.Printf("[Billing] refund shortfall client=%d product=%s coins=%d gems=%d", r.ClientID, r.ProductID, r.CoinsShort, r.GemsShort)
		}
		return nil
	}
	if r.AlreadyRevoked {
		return nil
	}
	found, clientID, err := repo.RevokeSubscription(ctx, token)
	if err != nil {
		return err
	}
	if found {
		audit(ctx, repo.BillingAudit{ClientID: clientID, TokenHash: th, Action: "refund", Result: "ok", Reason: source + ": subscription revoked"})
	}
	return nil
}

// ---- Reconciliation sweeper ----------------------------------------------------------

// StartBillingSweeper periodically (1) retries consuming granted packs and
// acknowledging subscriptions that Google has not confirmed, and (2) reads
// Google's voided-purchases list as a backup for lost refund notifications.
// Every step is idempotent, so running it on several instances is safe.
func StartBillingSweeper(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		// Wait a little after boot: cold starts must stay fast.
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Minute):
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			RunBillingSweep(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// RunBillingSweep runs one reconciliation pass. Does nothing while billing is disabled.
func RunBillingSweep(ctx context.Context) {
	rt, err := getBilling()
	if err != nil {
		return
	}
	cutoff := time.Now().UTC().Add(-2 * time.Minute) // leave in-flight verifies alone

	if pending, err := repo.ListUnconsumedPurchases(ctx, cutoff, 50); err != nil {
		log.Printf("[BillingSweeper] list unconsumed: %v", err)
	} else {
		for _, p := range pending {
			th := tokenHash(p.PurchaseToken)
			isPack := p.Kind == models.BillingKindNonConsumable
			if err := completeWithGoogle(ctx, rt, isPack, p.ProductID, p.PurchaseToken); err != nil {
				// Maybe it was completed already; ask Google.
				if gp, gerr := rt.play.GetProduct(ctx, p.ProductID, p.PurchaseToken); gerr == nil && (gp.Consumed || (isPack && gp.Acknowledged)) {
					_ = repo.MarkPurchaseConsumed(ctx, p.PurchaseToken)
					continue
				}
				audit(ctx, repo.BillingAudit{ClientID: p.ClientID, TokenHash: th, ProductID: p.ProductID, Action: "consume", Result: "error", Reason: err.Error()})
				continue
			}
			_ = repo.MarkPurchaseConsumed(ctx, p.PurchaseToken)
		}
	}

	if subs, err := repo.ListUnacknowledgedSubscriptions(ctx, cutoff, 50); err != nil {
		log.Printf("[BillingSweeper] list unacknowledged: %v", err)
	} else {
		for _, s := range subs {
			th := tokenHash(s.PurchaseToken)
			if err := rt.play.AcknowledgeSubscription(ctx, s.ProductID, s.PurchaseToken); err != nil {
				if gs, gerr := rt.play.GetSubscription(ctx, s.PurchaseToken); gerr == nil && gs.Acknowledged {
					_ = repo.MarkSubscriptionAcknowledged(ctx, s.PurchaseToken)
					continue
				}
				audit(ctx, repo.BillingAudit{ClientID: s.ClientID, TokenHash: th, ProductID: s.ProductID, Action: "acknowledge", Result: "error", Reason: err.Error()})
				continue
			}
			_ = repo.MarkSubscriptionAcknowledged(ctx, s.PurchaseToken)
		}
	}

	voided, err := rt.play.ListVoided(ctx, time.Now().UTC().Add(-72*time.Hour))
	if err != nil {
		log.Printf("[BillingSweeper] list voided purchases: %v", err)
	}
	for _, v := range voided {
		if err := revokePurchase(ctx, v.PurchaseToken, "voided-list"); err != nil {
			log.Printf("[BillingSweeper] revoke token=%.8s: %v", tokenHash(v.PurchaseToken), err)
		}
	}
}
