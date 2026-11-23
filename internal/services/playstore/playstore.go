// Package playstore talks to the Google Play Developer API. It is the only
// source of truth for whether a purchase was really paid for: the backend never
// trusts what the app says about a purchase, only what Google returns here.
package playstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/androidpublisher/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// ErrInvalidToken means Google does not know the purchase token (for this
// package and product). It is never retried: the purchase is not real.
var ErrInvalidToken = errors.New("purchase token not recognised by Google Play")

// One-time purchase states (ProductPurchase.purchaseState).
const (
	PurchaseStatePurchased = 0
	PurchaseStateCanceled  = 1
	PurchaseStatePending   = 2
)

// ProductPurchase is what Google reports about a one-time (consumable) purchase.
type ProductPurchase struct {
	ProductID           string
	OrderID             string
	PurchaseState       int64
	Consumed            bool
	Acknowledged        bool
	ObfuscatedAccountID string
	PurchaseTime        time.Time
	Quantity            int64
	IsTest              bool // license tester purchase: not charged
}

// SubscriptionPurchase is what Google reports about a subscription token.
type SubscriptionPurchase struct {
	ProductIDs          []string
	State               string // SUBSCRIPTION_STATE_* from Google
	ExpiresAt           *time.Time
	AutoRenewing        bool
	Acknowledged        bool
	LinkedPurchaseToken string
	LatestOrderID       string
	ObfuscatedAccountID string
	IsTest              bool
}

// HasProduct reports whether the subscription contains productID.
func (s *SubscriptionPurchase) HasProduct(productID string) bool {
	for _, id := range s.ProductIDs {
		if id == productID {
			return true
		}
	}
	return false
}

// VoidedPurchase is a refunded, charged-back or revoked purchase.
type VoidedPurchase struct {
	PurchaseToken string
	OrderID       string
	VoidedAt      time.Time
}

// Client is the subset of the Play Developer API the backend uses. Tests use a fake.
type Client interface {
	GetProduct(ctx context.Context, productID, token string) (*ProductPurchase, error)
	ConsumeProduct(ctx context.Context, productID, token string) error
	GetSubscription(ctx context.Context, token string) (*SubscriptionPurchase, error)
	AcknowledgeSubscription(ctx context.Context, productID, token string) error
	ListVoided(ctx context.Context, since time.Time) ([]VoidedPurchase, error)
}

// callTimeout bounds every call to Google so a slow API cannot hold a request
// past the Catalyst proxy's 30s idle timeout.
const callTimeout = 10 * time.Second

type apiClient struct {
	svc         *androidpublisher.Service
	packageName string
}

// NewClient builds a Play Developer API client from a service-account key.
func NewClient(ctx context.Context, packageName string, serviceAccountJSON []byte) (Client, error) {
	svc, err := androidpublisher.NewService(ctx,
		option.WithAuthCredentialsJSON(option.ServiceAccount, serviceAccountJSON),
		option.WithScopes(androidpublisher.AndroidpublisherScope),
	)
	if err != nil {
		return nil, fmt.Errorf("create android publisher client: %w", err)
	}
	return &apiClient{svc: svc, packageName: packageName}, nil
}

