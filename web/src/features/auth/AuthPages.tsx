// SPDX-License-Identifier: AGPL-3.0-only

// Logged-out and gated screens: owner setup, login (password, then a 2FA step), the signed-out
// gate over the app shell, password reset, email verification, invites, CLI approval and the 404.
// Every screen uses the same header, footer, 380px card, type scale and 42px controls.
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ArrowLeft, ArrowRight, CheckCircle2, Compass } from "lucide-react";
import { api, type InvitePreview, type Me } from "../../api/client";
import { Center, useSession } from "../../app/session";
import { ShellSkeleton } from "../../app/Shell";
import { ErrorLine, Field, Input, Spinner } from "../../kit";
import { AuthButton, AuthCard, AuthLayout, PasswordMeter, SuccessLine } from "./AuthChrome";

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/** Wraps a form submit: busy flag, error line, preventDefault. */
function useSubmit(fn: () => Promise<void>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(undefined);
    try { await fn(); } catch (err) { setError(err); } finally { setBusy(false); }
  };
  return { busy, error, onSubmit, clear: () => setError(undefined) };
}

const safeNext = (n: string | null) => (n && n.startsWith("/") && !n.startsWith("//") ? n : "/");

/** Stores the signed-in user. Outside the gate it also follows ?next=; the gate stays on its URL. */
function useSignedIn(gate = false) {
  const { setMe } = useSession();
  const nav = useNavigate();
  const [params] = useSearchParams();
  return (me: Me) => {
    setMe(me);
    if (!gate) nav(safeNext(params.get("next")), { replace: true });
  };
}

/** A logged-out page: the shared frame with one card. */
function AuthPage({ title, subtitle, children }: { title: ReactNode; subtitle?: ReactNode; children: ReactNode }) {
  return (
    <AuthLayout>
      <AuthCard title={title} subtitle={subtitle}>{children}</AuthCard>
    </AuthLayout>
  );
}

/** Quiet text link used under forms ("Back to log in"). */
function BackLink({ to, children }: { to: string; children: ReactNode }) {
  return (
    <Link to={to} className="inline-flex items-center justify-center gap-1.5 rounded text-xs leading-[18px] text-fg-2 transition-colors duration-200 hover:text-foreground">
      <ArrowLeft size={12} aria-hidden />
      {children}
    </Link>
  );
}

const LINK_BUTTON = "inline-flex items-center gap-1.5 rounded text-xs leading-[18px] text-fg-2 transition-colors duration-200 hover:text-foreground";

// ---------------------------------------------------------------------------------------------
// Owner setup

export function SetupPage() {
  const [params] = useSearchParams();
  const [status, setStatus] = useState<boolean | null>(null);
  const [token, setToken] = useState(params.get("token") || "");
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const done = useSignedIn();
  useEffect(() => { api.setupStatus().then((s) => setStatus(s.needsSetup), () => setStatus(true)); }, []);
  const s = useSubmit(async () => done(await api.setup({ token: token.trim(), name, email, password })));
  if (status === null) return <Center><Spinner /></Center>;
  if (!status) return <Navigate to="/login" replace />;
  return (
    <AuthPage title="Create the owner account" subtitle="This account owns the install. You can invite others later.">
      <form onSubmit={s.onSubmit} className="space-y-4">
        {!params.get("token") && (
          <Field label="Setup token" hint="Printed by the installer, also in the setup link.">
            <Input required value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" spellCheck={false} mono />
          </Field>
        )}
        <Field label="Name">
          <Input required value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        <Field label="Email">
          <Input required type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" spellCheck={false} />
        </Field>
        <div>
          <Field label="Password">
            <Input required minLength={10} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
          <PasswordMeter value={password} />
        </div>
        <ErrorLine error={s.error} />
        <AuthButton busy={s.busy}>Create account</AuthButton>
      </form>
    </AuthPage>
  );
}

// ---------------------------------------------------------------------------------------------
// Login

type LoginStep = "email" | "password" | "totp";

/**
 * The login card. Step 1 asks for the email (Continue stays disabled until it looks valid),
 * step 2 for the password, and a 2FA step follows when the account has TOTP enabled.
 */
