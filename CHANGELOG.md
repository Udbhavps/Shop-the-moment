# Changelog

Versioning: [Semantic Versioning](https://semver.org). Each release is a git tag `vX.Y.Z` and a section below.
Pre-1.0: minor versions may change behaviour; patch versions are fixes only.

## [0.2.0] - 2026-10-16

### Added
- Buy now on the TV shows a QR for a demo UPI payment. Scanning it opens an approval page on the phone (`/pay/{id}`); approving completes the UCP checkout and the TV shows the order. A simulated card button is the fallback when no phone is at hand.
- `demo_upi` payment handler in the UCP profile and checkout.
- Payment intents: `POST /api/sessions/{id}/pay-intent`, `GET /api/pay/{id}`, `POST /api/pay/{id}/approve|decline`. Intents expire after 5 minutes, and an old QR is refused if the cart changed.

### Changed
- Catalog currency is now INR with Indian price points (UPI is rupee-only). Amounts are shown as, for example, ₹9,999.
- Funnel: the phone steps are side paths, since a shopper can now buy without the phone.

### Known limits
- The QR is not a real UPI payment string and no UPI app will pay it. A real UPI flow needs a payment gateway account and webhook.

## [0.1.0] - 2026-10-16
First working prototype for the Google Cloud AI Builder Cup 2026 (Retail & Commerce).

### Added
- Go standard-library backend with an embedded plain HTML/CSS/JS front end (no build step).
- TV screen: three sample ads, pause and identify, exact / similar / not-in-catalog results, remote-style keyboard navigation.
- Gemini on Vertex AI over REST: structured frame analysis and a function-calling shopping assistant, with saved-analysis and rule-based fallbacks.
- Catalog grounding: the catalog is the source of truth, "exact" needs brand, category and colour to agree.
- Cart and checkout through a demo merchant that follows the Universal Commerce Protocol (UCP, spec 2026-04-08): `/.well-known/ucp`, `/ucp/v1/carts`, `/ucp/v1/checkout-sessions`, idempotency keys, simulated `demo_pay` handler.
- Buy on the TV, or send to the phone by QR (own QR encoder); both use the same cart.
- Advertiser dashboard (real events plus a labelled simulated sample) and an accuracy-check page.
- Dockerfile and `deploy/deploy.sh` for Cloud Run (`--max-instances=1`).

### Known limits
- Live Vertex AI calls are untested against the real service (only a fake server).
- UCP: no request signing, fulfillment or discount extensions; not checked against an official conformance suite.
- Sessions, carts and events are in memory and reset on restart.
