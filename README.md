# Shop the Moment

A shoppable connected-TV ad prototype. Pause an ad, and Gemini finds the products on screen, matches them against a real catalog, and lets the viewer ask questions, compare, and send items to their phone to buy. Built for the Google Cloud AI Builder Cup 2026 (Retail & Commerce).

Go standard library only, plain HTML/CSS/JS front end, no build step.

## Run it

```
go test ./...
PORT=8080 go run .            # opens with saved analysis; live AI off
```
- TV screen: `http://localhost:8080/`  (Enter = pause and identify, arrow keys move, Esc goes back)
- Phone view: scan the QR in the TV hand-off dialog, or open `/m/<session>`
- Advertiser view: `/dashboard`   Accuracy check: `/eval`

## How it works

```
 TV page ──pause frame──▶ POST /api/analyze ──▶ Gemini (Vertex AI, JSON schema)
    │                          │                       │ candidates + boxes
    │                          ▼                       ▼
    │                     Ground(): catalog match (exact / similar / none)
    ├─ ask bar ──▶ POST /api/chat ──▶ Gemini function calling over catalog tools
    │                         (search_catalog, get_product, find_cheaper_alternatives, shortlist_product)
    └─ QR (opaque session id) ──▶ /m/{id} phone cart ──▶ simulated checkout ──▶ events ──▶ /dashboard
```

Rules the code enforces:
- The catalog is the source of truth. The model never supplies prices, stock or product ids; chat cards come only from tool results.
- "Exact match" needs brand, category and colour to agree and be unambiguous. Everything else is shown as "similar" (dashed) or "not in catalog" (dotted).
- If Gemini fails or is off, a saved analysis of the sample ads is used and the UI says "Saved analysis" instead of "Gemini live". Chat falls back to a rule-based agent.
- Checkout is a simulation. No payment is taken. The QR holds only a random session id.
- The dashboard's top funnel is real events from this deployment; the second is a labelled SIMULATED sample. Nothing here claims integration with any Google ad system.

## Cart and checkout: Universal Commerce Protocol (UCP)

The TV and the phone both build the cart and check out through a demo merchant that speaks UCP over REST (spec version `2026-04-08`, https://ucp.dev/2026-04-08/specification/overview/). You can buy entirely on the TV ("Buy now"), or send items to the phone, or both: it is the same cart.

| Endpoint | Purpose |
|---|---|
| `GET /.well-known/ucp` | business profile: services, capabilities, payment handler |
| `POST /ucp/v1/carts`, `GET/PUT /ucp/v1/carts/{id}`, `POST .../cancel` | cart capability (`dev.ucp.shopping.cart`) |
| `POST /ucp/v1/checkout-sessions`, `GET/PUT .../{id}`, `POST .../{id}/complete`, `POST .../{id}/cancel` | checkout capability (`dev.ucp.shopping.checkout`) |

Item ids are variant SKUs (for example `STRIDE-PULSE-WHITE`). Amounts are integer minor units (cents). Calls need a `UCP-Agent: profile="https://..."` header; create and complete need `Idempotency-Key` (replays return the stored result, a reused key with a different body returns 409). Business problems such as out of stock come back as HTTP 200 with `ucp.status: "error"` and a `messages[]` array.

Example:
```
curl -s -X POST $URL/ucp/v1/carts -H 'UCP-Agent: profile="https://me.example/.well-known/ucp"' \
  -H 'Idempotency-Key: demo-1' -H 'Content-Type: application/json' \
  -d '{"line_items":[{"item":{"id":"STRIDE-PULSE-WHITE"},"quantity":1}]}'
```

What is and is not covered:
- Covered: profile discovery, cart and checkout operations, status lifecycle (`ready_for_complete` to `completed` or `canceled`), totals, idempotency, error messages.
- Not covered: request signing (RFC 9421), fulfillment and discount extensions, identity linking, fetching or verifying the caller's profile, tax and shipping. Checkout skips `incomplete` because the demo needs no buyer details.
- Payment is a made-up handler, `demo_pay`. Any token starting `demo_tok_` is accepted and refused otherwise. No card data is ever collected, and no money moves.
- Written from the published spec pages and tested only against this project's own tests, not an official conformance suite. Check field names against the current spec before claiming compatibility. This is not an official or Google-run UCP deployment.

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `PORT` | listen port | 8080 |
| `GOOGLE_CLOUD_PROJECT` | enables live Gemini | unset = live AI off |
| `GEMINI_LOCATION` | Vertex location | `global` |
| `GEMINI_MODEL` | model id | `gemini-2.5-flash` (check the current model list and change if needed) |
| `GEMINI_ENABLED` | `false` forces saved mode | on |
| `PUBLIC_BASE_URL` | base URL put in QR codes | derived from request headers |
| `GOOGLE_ACCESS_TOKEN` | local testing token (else metadata server, else `gcloud`) | |

## Deploy to Cloud Run

```
PROJECT_ID=your-project ./deploy/deploy.sh
```
Sessions are in memory, so the service must run with `--max-instances=1` (the script does this). Data resets when the instance restarts. The runtime service account needs `roles/aiplatform.user`. After deploying, `curl $URL/api/config` should show `"live_ai": true`.

## Status, honestly
- Tested: catalog grounding, agent tools, session/cart/checkout, QR encoder (decoded with OpenCV), HTTP API, and the full browser flow in headless Chromium (`scripts/e2e.mjs`).
- Not yet tested: the real Vertex AI calls. They were exercised only against a fake server, because the build sandbox had no Google Cloud credentials. Run the `/eval` page on your deployment in live mode first.
- The sample ads are synthetic vector scenes, so accuracy numbers show the pipeline works on a controlled set, not real footage.

## API
`GET /api/config, /api/ads, /api/catalog, /api/products/{id}, /api/analytics, /api/events`
`POST /api/sessions`, `GET /api/sessions/{id}`, `POST /api/sessions/{id}/{select|unselect|cart|checkout|phone-opened}`
`POST /api/sessions/{id}/cart {product_id, color, delta, channel}` and `/checkout {channel}` are adapters over the UCP cart and checkout above.
`POST /api/analyze {scene_id, frame, mode}`, `POST /api/chat {session_id, scene_id, message, history}`, `POST /api/events`
`GET /api/qr?text=`, `/img/{product}.svg?color=`, `/scenes/{scene}.svg`

## Demo script (3 minutes)
1. Play "Morning Run"; say the problem: viewers like what they see in ads and have no way to act on it.
2. Press OK. Show the green exact match for the sneakers and the dashed similar items.
3. Open the hidden-brand backpack scene: only "similar", and say why it is not claimed as exact.
4. Ask "Show me a cheaper one" and "this in blue"; show tool-call trace and catalog prices.
5. Press Buy now, change the quantity, pay with the demo card on the TV. Then show Send to phone and the QR for the phone path.
6. Show `/dashboard` (real funnel vs. labelled simulated) and `/eval`.
7. Close on architecture: Gemini on Vertex AI, Cloud Run, grounded in a catalog.

## Submission checklist
PDF deck, public video of 3 minutes or less, category Retail & Commerce, deployed URL, deadline Oct 18, 2026 (confirm the exact time and zone on the Hack2skill dashboard).
