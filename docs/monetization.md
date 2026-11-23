# Monetization: Quiz Pass, gem packs, topic packs, rewarded ads and server-charged power-ups

What was built, how money and rewards move, and what has to be set up before release.
Covers the Go backend (this repo), the schema (`SkillVerse-SQL_Migration` migration 000010) and the Flutter app (`Gamified-learning`).

## 1. What is in this release

| Feature | What the player gets | Where it is enforced |
|---|---|---|
| **Quiz Pass** (subscription `quiz_pass`, base plans `monthly`, `yearly`) | Rewards without watching ads, 60 gems each calendar month, one extra streak-freeze slot | Server: `client_subscriptions` + `services.IsPremium` |
| **Gem packs** (consumables `gems_20` … `gems_1400`) | Gems | Server: verified with Google Play, then credited |
| **Topic packs** (non-consumables, e.g. `pack_gk`) | Permanent access to the subjects mapped to the pack | Server: checked at every game start |
| **Ad chest** | Coins, sometimes a 50:50 token, rarely 3 gems | Server: prize rolled and paid after Google's signed ad callback |
| **Power-up tokens by ad** | One 50:50, +5 seconds or Skip token per ad | Server: paid after Google's signed ad callback |
| **50:50** (MCQ) | Hides two wrong options, once per question, 20 coins or a token | Server: charged inside `POST /quiz/power-ups/fifty-fifty` |
| **+5 seconds** (Sudden Death) | Adds 5 s to the live question, once per run, 12 coins or a token | Server: charged inside `POST /quiz/sudden-death/power-up` |
| **Skip** (Sudden Death) | Skips a question safely, once per run, 35 coins or a token | Server: charged inside `POST /quiz/sudden-death/answer` with `option: "skip"` |

Prices for Play products are set in Play Console only. Suggested: gem packs ₹10–499, Quiz Pass ₹49–99 a month or ₹499–799 a year, topic packs ₹29–99.

Not in this release (see section 13): age band at signup, AI explanations for the Pass, server-side earned chests and spin wheel, Razorpay, team plans.

## 2. The rules everything follows

1. **Only the server grants value.** The app never credits coins, gems, tokens, access or Premium. For a purchase it forwards the purchase token; for an ad it waits for the server to report that Google's signed callback arrived.
2. **Every movement of value is one `wallet_ledger` row**, written in the same database transaction as the balance change. Coins, gems and power-up tokens (`fifty_fifty`, `add_time`, `skip`) are all ledger currencies.
3. **Every ad reward is one `ad_rewards` row**, from request to Google's callback to payout. Quiz Pass claims of the same rewards are rows too (`source = 'pass'`).
4. **Retries never pay or charge twice.** Each grant or charge has a unique key: purchase token, ad reward id plus Google transaction id, or session + question + power-up. The partial unique index `uq_wallet_ledger_server_event` backs this up in the ledger itself.
5. **Fail closed.** Missing configuration means 503, never "accept everything". An unknown subscription state means no access, and a refunded purchase loses its grant.

## 3. Products

Catalog: `billing_products` (seed `SkillVerse-SQL_Migration/seeds/sql/70_billing_products.sql`). Each `product_id` must exist in Play Console with the same id.

| Product id | Kind | Gives |
|---|---|---|
| `gems_20`, `gems_110`, `gems_240`, `gems_650`, `gems_1400` | consumable | That many gems |
| `quiz_pass` | subscription (base plans `monthly`, `yearly`) | Quiz Pass perks while active |
| `pack_*` (added per topic pack) | non_consumable | Subjects in `billing_product_subjects` |
| `coins_500`, `coins_1200`, `coins_3000`, `gems_50`, `gems_150` | consumable, **inactive** | Retired: coins stay free to earn; gems are what is sold |

How to add a topic pack:

1. Add its subjects to `Sheets Dump/catalog/subjects.csv`.
2. Add the product and its mappings to `seeds/sql/75_topic_packs.sql`. The template is in the file.
3. Create the same product id in Play Console.

The seed fails if an active pack unlocks no subject, so an empty pack can never be sold. The backend also hides packs with no subjects from `GET /billing/products`.