// mapErr turns "this token does not exist" answers into ErrInvalidToken and
// keeps everything else as a (retryable) error.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case http.StatusNotFound, http.StatusGone:
			return fmt.Errorf("%w: %s", ErrInvalidToken, gerr.Message)
		case http.StatusBadRequest:
			msg := strings.ToLower(gerr.Message)
			for _, e := range gerr.Errors {
				msg += " " + strings.ToLower(e.Reason)
			}
			if strings.Contains(msg, "token") || strings.Contains(msg, "invalid") {
				return fmt.Errorf("%w: %s", ErrInvalidToken, gerr.Message)
			}
		}
		return fmt.Errorf("google play api: %d %s", gerr.Code, gerr.Message)
	}
	return fmt.Errorf("google play api: %w", err)
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func (c *apiClient) GetProduct(ctx context.Context, productID, token string) (*ProductPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	p, err := c.svc.Purchases.Products.Get(c.packageName, productID, token).Context(ctx).Do()
	if err != nil {
		return nil, mapErr(err)
	}
	return &ProductPurchase{
		ProductID:           p.ProductId,
		OrderID:             p.OrderId,
		PurchaseState:       p.PurchaseState,
		Consumed:            p.ConsumptionState == 1,
		Acknowledged:        p.AcknowledgementState == 1,
		ObfuscatedAccountID: p.ObfuscatedExternalAccountId,
		PurchaseTime:        time.UnixMilli(p.PurchaseTimeMillis).UTC(),
		Quantity:            p.Quantity,
		// purchaseType is only present for non-standard purchases; 0 = test.
		IsTest: p.PurchaseType != nil && *p.PurchaseType == 0,
	}, nil
}

func (c *apiClient) ConsumeProduct(ctx context.Context, productID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return mapErr(c.svc.Purchases.Products.Consume(c.packageName, productID, token).Context(ctx).Do())
}

func (c *apiClient) GetSubscription(ctx context.Context, token string) (*SubscriptionPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	s, err := c.svc.Purchases.Subscriptionsv2.Get(c.packageName, token).Context(ctx).Do()
	if err != nil {
		return nil, mapErr(err)
	}
	out := &SubscriptionPurchase{
		State:               s.SubscriptionState,
		Acknowledged:        s.AcknowledgementState == "ACKNOWLEDGEMENT_STATE_ACKNOWLEDGED",
		LinkedPurchaseToken: s.LinkedPurchaseToken,
		IsTest:              s.TestPurchase != nil,
	}
	if s.ExternalAccountIdentifiers != nil {
		out.ObfuscatedAccountID = s.ExternalAccountIdentifiers.ObfuscatedExternalAccountId
	}
	for _, li := range s.LineItems {
		if li == nil {
			continue
		}
		out.ProductIDs = append(out.ProductIDs, li.ProductId)
		if exp := parseTime(li.ExpiryTime); exp != nil && (out.ExpiresAt == nil || exp.After(*out.ExpiresAt)) {
			out.ExpiresAt = exp
			out.LatestOrderID = li.LatestSuccessfulOrderId
		}
		if li.AutoRenewingPlan != nil && li.AutoRenewingPlan.AutoRenewEnabled {
			out.AutoRenewing = true
		}
	}
	return out, nil
}

func (c *apiClient) AcknowledgeSubscription(ctx context.Context, productID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return mapErr(c.svc.Purchases.Subscriptions.Acknowledge(c.packageName, productID, token,
		&androidpublisher.SubscriptionPurchasesAcknowledgeRequest{}).Context(ctx).Do())
}

func (c *apiClient) ListVoided(ctx context.Context, since time.Time) ([]VoidedPurchase, error) {
	var out []VoidedPurchase
	pageToken := ""
	for page := 0; page < 20; page++ { // hard stop: at most 20 pages per sweep
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		// Type 1 = one-time purchases and subscriptions.
		call := c.svc.Purchases.Voidedpurchases.List(c.packageName).
			StartTime(since.UnixMilli()).Type(1).MaxResults(1000).Context(callCtx)
		if pageToken != "" {
			call = call.Token(pageToken)
		}
		res, err := call.Do()
		cancel()
		if err != nil {
			return out, mapErr(err)
		}
		for _, v := range res.VoidedPurchases {
			if v == nil || v.PurchaseToken == "" {
				continue
			}
			out = append(out, VoidedPurchase{
				PurchaseToken: v.PurchaseToken,
				OrderID:       v.OrderId,
				VoidedAt:      time.UnixMilli(v.VoidedTimeMillis).UTC(),
			})
		}
		if res.TokenPagination == nil || res.TokenPagination.NextPageToken == "" {
			break
		}
		pageToken = res.TokenPagination.NextPageToken
	}
	return out, nil
}
