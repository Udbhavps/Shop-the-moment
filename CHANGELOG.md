# Changelog

Versioning: [Semantic Versioning](https://semver.org). Each release is a git tag `vX.Y.Z` and a section below.
Pre-1.0: minor versions may change behaviour; patch versions are fixes only.

## [0.4.3] - 2026-10-18

### Fixed
- After one order was placed, every product's detail showed "Order placed", and nothing else could be bought in that session. The order now records its items, only those show "Order placed", and adding a different product starts a new purchase. A placed order still cannot be edited.

### Changed
- The sample clips now show people (a runner, hikers, a listener, figures on the dock), drawn behind the products. The vision prompt tells the model that people and plain clothing are not products.
- The TV player now has a play/pause chip with the title and time, a centre icon when the state changes, and a scrubber, so the sample reads as video rather than a still ad.

## [0.4.2] - 2026-10-17

### Fixed
- Buy now and Send to phone showed "not found" when the server had lost the viewer's session (a redeploy or restart, or a tab left open for hours). The TV page now starts a fresh session quietly and repeats the call once. `scripts/session_recover.mjs` tests it.

## [0.4.1] - 2026-10-17

### Changed
- Live analysis: Flash models now run without extended thinking (lower latency; Pro models are unchanged), the paused frame is sent at 1920x1080 instead of 1280x720, and the prompt asks the model to look closely for small printed wordmarks. First live `/eval` on Cloud Run: 100% exact-claim precision (11 of 11), 0 wrong exact claims, 95% items found (18 of 19), 74% exact/not-exact decisions right (14 of 19), 5.6 s average latency. The misses were all cautious (a readable brand not read), never a wrong claim.

## [0.4.0] - 2026-10-16

### Added
- A sample show, "Neon Harbor" (fictional, two scenes), next to the three ads. Items tied to the story carry a story card: "Worn by Mira Vale", or "Prop from the show" with why it matters.
- "Shop Mira's look": one button adds every exactly-matched item tied to a character to the UCP cart and goes to checkout.
- Official show merchandise: the catalog marks licensed products (`licensed`), shown as a badge. New `pendant` category with the replica Harbor Compass Pendant and a generic pendant as the similar option.
- `scripts/show_flow.mjs` and `scripts/all_scenes.mjs` browser checks.

### Notes
- Story text comes from per-scene metadata (cast, wardrobe, props), the way a studio would supply it. A vision model cannot know who wears what, so the story is never claimed as AI output. It attaches to live Gemini results by matching category and colour.
- Costume pieces with no legible logo stay "similar", never "exact", but keep their story.
- All characters and brands are fictional.

## [0.3.0] - 2026-10-16

### Added
- The sample ads now play as video. Each of the six scenes is a 7-second clip (products slide in, a light streak crosses, dust drifts), served from `/video/{scene}.webm|.mp4` with range support. The TV page picks WebM or MP4 by browser support.
- `full-<ad>.mp4`: one continuous file per ad, handy for screen recording and the demo video.
- `scripts/render_videos.mjs` re-renders the clips from the vector scenes (needs Playwright and ffmpeg).
- Pausing during the entrance settles on the finished frame, so detection boxes line up with the products.

### Known limits
- Clips are animated vector art made for this demo, not live-action footage. Products stand still after the first 1.7 seconds, so one set of boxes per scene is accurate. Moving products would need per-timestamp boxes.
- No soundtrack.

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
