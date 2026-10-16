# Changelog

Versioning: [Semantic Versioning](https://semver.org). Each release is a git tag `vX.Y.Z` and a section below.
Pre-1.0: minor versions may change behaviour; patch versions are fixes only.

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
