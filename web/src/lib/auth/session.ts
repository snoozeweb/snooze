import { authStore } from "./store";
import { decodeJwt, isExpired, secondsUntilExpiry } from "./jwt";
import { readRefreshToken, readToken } from "./storage";

/**
 * session.ts owns the *lifetime* of the browser session: keeping the access
 * token fresh so the user is never interrupted, and reporting honestly when
 * that is no longer possible so the caller can bounce to the login page.
 *
 * Two mechanisms, deliberately overlapping:
 *
 *   1. Proactive — a timer fires LEAD_SECONDS before `exp` and rotates the
 *      token silently. Nothing in the UI observes it. This is the path that
 *      matters: a request that needs a fresh token never happens, because the
 *      token was already refreshed.
 *   2. Reactive — the API client calls ensureRotation() on a 401 and retries
 *      once. This covers what the timer cannot: a machine that slept through
 *      the scheduled wake-up, a token revoked server-side, a clock skew.
 *
 * Timers are unreliable (background-tab throttling, suspend/resume), so
 * visibilitychange and online also re-arm and top up the token — the common
 * "laptop was closed for two hours, first click does nothing" case.
 */

/** Refresh this many seconds before the access token's `exp`. */
const LEAD_SECONDS = 60;
/** Never schedule a wake-up tighter than this (avoids a spin on odd clocks). */
const MIN_DELAY_MS = 5_000;
/** Cap a single sleep so a very long-lived token still re-checks periodically. */
const MAX_DELAY_MS = 10 * 60 * 1000;

/** Cross-tab rotation lock. Rotation revokes the presented refresh token
 *  server-side, so two tabs rotating the same token means one of them is
 *  handed "refresh token revoked" and gets logged out. */
const LOCK_KEY = "snooze-refresh-lock";
const LOCK_TTL_MS = 10_000;
const LOCK_POLL_MS = 100;

export type RotationOutcome = {
  /** The new access token, or null when rotation did not produce one. */
  token: string | null;
  /**
   * True when the server definitively rejected the refresh token (expired,
   * revoked, unknown). The session is unrecoverable and the caller should
   * send the user to the login page. False for transient failures (offline,
   * 5xx, no refresh token stored at all) — those must NOT log anyone out.
   */
  fatal: boolean;
};

const OK = (token: string): RotationOutcome => ({ token, fatal: false });
const TRANSIENT: RotationOutcome = { token: null, fatal: false };
const FATAL: RotationOutcome = { token: null, fatal: true };

type RefreshEnvelope = { token?: string; refresh_token?: string };

let lockId = "";
function ownerId(): string {
  if (!lockId) lockId = Math.random().toString(36).slice(2) + Date.now().toString(36);
  return lockId;
}

/** Best-effort cross-tab mutex. Returns false when another tab holds a lock
 *  that has not aged out. Storage being unavailable means there is nothing to
 *  coordinate with, so we report success and behave as a single tab. */
function acquireLock(): boolean {
  try {
    const raw = localStorage.getItem(LOCK_KEY);
    if (raw) {
      const ts = Number(raw.slice(raw.indexOf(":") + 1));
      if (Number.isFinite(ts) && Date.now() - ts < LOCK_TTL_MS) return false;
    }
    const mine = `${ownerId()}:${Date.now()}`;
    localStorage.setItem(LOCK_KEY, mine);
    // Read back: on a simultaneous write the last writer wins, and the loser
    // sees someone else's id here and steps aside.
    return localStorage.getItem(LOCK_KEY) === mine;
  } catch {
    return true;
  }
}

function releaseLock(): void {
  try {
    const raw = localStorage.getItem(LOCK_KEY);
    if (raw && raw.startsWith(`${ownerId()}:`)) localStorage.removeItem(LOCK_KEY);
  } catch {
    // Best-effort.
  }
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

/** Wait for the tab holding the lock to publish a new access token. */
async function awaitPeerRotation(previous: string | null): Promise<RotationOutcome> {
  const deadline = Date.now() + LOCK_TTL_MS;
  while (Date.now() < deadline) {
    await sleep(LOCK_POLL_MS);
    const token = readToken();
    if (token && token !== previous) {
      // Mirror the peer's result into this tab's store. The storage event does
      // this too, but only for documents that were not the writer — and it is
      // not guaranteed to have been delivered yet.
      authStore.getState().refresh();
      return OK(token);
    }
    try {
      if (!localStorage.getItem(LOCK_KEY)) break; // peer finished without a new token
    } catch {
      break;
    }
  }
  return TRANSIENT;
}

async function postRefresh(stored: string): Promise<RotationOutcome> {
  let res: Response;
  try {
    res = await fetch("/api/v1/login/refresh", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: stored }),
    });
  } catch {
    // Network is down / request aborted. The refresh token itself may well
    // still be good, so do not tear the session down over it.
    return TRANSIENT;
  }
  if (!res.ok) {
    // 400/401/403 mean the server looked at the token and said no. Anything
    // else (5xx, 502 from a proxy mid-deploy) is the server's problem.
    return res.status === 400 || res.status === 401 || res.status === 403 ? FATAL : TRANSIENT;
  }
  let env: RefreshEnvelope;
  try {
    env = (await res.json()) as RefreshEnvelope;
  } catch {
    return TRANSIENT;
  }
  if (!env.token) return TRANSIENT;
  // Route the write through the auth store so claims, permissions and
  // isAuthenticated all move together — writing straight to localStorage
  // would leave the store holding the previous, expired claims.
  authStore.getState().login(env.token, env.refresh_token ?? null);
  return OK(env.token);
}

