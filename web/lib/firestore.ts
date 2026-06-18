import {
  getApps,
  initializeApp,
  cert,
  applicationDefault,
  type App,
} from "firebase-admin/app";
import { getFirestore, type Firestore } from "firebase-admin/firestore";

let cached: Firestore | null = null;

/**
 * Lazily initialize Firestore from one of:
 *  - FIREBASE_SERVICE_ACCOUNT_KEY  (full service-account JSON, single line)
 *  - GOOGLE_APPLICATION_CREDENTIALS (path to creds — uses applicationDefault)
 * Returns null when nothing is configured so callers can degrade gracefully.
 */
export function getDb(): Firestore | null {
  if (cached) return cached;

  const raw = process.env.FIREBASE_SERVICE_ACCOUNT_KEY;
  const hasADC = !!process.env.GOOGLE_APPLICATION_CREDENTIALS;
  if (!raw && !hasADC) return null;

  let app: App;
  if (getApps().length) {
    app = getApps()[0];
  } else if (raw) {
    app = initializeApp({ credential: cert(JSON.parse(raw)) });
  } else {
    app = initializeApp({ credential: applicationDefault() });
  }

  cached = getFirestore(app);
  return cached;
}
