import { NextResponse } from "next/server";
import { FieldValue } from "firebase-admin/firestore";
import { getDb } from "@/lib/firestore";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export async function POST(req: Request) {
  let email: unknown;
  try {
    ({ email } = await req.json());
  } catch {
    return NextResponse.json({ error: "bad request." }, { status: 400 });
  }

  if (typeof email !== "string" || !EMAIL_RE.test(email.trim())) {
    return NextResponse.json(
      { error: "that email looks off. try again." },
      { status: 400 },
    );
  }

  const normalized = email.trim().toLowerCase();

  const db = getDb();
  if (!db) {
    // No backend wired yet — accept so the UI works in dev. Configure
    // FIREBASE_SERVICE_ACCOUNT_KEY to persist signups.
    console.warn("[waitlist] no Firestore configured; signup not persisted:", normalized);
    return NextResponse.json({ ok: true, persisted: false });
  }

  try {
    await db.collection("waitlist").doc(normalized).set(
      {
        email: normalized,
        createdAt: FieldValue.serverTimestamp(),
        source: "landing",
      },
      { merge: true },
    );
    return NextResponse.json({ ok: true, persisted: true });
  } catch (err) {
    console.error("[waitlist] write failed:", err);
    return NextResponse.json(
      { error: "couldn't save that. try again in a sec." },
      { status: 500 },
    );
  }
}