function LoginFlow({ gate = false }: { gate?: boolean }) {
  const done = useSignedIn(gate);
  const [step, setStep] = useState<LoginStep>("email");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [ticket, setTicket] = useState("");
  const [code, setCode] = useState("");
  const emailOk = EMAIL_RE.test(email.trim());
  const s = useSubmit(async () => {
    if (step === "totp") { done(await api.loginTotp(ticket, code.replace(/\s/g, ""))); return; }
    const r = await api.login({ email: email.trim(), password });
    if (r.needTotp && r.ticket) { setTicket(r.ticket); setStep("totp"); return; }
    if (r.user && r.install) done({ user: r.user, install: r.install });
  });

  if (step === "totp") {
    return (
      <AuthCard titleId="login-title" title="Two-factor code" subtitle="Enter the 6-digit code from your authenticator app, or a recovery code.">
        <form onSubmit={s.onSubmit} className="space-y-4">
          <Input
            required autoFocus inputMode="numeric" autoComplete="one-time-code" aria-label="Verification code"
            placeholder="000000" value={code} onChange={(e) => setCode(e.target.value)}
            className="text-center font-mono text-lg tracking-[0.3em]"
          />
          <ErrorLine error={s.error} />
          <AuthButton busy={s.busy} disabled={!code.trim()}>Verify</AuthButton>
          <div className="flex justify-center">
            <button type="button" className={LINK_BUTTON} onClick={() => { setStep("password"); setCode(""); s.clear(); }}>
              <ArrowLeft size={12} aria-hidden />
              Back
            </button>
          </div>
        </form>
      </AuthCard>
    );
  }

  if (step === "password") {
    return (
      <AuthCard titleId="login-title" title="Enter your password" subtitle={<>Logging in as <span className="text-foreground">{email.trim()}</span></>}>
        <form onSubmit={s.onSubmit} className="space-y-3">
          <Input
            required autoFocus type="password" autoComplete="current-password" aria-label="Password"
            placeholder="Password" value={password} onChange={(e) => setPassword(e.target.value)}
          />
          <ErrorLine error={s.error} />
          <AuthButton busy={s.busy} disabled={!password}>Log in</AuthButton>
          <div className="flex items-center justify-between pt-1">
            <button type="button" className={LINK_BUTTON} onClick={() => { setStep("email"); setPassword(""); s.clear(); }}>
              <ArrowLeft size={12} aria-hidden />
              Use another email
            </button>
            <Link to="/forgot" className="rounded text-xs leading-[18px] text-fg-2 transition-colors duration-200 hover:text-foreground">Forgot password?</Link>
          </div>
        </form>
      </AuthCard>
    );
  }

  return (
    <AuthCard titleId="login-title" title="Welcome to 5050cloud" subtitle="Deploy and run your services on hardware you control." streaks>
      <form
        onSubmit={(e) => { e.preventDefault(); if (emailOk) setStep("password"); }}
        className="space-y-3"
      >
        <Input
          type="email" autoFocus autoComplete="email" aria-label="Email" placeholder="you@example.com"
          value={email} onChange={(e) => setEmail(e.target.value)} spellCheck={false}
        />
        <AuthButton type="submit" disabled={!emailOk} iconRight={<ArrowRight size={16} aria-hidden />}>Continue</AuthButton>
      </form>
      <p className="mt-6 text-center text-xs leading-[18px] text-fg-3">New here? Ask the install owner for an invite link.</p>
    </AuthCard>
  );
}

export function LoginPage() {
  const { me } = useSession();
  const [params] = useSearchParams();
  if (me) return <Navigate to={safeNext(params.get("next"))} replace />;
  return <AuthLayout><LoginFlow /></AuthLayout>;
}

/**
 * Shown in place of a gated page while signed out. The app shell stays behind a blurred, dimmed
 * backdrop and the URL does not change. Escape and backdrop clicks do nothing; signing in
 * re-renders the gated page.
 */