## 4. Flows

### 4.1 Buying through Google Play (gems, topic packs, Quiz Pass)

```
App                                   Backend                                Google Play
 │ GET /billing/products ───────────▶ │ active catalog (packs only if they unlock subjects)
 │ GET /billing/account-id ─────────▶ │ HMAC(client id) = obfuscatedAccountId
 │ launch purchase (accountId) ───────────────────────────────────────────▶ │
 │ ◀── purchaseStream: purchased, token ───────────────────────────────────── │
 │ POST /billing/verify {product_id, purchase_token} ▶ │
 │                                    │ products.get / subscriptionsv2.get ─▶ │
 │                                    │ checks: purchased, product, account id,
 │                                    │ test purchase, quantity, not redeemed elsewhere
 │                                    │ one transaction: play_purchases (UNIQUE token)
 │                                    │   + balance + wallet_ledger + billing_audit
 │                                    │ then consume (gems) / acknowledge (pack, Pass) ▶ │
 │ ◀── 200 granted / already_granted / 202 pending
 │ completePurchase (only after 200)
```

- **Rejected purchase** (400, 403 or 409): the app leaves it unfinished, so Google refunds it automatically after 3 days.
- **Offline or server error:** the purchase stays unfinished in Google Play. When the app next starts, `startBillingWhenSignedIn` (in `main.dart`) asks Play to report unfinished purchases and verifies them again. The server's answer is idempotent.
- **Consume or acknowledge fails after the grant:** the grant stands. The hourly sweeper (`RunBillingSweep`) retries, and it acknowledges topic packs rather than consuming them.

### 4.2 Quiz Pass lifecycle

- Google sends Real-time Developer Notifications through a Pub/Sub push to `POST /billing/rtdn`. That request needs a Google-signed OIDC token with the right audience and service-account email, plus the `?token=` secret.
- Every notification is re-read from the Play API before anything changes, and each message is handled once (`billing_rtdn_messages`).
- Premium is computed on every request from `client_subscriptions` (status `active`, `in_grace` or `canceled`, and `expires_at` in the future).
- An upgrade or resubscribe token marks the token it replaces as `superseded`. A refunded subscription is `revoked`, and later refreshes cannot reactivate it.

### 4.3 Refunds and chargebacks

- A voided-purchase notification, or the hourly `voidedpurchases.list` backup, revokes the purchase:
  - Gem packs: the gems are taken back, never below zero, with an `iap_refund` ledger row.
  - Topic packs: access ends.
  - Subscriptions: status becomes `revoked`.
- When the player already spent the refunded gems, a `billing_audit` row with result `shortfall` flags the account for review.

### 4.4 Rewarded ads (AdMob server-side verification)

```
App                                   Backend                                 Google AdMob
 │ tap "Watch ad"                      │
 │ POST /ads/rewards {placement} ────▶ │ caps, cooldown, token cap → ad_rewards row (pending)
 │ ◀── {id, ssv_user_id, ad_required}  │   (Quiz Pass: paid now, source = 'pass', no ad)
 │ RewardedAd.load + setServerSideOptions(userId = ssv_user_id, customData = id)
 │ show → user watches to the end ───────────────────────────────────────────▶ │
 │                                     │ ◀── GET /ads/ssv?…&signature&key_id ────── │
 │                                     │ ECDSA verify with Google's published keys,
 │                                     │ ad unit is ours, user id = HMAC(owner),
 │                                     │ pending and not expired, transaction id unused
 │                                     │ one transaction: ad_rewards → fulfilled
 │                                     │   + balance/token + wallet_ledger
 │ GET /ads/rewards/{id} (polls ~17 s) ▶│
 │ ◀── fulfilled: "You got 40 coins!"
```