// In-tab single flight: concurrent 401s share one rotation.
let inFlight: Promise<RotationOutcome> | null = null;

/**
 * ensureRotation exchanges the stored refresh token for a fresh access token.
 * Concurrent callers in this tab share one request; concurrent *tabs* are
 * serialised through a localStorage lock so only one of them ever presents
 * the refresh token (presenting a rotated-away token would revoke the family).
 */
export function ensureRotation(): Promise<RotationOutcome> {
  if (inFlight) return inFlight;

  const run = async (): Promise<RotationOutcome> => {
    const stored = readRefreshToken();
    if (!stored) return TRANSIENT;
    const before = readToken();
    if (!acquireLock()) {
      const peer = await awaitPeerRotation(before);
      if (peer.token) return peer;
      // The peer never published one (tab closed mid-flight, lock aged out).
      // Fall through and rotate whatever is stored now.
      if (!acquireLock()) return TRANSIENT;
    }
    try {
      const current = readRefreshToken();
      if (!current) return TRANSIENT;
      return await postRefresh(current);
    } finally {
      releaseLock();
    }
  };

  // Start the body on a microtask, not synchronously. `run` can return without
  // ever awaiting (no refresh token stored) — were it invoked inline, its
  // cleanup would clear `inFlight` before the assignment below ever set it, and
  // every later caller would be handed that stale, already-settled promise.
  const p = Promise.resolve().then(run);
  inFlight = p;
  void p.finally(() => {
    if (inFlight === p) inFlight = null;
  });
  return p;
}

/**
 * ensureFreshToken returns an access token that is valid for at least
 * `leewaySec` more seconds, rotating first if needed. Returns null when there
 * is no usable session — the caller should redirect to login.
 */
export async function ensureFreshToken(leewaySec = LEAD_SECONDS): Promise<string | null> {
  const token = readToken();
  const claims = token ? decodeJwt(token) : null;
  if (token && claims && !isExpired(claims, leewaySec)) return token;
  if (!readRefreshToken()) return token && claims && !isExpired(claims) ? token : null;
  const { token: rotated } = await ensureRotation();
  if (rotated) return rotated;
  // Rotation failed. A token that is merely inside the lead window is still
  // usable — better to let the request run than to bounce a working session.
  return token && claims && !isExpired(claims) ? token : null;
}

// ---------------------------------------------------------------------------
// Proactive scheduler
// ---------------------------------------------------------------------------

let timer: ReturnType<typeof setTimeout> | null = null;
let started = false;

function clearTimer(): void {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
}

/** Milliseconds to wait before the next proactive rotation, or null when there
 *  is nothing to schedule (no session, or no refresh token to rotate with). */
function nextDelayMs(): number | null {
  const token = readToken();
  if (!token || !readRefreshToken()) return null;
  const claims = decodeJwt(token);
  if (!claims) return null;
  const ttl = secondsUntilExpiry(claims);
  if (!Number.isFinite(ttl)) return MAX_DELAY_MS; // no exp: just re-check later
  return Math.min(MAX_DELAY_MS, Math.max(MIN_DELAY_MS, (ttl - LEAD_SECONDS) * 1000));
}

function schedule(): void {
  clearTimer();
  const delay = nextDelayMs();
  if (delay === null) return;
  timer = setTimeout(() => {
    timer = null;
    void tick();
  }, delay);
}

async function tick(): Promise<void> {
  const token = readToken();
  const claims = token ? decodeJwt(token) : null;
  // Re-arm even if this rotation is a no-op or fails: the next visibility
  // change or timer tick gets another chance.
  if (token && claims && !isExpired(claims, LEAD_SECONDS)) {
    schedule();
    return;
  }
  const { fatal } = await ensureRotation();
  if (fatal) {
    // Refresh token is gone for good. Drop the session; the router's
    // unauthorized handler / route guard takes it from here.
    authStore.getState().logout();
    return;
  }
  schedule();
}

/**
 * startSessionRefresh arms the proactive refresh loop. Idempotent — calling it
 * twice is a no-op. It follows the auth store, so it starts working the moment
 * the user logs in and stands down when they log out.
 */
export function startSessionRefresh(): void {
  if (started || typeof window === "undefined") return;
  started = true;

  authStore.subscribe(schedule);

  const topUp = () => {
    if (typeof document !== "undefined" && document.visibilityState === "hidden") return;
    // A suspended machine wakes with a timer that fired late (or not at all)
    // and a token that expired while it slept. Rotate now, then re-arm.
    void tick();
  };
  document.addEventListener("visibilitychange", topUp);
  window.addEventListener("online", topUp);
  // Another tab rotated: adopt its token and re-arm against the new expiry.
  window.addEventListener("storage", (e) => {
    if (e.key === "snooze-token" || e.key === "snooze-refresh-token") schedule();
  });

  schedule();
}

/** Test seam: tears the scheduler down so a suite can re-arm it cleanly. */
export function stopSessionRefresh(): void {
  clearTimer();
  started = false;
}