export function AuthGate() {
  return (
    <>
      <ShellSkeleton />
      <div role="dialog" aria-modal="true" aria-labelledby="login-title" className="fixed inset-0 z-50 flex items-center justify-center bg-overlay px-5 backdrop-blur-[2px] animate-overlay-in">
        <LoginFlow gate />
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------------------------
// Password reset

export function ForgotPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const s = useSubmit(async () => { await api.forgot(email.trim()); setSent(true); });
  return (
    <AuthPage title="Reset your password" subtitle="We will email a link that lets you choose a new password.">
      {sent ? (
        <div className="space-y-4">
          <SuccessLine>If an account uses {email.trim()}, a reset link is on its way. Check your inbox and spam folder.</SuccessLine>
          <BackLink to="/login">Back to log in</BackLink>
        </div>
      ) : (
        <form onSubmit={s.onSubmit} className="space-y-4">
          <Field label="Email">
            <Input required type="email" autoFocus autoComplete="email" spellCheck={false} value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <ErrorLine error={s.error} />
          <AuthButton busy={s.busy}>Send reset link</AuthButton>
          <BackLink to="/login">Back to log in</BackLink>
        </form>
      )}
    </AuthPage>
  );
}

export function ResetPage() {
  const { token = "" } = useParams();
  const [password, setPassword] = useState("");
  const [ticket, setTicket] = useState<string>();
  const [code, setCode] = useState("");
  const done = useSignedIn();
  const s = useSubmit(async () => {
    if (ticket) { done(await api.loginTotp(ticket, code.replace(/\s/g, ""))); return; }
    const r = await api.reset(token, password);
    if (r.needTotp && r.ticket) { setTicket(r.ticket); return; }
    if (r.user && r.install) done({ user: r.user, install: r.install });
  });
  if (ticket) {
    return (
      <AuthPage title="Two-factor code" subtitle="Your password is changed. Enter the 6-digit code from your authenticator app, or a recovery code, to sign in.">
        <form onSubmit={s.onSubmit} className="space-y-4">
          <Input
            required autoFocus inputMode="numeric" autoComplete="one-time-code" aria-label="Verification code"
            placeholder="000000" value={code} onChange={(e) => setCode(e.target.value)}
            className="text-center font-mono text-lg tracking-[0.3em]"
          />
          <ErrorLine error={s.error} />
          <AuthButton busy={s.busy} disabled={!code.trim()}>Verify</AuthButton>
        </form>
      </AuthPage>
    );
  }
  return (
    <AuthPage title="Choose a new password" subtitle="Pick something you do not use anywhere else.">
      <form onSubmit={s.onSubmit} className="space-y-4">
        <div>
          <Field label="New password">
            <Input required minLength={10} type="password" autoFocus value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
          <PasswordMeter value={password} />
        </div>
        <ErrorLine error={s.error} />
        <AuthButton busy={s.busy}>Set password</AuthButton>
        <BackLink to="/login">Back to log in</BackLink>
      </form>
    </AuthPage>
  );
}

// ---------------------------------------------------------------------------------------------
// Verification, invites, CLI approval

type VerifyState = { status: "busy" } | { status: "ok" } | { status: "error"; error: unknown };

export function VerifyPage() {
  const { token = "" } = useParams();
  const nav = useNavigate();
  const [state, setState] = useState<VerifyState>({ status: "busy" });
  useEffect(() => {
    let live = true;
    api.verifyEmail(token).then(
      () => { if (live) setState({ status: "ok" }); },
      (error) => { if (live) setState({ status: "error", error }); },
    );
    return () => { live = false; };
  }, [token]);
  return (
    <AuthPage title="Verify your email" subtitle="Confirming the address this install uses to reach you.">
      {state.status === "busy" && (
        <div className="flex justify-center py-6"><Spinner size={22} /></div>
      )}
      {state.status === "ok" && (
        <div className="space-y-4">
          <div className="flex justify-center"><CheckCircle2 size={32} className="text-ok-soft" aria-hidden /></div>
          <SuccessLine>Your email is verified.</SuccessLine>
          <AuthButton type="button" onClick={() => nav("/")}>Open the dashboard</AuthButton>
        </div>
      )}
      {state.status === "error" && (
        <div className="space-y-4">
          <ErrorLine error={state.error} />
          <BackLink to="/login">Back to log in</BackLink>
        </div>
      )}
    </AuthPage>
  );
}

export function InvitePage() {
  const { token = "" } = useParams();
  const [p, setP] = useState<InvitePreview>();
  const [err, setErr] = useState<unknown>();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const done = useSignedIn();
  useEffect(() => { api.invitePreview(token).then((x) => { setP(x); setEmail(x.email || ""); }, setErr); }, [token]);
  const s = useSubmit(async () => done(await api.acceptInvite({ token, email, name, password })));
  if (err) return <AuthPage title="Invite"><ErrorLine error={err} /></AuthPage>;
  if (!p) return <Center><Spinner /></Center>;
  if (!p.valid) {
    return (
      <AuthPage title="This invite cannot be used" subtitle={p.reason || "This invite is no longer valid."}>
        <BackLink to="/login">Back to log in</BackLink>
      </AuthPage>
    );
  }
  return (
    <AuthPage
      title={`Join ${p.installName}`}
      subtitle={`You are invited as ${p.role}${p.projectName ? ` on ${p.projectName}` : ""}.`}
    >
      <form onSubmit={s.onSubmit} className="space-y-4">
        <Field label="Name">
          <Input required value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" />
        </Field>
        <Field label="Email">
          <Input required type="email" value={email} readOnly={!!p.email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" spellCheck={false} />
        </Field>
        <div>
          <Field label="Password">
            <Input required minLength={10} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
          </Field>
          <PasswordMeter value={password} />
        </div>
        <ErrorLine error={s.error} />
        <AuthButton busy={s.busy}>Accept invite</AuthButton>
      </form>
    </AuthPage>
  );
}

/** /cli-auth?code=WXYZ-1234, signed in: approve the terminal's login. Sits on the bare background, no chrome. */
export function CliAuthPage() {
  const [params] = useSearchParams();
  const [code, setCode] = useState(params.get("code") || "");
  const fromTerminal = !!params.get("code");
  const [ok, setOk] = useState(false);
  const s = useSubmit(async () => { await api.cliApprove(code.trim().toUpperCase()); setOk(true); });
  return (
    <main className="flex min-h-screen items-center justify-center bg-background px-5 text-foreground animate-fade-in">
      <AuthCard titleId="cli-title" title="Approve CLI login" subtitle="Only approve a code you see in your own terminal right now.">
        {ok ? (
          <SuccessLine>Approved. You can return to your terminal.</SuccessLine>
        ) : (
          <form onSubmit={s.onSubmit} className="space-y-4">
            {fromTerminal ? (
              <div className="rounded-md border border-line bg-surface px-3 py-3 text-left">
                <p className="text-xs leading-[18px] text-fg-3">Code from your terminal</p>
                <p className="font-mono text-lg tracking-[0.2em] text-foreground">{code}</p>
              </div>
            ) : (
              <Field label="Code from your terminal">
                <Input required value={code} onChange={(e) => setCode(e.target.value)} placeholder="WXYZ-1234" mono spellCheck={false} />
              </Field>
            )}
            <ErrorLine error={s.error} />
            <AuthButton busy={s.busy} disabled={!code.trim()}>Approve login</AuthButton>
          </form>
        )}
      </AuthCard>
    </main>
  );
}

// ---------------------------------------------------------------------------------------------
// Unknown routes

/** The 404 page: a lucide compass, the code, one line of copy and a Go home button. */
export function AuthNotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center bg-background px-5 text-center text-foreground animate-fade-in">
      <div className="flex h-16 w-16 items-center justify-center rounded-full border border-line text-fg-2">
        <Compass size={28} strokeWidth={1.5} aria-hidden />
      </div>
      <p className="mt-6 text-2xl font-semibold leading-8">404</p>
      <p className="mt-2 text-base leading-6 text-fg-3">That page does not exist, or it moved.</p>
      <Link
        to="/"
        className="mt-8 inline-flex h-[42px] items-center justify-center rounded-md border border-accent bg-accent px-6 text-sm font-medium text-accent-fg transition-colors duration-200 ease-out hover:border-accent-hover hover:bg-accent-hover active:scale-[.98] active:duration-[50ms]"
        style={{ boxShadow: "0 4px 24px color-mix(in srgb, var(--pink-500) 27%, transparent)" }}
      >
        Go home
      </Link>
    </main>
  );
}
