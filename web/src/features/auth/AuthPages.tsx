// SPDX-License-Identifier: AGPL-3.0-only

// Owner setup, login with the TOTP step, password reset, email verification,
// invite accept and CLI approval (SPEC 13.1, 9.15).
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { Link, Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { api, type InvitePreview, type Me } from "../../api/client";
import { Center, useSession } from "../../app/session";
import { Button, ErrorLine, Field, Input, Logo, Spinner } from "../../kit";

function Card({ title, sub, children }: { title: string; sub?: ReactNode; children: ReactNode }) {
  return (
    <Center>
      <main className="w-full max-w-[400px] space-y-6">
        <div className="flex flex-col items-center gap-3 text-center">
          <Logo size={40} />
          <h1 className="text-xl font-semibold">{title}</h1>
          {sub && <p className="text-sm text-gray-600">{sub}</p>}
        </div>
        <div className="rounded-xl border border-gray-200 bg-secondaryBg p-6">{children}</div>
      </main>
    </Center>
  );
}

/** Wraps a form submit: busy flag, error line, preventDefault. */
function useSubmit(fn: () => Promise<void>) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true); setError(undefined);
    try { await fn(); } catch (err) { setError(err); } finally { setBusy(false); }
  };
  return { busy, error, onSubmit };
}

const safeNext = (n: string | null) => (n && n.startsWith("/") && !n.startsWith("//") ? n : "/");

function useSignedIn() {
  const { setMe } = useSession();
  const nav = useNavigate();
  const [params] = useSearchParams();
  return (me: Me) => { setMe(me); nav(safeNext(params.get("next")), { replace: true }); };
}

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
    <Card title="Create the owner account" sub="This account owns the install. You can invite others later.">
      <form onSubmit={s.onSubmit} className="space-y-4">
        {!params.get("token") && <Field label="Setup token" hint="Printed by the installer, also in the setup link."><Input required value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" /></Field>}
        <Field label="Name"><Input required value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" /></Field>
        <Field label="Email"><Input required type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" /></Field>
        <Field label="Password" hint="At least 10 characters."><Input required minLength={10} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" /></Field>
        <ErrorLine error={s.error} />
        <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Create account</Button>
      </form>
    </Card>
  );
}

export function LoginPage() {
  const { me } = useSession();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [ticket, setTicket] = useState<string>();
  const [code, setCode] = useState("");
  const done = useSignedIn();
  const [params] = useSearchParams();
  const s = useSubmit(async () => {
    if (ticket) { done(await api.loginTotp(ticket, code.replace(/\s/g, ""))); return; }
    const r = await api.login({ email, password });
    if (r.needTotp && r.ticket) { setTicket(r.ticket); return; }
    if (r.user && r.install) done({ user: r.user, install: r.install });
  });
  if (me) return <Navigate to={safeNext(params.get("next"))} replace />;
  if (ticket) return (
    <Card title="Two-factor code" sub="Enter the 6-digit code from your authenticator app, or a recovery code.">
      <form onSubmit={s.onSubmit} className="space-y-4">
        <Field label="Code"><Input required autoFocus inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} /></Field>
        <ErrorLine error={s.error} />
        <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Verify</Button>
        <button type="button" className="w-full text-center text-sm text-gray-500 hover:text-foreground" onClick={() => { setTicket(undefined); setCode(""); }}>Back</button>
      </form>
    </Card>
  );
  return (
    <Card title="Log in">
      <form onSubmit={s.onSubmit} className="space-y-4">
        <Field label="Email"><Input required type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus /></Field>
        <Field label="Password"><Input required type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" /></Field>
        <ErrorLine error={s.error} />
        <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Log in</Button>
        <Link to="/forgot" className="block text-center text-sm text-gray-500 hover:text-foreground">Forgot password?</Link>
      </form>
    </Card>
  );
}

export function ForgotPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const s = useSubmit(async () => { await api.forgot(email); setSent(true); });
  return (
    <Card title="Reset your password">
      {sent ? <p className="text-sm text-gray-700">If an account uses {email}, a reset link is on its way. <Link className="underline" to="/login">Back to log in</Link></p> : (
        <form onSubmit={s.onSubmit} className="space-y-4">
          <Field label="Email"><Input required type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus /></Field>
          <ErrorLine error={s.error} />
          <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Send reset link</Button>
        </form>
      )}
    </Card>
  );
}

export function ResetPage() {
  const { token = "" } = useParams();
  const [password, setPassword] = useState("");
  const done = useSignedIn();
  const s = useSubmit(async () => done(await api.reset(token, password)));
  return (
    <Card title="Choose a new password">
      <form onSubmit={s.onSubmit} className="space-y-4">
        <Field label="New password" hint="At least 10 characters."><Input required minLength={10} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" autoFocus /></Field>
        <ErrorLine error={s.error} />
        <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Set password</Button>
      </form>
    </Card>
  );
}

export function VerifyPage() {
  const { token = "" } = useParams();
  const [state, setState] = useState<"busy" | "ok" | Error>("busy");
  useEffect(() => { api.verifyEmail(token).then(() => setState("ok"), (e) => setState(e)); }, [token]);
  return (
    <Card title="Email verification">
      {state === "busy" ? <Spinner /> : state === "ok" ? <p className="text-sm">Your email is verified. <Link className="underline" to="/">Open the dashboard</Link></p> : <ErrorLine error={state} />}
    </Card>
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
  if (err) return <Card title="Invite"><ErrorLine error={err} /></Card>;
  if (!p) return <Center><Spinner /></Center>;
  if (!p.valid) return <Card title="Invite"><p className="text-sm">{p.reason || "This invite is no longer valid."}</p></Card>;
  return (
    <Card title={`Join ${p.installName}`} sub={`You are invited as ${p.role}${p.projectName ? ` on ${p.projectName}` : ""}.`}>
      <form onSubmit={s.onSubmit} className="space-y-4">
        <Field label="Name"><Input required value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" /></Field>
        <Field label="Email"><Input required type="email" value={email} readOnly={!!p.email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" /></Field>
        <Field label="Password" hint="At least 10 characters."><Input required minLength={10} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" /></Field>
        <ErrorLine error={s.error} />
        <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Accept invite</Button>
      </form>
    </Card>
  );
}

/** /cli-auth?code=WXYZ-1234, signed in: approve the terminal's login. */
export function CliAuthPage() {
  const [params] = useSearchParams();
  const [code, setCode] = useState(params.get("code") || "");
  const [ok, setOk] = useState(false);
  const s = useSubmit(async () => { await api.cliApprove(code.trim().toUpperCase()); setOk(true); });
  return (
    <Card title="Approve CLI login">
      {ok ? <p className="text-sm">Approved. You can return to your terminal.</p> : (
        <form onSubmit={s.onSubmit} className="space-y-4">
          {params.get("code") ? <p className="text-sm">Approve login for code <span className="font-mono font-semibold">{code}</span>?</p>
            : <Field label="Code from your terminal"><Input required value={code} onChange={(e) => setCode(e.target.value)} placeholder="WXYZ-1234" /></Field>}
          <p className="text-xs text-gray-500">Only approve a code you see in your own terminal right now.</p>
          <ErrorLine error={s.error} />
          <Button type="submit" variant="primary" className="w-full" busy={s.busy}>Approve</Button>
        </form>
      )}
    </Card>
  );
}