- A callback with a bad or unknown signature gets 403 and is never paid. Valid but unusable callbacks (Google's console test, a foreign ad unit, expired, duplicate) get 200 so Google does not retry, and pay nothing.
- When the callback is slow, the app says "your reward will arrive in a moment". The server pays when the callback arrives, up to 1 hour after the request.
- A token the player can no longer hold (5 per kind) is paid as 20 coins instead.
- Ad chest odds, in parts per million: 20 coins 40%, 40 coins 30%, 60 coins 15%, a 50:50 token 10%, 3 gems 5%. The roll uses `crypto/rand`.

### 4.5 Power-ups in a game

| Power-up | Mode | Price | Limit | Endpoint |
|---|---|---|---|---|
| `fifty_fifty` | MCQ | 20 coins | once per question | `POST /quiz/power-ups/fifty-fifty` (`pay_with`) |
| `add_time` | Sudden Death | 12 coins | once per run | `POST /quiz/sudden-death/power-up` (`pay_with`) |
| `skip` | Sudden Death | 35 coins | once per run | `POST /quiz/sudden-death/answer` with `option: "skip"` (`pay_with`) |

- **Choosing how to pay.** `pay_with` is `coins`, `token`, or empty. Empty uses a token if the player has one, otherwise coins. Old app versions send nothing, so they now pay instead of getting power-ups free.
- **Failed charges.** The game state is unchanged: the clock does not move and the question stays live. The response carries the error code `insufficient_funds` or `no_power_up_token`. On HTTP the MCQ endpoint returns 402.
- **Retries.** The payment, the ledger row and the `power_up_uses` row are one transaction. A retried request returns the stored result, so 50:50 hides the same options, and nothing is charged again.
- **WebSocket path.** The legacy socket game charges the same way, so it cannot be used to get power-ups free.
- **Other power-ups.** Hint, Reveal, Undo, Auto-match and Shuffle are still debited by the app through `/wallet/debit`. A debit can only take coins away from the player, never add any.

### 4.6 Topic packs

- `CheckTopicAccess` runs on `POST /quiz/sessions/create` and `GET /topics/:id/questions`. A pack subject the player does not own returns **402** with the packs that unlock it. 403 would read as "signed out" in the app.
- `GET /subjects` marks `locked` and `pack_product_ids` for each player.
- The pack → subject map is cached in-process for 5 minutes, so free subjects cost no query.
- If migration 000010 has not been applied yet, there are no packs and nothing is locked. Any other database error fails closed (503).

### 4.7 Quiz Pass perks

- **Ad-free rewards:** `POST /ads/rewards` pays immediately, with the same daily caps.
- **Monthly gems:** `POST /pass/monthly-gems` pays 60 gems once per calendar month (UTC), as a `pass_reward` ledger row with reference `pass_gems:YYYY-MM`.
- **Extra streak-freeze slot:** a Pass holder may hold 3 freezes instead of 2.

## 5. Security model

| Threat | Defense |
|---|---|
| Modified app grants itself gems, Premium or access | The app never grants. Everything is credited only after Google confirms it, server to server. |
| Replaying or sharing a purchase token | `play_purchases.purchase_token` is UNIQUE, and the purchase must carry this player's `obfuscatedAccountId`, an HMAC that cannot be forged without the server secret. A mismatch is audited as `fraud`. |
| Faking "I watched an ad" | Rewards are paid only for AdMob callbacks with a valid ECDSA signature (Google's rotating public keys), our ad unit, the owner's HMAC user id and an unused transaction id. |
| Calling the reward endpoint in a loop | Daily caps per placement (chest 3, 50:50 3, +5 s 3, Skip 2), 8 per day in total, a 30 s cooldown, a 5-token cap, a per-player rate limit, and a row lock against races. |
| Free power-ups through a modified app | The power-up endpoints charge on the server, in one transaction with the use record. The socket path charges too. |
| Refund after spending | Gems are taken back; any shortfall is flagged in `billing_audit`. |
| Forged Pub/Sub notification | OIDC JWT (audience and service-account email) plus a shared `?token=` secret, compared in constant time. The body is never trusted: state is re-read from the Play API. |
| Test purchases in production | Rejected unless `GOOGLE_PLAY_ALLOW_TEST_PURCHASES=true`. |
| Leaked secrets | Service-account key, HMAC secrets and the RTDN token are env vars only, never in `.env`. Raw purchase tokens are never logged; the audit log stores SHA-256 hashes. |
| Forged login tokens through the public fallback JWT secret | Production logs a `SECURITY WARNING` on every start while `JWT_SECRET` is missing, the public default or shorter than 32 characters, and while `PASSWORD_SECRET` is the public default. It does not refuse to start: that took the API down while Catalyst's secret was unsafe. Set a private `JWT_SECRET` in Catalyst. |
| Self-credit through `/wallet/credit` | Restricted earlier to `spin_wheel` and `chest_reward_*` with per-credit and daily caps (`dto.ClientRewardRules`). Coin packs and IAP reasons are refused. See section 13. |

## 6. Ads that do not annoy

- **Rewarded ads only.** There are no banners, interstitials or app-open ads, and every ad starts from a tap on "Watch ad" that names the reward and how many are left today.
- **Never during a game.** Ads are offered in Shop › Gems & Pass › Free rewards and in Chest Box › Free ad chest. Sudden Death runs on a server clock, so an ad mid-question would cost the player the question.
- **Small daily caps and a cooldown.** The caps limit farming, and once a reward is used up the button says so ("Come back tomorrow").
- **Quiz Pass holders never see an ad.** They get the same rewards with a "Claim" button.
- **No penalty for closing early.** An ad closed before the end gives nothing, but the request is kept, so the next tap reuses it rather than counting again.
- **One switch turns ads off.** `ADS_ENABLED=false` hides every ad option without an app release; Quiz Pass claims keep working.

## 7. Children and privacy

India's DPDP Act treats everyone under 18 as a child: targeted ads and behavioural tracking of children are not allowed, and verifiable parental consent applies from 12 May 2027. Until the app collects and verifies an age band, **every player is treated as under 18**. The server sends this policy in `GET /ads/config`:

- **Non-personalised ads:** `non_personalized: true`, which the app passes as `AdRequest(nonPersonalizedAds: true)`.
- **Content rating capped at G:** `max_ad_content_rating: "G"`, from `ADS_MAX_CONTENT_RATING`.
- **Under-18 age treatment:** `age_treatment: "teen"`, from `ADS_AGE_TREATMENT`. Set it to `child` if the Play listing targets under-13s.
- **Tagged under the age of consent:** the app tags its Google UMP consent request this way, and does not request ads where consent is required but not given.

Sensitive ad categories must also be blocked in the AdMob console (Blocking controls). That is a console setting, not code.

## 8. Data model and logging

**New in migration 000010:**

- **`billing_product_subjects`**: which subjects each topic pack unlocks. PK `(product_id, subject)`, plus an index on `subject`.
- **`client_power_ups`**: token balances. PK `(client_id, power_up)`, `CHECK 0..99`.
- **`power_up_uses`**: one row per power-up use. PK `(session_id, question_code, power_up)`; `'*'` in place of the question code means once per run. Indexed on `(client_id, created_at DESC)`.
- **`ad_rewards`**: the one table for every ad reward. Its indexes:
  - `uq_ad_rewards_ssv_transaction`: UNIQUE on Google's transaction id.
  - `idx_ad_rewards_client_created`: one player's rewards by time.
  - `idx_ad_rewards_client_placement_created`: daily caps per placement.
  - `idx_ad_rewards_pending_expires`: lets the sweeper find expired pending rows.
  - `idx_ad_rewards_fulfilled`: reports.

**Changed in 000010:**

- `wallet_ledger.currency` now also allows the token currencies. New indexes:
  - `idx_wallet_ledger_client_type_created`: daily caps and history.
  - `idx_wallet_ledger_type_created`: reports.
  - `uq_wallet_ledger_server_event`: UNIQUE `(client_id, transaction_type, currency, reference_id)` for `ad_reward`, `pass_reward`, `power_up_charge` and `power_up_token_use`.
- `billing_products.kind` now also allows `non_consumable`.
- `play_purchases` gets an index on owned products (`client_id, product_id`, granted only).

**Ledger transaction types** (all in `wallet_ledger`):

| Type | Currency | Meaning | Reference |
|---|---|---|---|
| `iap_purchase` | gems/coins | A verified pack | Google order id |
| `iap_refund` | gems/coins | Taken back after a refund | `refund:<token prefix>` |
| `ad_reward` | coins/gems/token | A verified ad, or a Pass claim of it | `ad:<ad_rewards.id>` |
| `pass_reward` | gems | Quiz Pass monthly gems | `pass_gems:YYYY-MM` |
| `power_up_charge` | coins | Coins paid for a power-up | `pu:<hash of session, question, power-up>` |
| `power_up_token_use` | token | A token spent on a power-up | same |
| `spin_wheel`, `chest_reward_*` | coins/gems | App-reported in-game rewards (capped) | |

The purchase records themselves are `play_purchases` and `client_subscriptions`. Every verification, grant, refund, rejection and notification is a row in `billing_audit`. Grant and refund audit rows are written inside the same transaction as the grant or refund.

## 9. API

All routes exist at the root and under `/api/v1`.

| Method and path | Auth | Purpose |
|---|---|---|
| `GET /billing/products` | player | Catalog for this player (packs with `subjects` and `owned`) |
| `GET /billing/account-id` | player | `obfuscated_account_id` to pass to Play |
| `POST /billing/verify` | player | Verify and grant a purchase (200 / 202 pending; 400, 403, 409 rejected; 502 retry) |
| `GET /billing/subscription` | player | Quiz Pass status |
| `GET /billing/entitlements` | player | Pass status, owned topic packs, perks |
| `POST /billing/rtdn` | Pub/Sub OIDC + `?token=` | Google Play notifications |
| `POST /pass/monthly-gems` | player | Claim monthly gems (402 without the Pass, 409 already claimed) |
| `GET /ads/config` | player | Ad units, request policy, placements, what is left today, and the ad chest odds (`chest_odds`) |
| `POST /ads/rewards` | player | Start an ad reward (Pass: paid now). 429 caps or cooldown, 409 token cap, 503 ads off |
| `GET /ads/rewards/:id` | player | Poll a reward |
| `POST /ads/rewards/:id/dev-complete` | player | Local testing only. 404 unless `ADS_DEV_AUTO_FULFILL=true` outside production |
| `GET /ads/ssv` | Google signature | AdMob server-side verification callback |
| `GET /power-ups` | player | Prices, tokens held, coins |
| `POST /quiz/power-ups/fifty-fifty` | player | Now charges (`pay_with`); 402 when the player cannot pay |
| `POST /quiz/sudden-death/power-up` and `/answer` (skip) | player | Now charge (`pay_with`); error events `insufficient_funds` and `no_power_up_token` |
| `POST /profile/delete` | player | Delete the account and all its data (password confirmed; 403 wrong password; rate limited) |
| `GET /privacy`, `GET /account-deletion` | public | Privacy policy and how to delete an account without the app (URLs for Play Console) |

## 10. Configuration

Backend (Catalyst AppSail env vars, never `.env`):

| Variable | Required | Notes |
|---|---|---|
| `JWT_SECRET` | **yes, in production** | 32+ private characters. Without it, every start logs a security warning and login tokens can be forged. Changing it signs everyone out. |
| `GOOGLE_PLAY_PACKAGE_NAME` | | Default `com.skillverse.skillverse_app` |
| `GOOGLE_PLAY_SERVICE_ACCOUNT_JSON_B64` | for billing | Base64 of the service-account key |
| `BILLING_ACCOUNT_SECRET` | for billing | 32+ characters; keys the purchase-owner HMAC. Do not rotate it while purchases are unfinished. |
| `GOOGLE_PLAY_ALLOW_TEST_PURCHASES` | | Default true outside production, false in production |
| `GOOGLE_PLAY_RTDN_TOKEN`, `GOOGLE_PLAY_RTDN_AUDIENCE`, `GOOGLE_PLAY_RTDN_PUSH_SA_EMAIL` | for notifications | Pub/Sub push authentication |
| `ADS_ENABLED` | | Kill switch; default true |
| `ADS_USER_ID_SECRET` | for ads | 32+ characters; falls back to `BILLING_ACCOUNT_SECRET` |
| `ADMOB_REWARDED_AD_UNIT_ANDROID`, `ADMOB_REWARDED_AD_UNIT_IOS` | **yes, in production** | Real rewarded ad unit ids. Outside production they default to Google's test units, which production refuses. |
| `ADMOB_SSV_KEYS_URL` | | Default `https://www.gstatic.com/admob/reward/verifier-keys.json` |
| `ADS_MAX_CONTENT_RATING` | | Default `G` |
| `ADS_AGE_TREATMENT` | | `teen` (default), `child`, `unspecified` |
| `ADS_DEV_AUTO_FULFILL` | | Local testing with test ads; ignored in production |
| `SUPPORT_EMAIL` | for the Play listing | Contact shown on `/privacy` and `/account-deletion`; without it the pages point to the Play listing's contact |

App build:

- **Android release:** `flutter build appbundle -PADMOB_APP_ID=ca-app-pub-XXXX~YYYY`. Without it, Google's test app id is used and only test ads are shown.
- **iOS:** replace `GADApplicationIdentifier` in `ios/Runner/Info.plist` before an iOS release. Billing is Android-only for now, because the backend verifies Google Play purchases.

## 11. Setup checklist

**Play Console**

1. Create the app and a payments profile.
2. Set up an upload keystore and release signing.
3. Upload a build to internal testing.
4. Create the products with the ids in section 3, and the `quiz_pass` subscription with base plans `monthly` and `yearly`.
5. Add license testers.

**Google Cloud**

1. Enable the Google Play Android Developer API.
2. Create a service account and download its JSON key.
3. In Play Console › Users & permissions, invite the service account with *View financial data* and *Manage orders and subscriptions*.

**Pub/Sub**

1. Create a topic and grant `google-play-developer-notifications@system.gserviceaccount.com` the Publisher role.
2. Create a push subscription with authentication (a service account; audience = the endpoint URL) pointing to `https://<api>/api/v1/billing/rtdn?token=<GOOGLE_PLAY_RTDN_TOKEN>`.
3. Set the topic in Play Console › Monetization setup.

**AdMob**

1. Create the app and a rewarded ad unit.
2. Turn on server-side verification with the callback URL `https://<api>/api/v1/ads/ssv`. Google sends a signed test callback, which the server answers with 200.
3. Set Blocking controls for sensitive categories.
4. Configure the privacy and messaging (UMP) forms.

**Backend:** set the env vars in section 10.

## 12. Deploy order and changes in behaviour

1. **Set a private `JWT_SECRET` in Catalyst.** Until then, every start logs a security warning and login tokens can be forged.
2. **Apply the schema:** push the migration repo (000010 runs from its CI), then deploy the backend. Older backends ignore the new tables, and the new backend runs without 000010, but its new endpoints fail until it is applied.
3. **Release the app** with the Gems & Pass tab, the ad chest and server-charged 50:50.

What changes for players on **older app versions**:

- **Sudden Death `+5s` and Skip now cost coins.** They were free in live mode before. A player who can't pay sees "not enough coins for this power-up".
- **MCQ 50:50 is still charged locally** through `/wallet/debit` (unchanged), and the old endpoint was unused by those versions.

## 13. Not done yet (next steps)

- **Age band at signup.** Ask for date of birth at signup and store an age band, then serve personalised ads, with consent, to verified adults only. The server already decides the ad policy per player, so only `adRequestPolicy` needs the band.
- **Parental consent for under-18s before 12 May 2027.**
- **Server-side earned chests and spin wheel.** This would remove `/wallet/credit` entirely; until then the capped `spin_wheel` and `chest_reward_*` credits remain the last client-reported rewards. Also call `GrantSpins` from an ad placement.
- **Charge the remaining power-ups on the server:** Hint, Reveal, Undo, Auto-match and Shuffle.
- **AI explanations and unlimited reviews for the Pass**, plus live events, leagues and team plans.
- **iOS purchases** (App Store verification) and a Razorpay web store.

## 14. Tests

- **Backend.** Run `go test ./...`. It covers:
  - Purchase verification: owner, replay, test purchases, quantity, consume retry, refunds and shortfall, topic packs, the sweeper.
  - Notifications: OIDC authentication.
  - Rewarded ads: ECDSA-signed callbacks, forged or duplicate or concurrent callbacks, caps, cooldown, token cap, the Pass path, the kill switch.
  - Power-up charges and their idempotency, on the HTTP and socket Sudden Death paths.
  - The Pass perks, and the production-secret check.
- **Backend on real PostgreSQL.** Run `MONETIZATION_PG_URL=<local db> go test ./internal/services -run PostgreSQL`. It never reads `.env`.
- **App.** Run `flutter test test/monetization_test.dart`. It covers:
  - The billing controller: it finishes a purchase only after the server grants it, leaves a rejected one unfinished, keeps an offline one for retry, and ignores duplicate events.
  - The ad runner: Pass path, early close, caps, slow callback.
  - The datasource parsing, the 402 mapping, and the store widget.

## 15. Google Play compliance

**In the app and backend:**

| Play requirement | How it is met |
|---|---|
| Digital goods use Google Play Billing | Gem packs, topic packs and the Quiz Pass are sold only through Play Billing (Billing Library 9.1.0). There are no outside payment links in the app. |
| Purchases acknowledged within 3 days; refunds respected | The server consumes or acknowledges every purchase after granting it. The hourly sweeper retries, and voided purchases take back what they gave. |
| Subscription terms clear before purchase; easy to cancel | The Quiz Pass card shows the price per period, says it renews automatically until cancelled and how to cancel, and has a **Manage or cancel in Google Play** button that opens Play's subscription centre. |
| Odds disclosed for randomised rewards | The ad chest shows its odds before opening (`chest_odds` from `/ads/config`). Coins can't be bought, so the coin-priced Chest Box chests are not paid loot boxes. |
| Rewarded ads opt-in, reward stated, not disruptive | Rewarded ads only, each started from a tap that names the reward. Never shown during a game; daily caps; no banners, interstitials or app-open ads. |
| Families policy: no advertising ID from children or unknown ages | Everyone is treated as under 18 until age is verified: non-personalised ads, G rating, teen treatment. The `AD_ID` permission is removed from the manifest (`tools:node="remove"`). |
| EEA/UK consent can be changed | Google UMP consent is requested before the first ad. An **Ad privacy choices** button appears where Google says it is required. |
| Account deletion in the app and on the web | Profile › **Delete account** (password confirmed) deletes the account and all its data (`POST /profile/delete`). The public page `/account-deletion` explains how to delete without the app. |
| Privacy policy in the listing and in the app | `/privacy` is served by the backend; the app links to it from Shop › Gems & Pass. **The text is a draft written from what the app actually does: have it reviewed, and set `SUPPORT_EMAIL`.** |
| Target API level | targetSdk 36 (Flutter default). |

**In Play Console (not code):**

- **Store presence › Privacy policy:** `https://<api host>/privacy`.
- **App content › Ads:** "Yes, my app contains ads".
- **App content › Advertising ID:** "No" (the permission is removed).
- **App content › Target audience and content:** choose the age groups honestly.
  - If they include children under 13, the app falls under the Families policy: set `ADS_AGE_TREATMENT=child`, and use only Families-certified ad settings.
  - Children under 13 are also why `AD_ID` stays removed.
- **App content › Data safety:** declare these data types:
  - Personal info: email, name.
  - App activity: in-app interactions, in-app purchase history.
  - Financial info: purchase history (via Google Play).
  - App info and performance, and device or other IDs, collected by the Mobile Ads SDK.
  - Encrypted in transit: yes. Deletion: yes, with the `/account-deletion` URL.
- **App content › Account deletion:** the `/account-deletion` URL.
- **Monetize:**
  - Create the products and the `quiz_pass` base plans (section 3), with clear names and descriptions.
  - For Indian users, set prices in INR.
- **AdMob:**
  - Blocking controls for sensitive categories.
  - Privacy and messaging (UMP) forms for GDPR.
  - SSV callback URL (section 11).
- **Content rating questionnaire:** declare in-app purchases, rewarded ads, and that random rewards (the chests and the spin wheel) cannot be bought with real money.
