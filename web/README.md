# proofswe.com — landing

Next.js (App Router) marketing landing + waitlist for ProofSWE. Lives in `web/`
inside the proofswe repo, fully isolated from the Go tooling.

## Run

```bash
cd web
npm install
npm run dev      # http://localhost:3000
```

## Waitlist

`POST /api/waitlist { email }` writes to the `waitlist` Firestore collection
(doc id = lowercased email). Without credentials configured the endpoint still
returns 200 so the form works in dev — see `.env.local.example`.

## Structure

- `app/page.tsx` — static landing (chrome wordmark, thesis, waitlist).
- `app/waitlist-form.tsx` — the one client component (form + states).
- `app/api/waitlist/route.ts` — signup endpoint.
- `lib/firestore.ts` — lazy Firestore init.

Everything renders static except the API route. Blog / leaderboard / charts
slot in as new routes later without touching the landing page.
