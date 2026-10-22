// SPDX-License-Identifier: AGPL-3.0-only

import { lazy, Suspense, useEffect, type ComponentType } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { RequireAuth, SessionProvider } from "./session";
import { featureComponent } from "./routes";
import ProjectLayout from "./ProjectLayout";
import { useProject } from "./project";
import { Spinner, Toaster, applyTheme, watchSystemTheme } from "../kit";
import { CliAuthPage, ForgotPage, InvitePage, LoginPage, ResetPage, SetupPage, VerifyPage } from "../features/auth/AuthPages";
import Dashboard from "../features/dashboard/Dashboard";

const NewPage = lazy(() => import("../features/new/NewPage"));
const TemplateList = lazy(() => import("../features/templates/Templates").then((m) => ({ default: m.TemplateList })));
const TemplateDetailPage = lazy(() => import("../features/templates/Templates").then((m) => ({ default: m.TemplateDetailPage })));
const RailwayImport = lazy(() => import("../features/import/RailwayImport"));

const ServiceRoute = featureComponent<{ eid: string }>("service", "ServiceRoute", true);
const VolumeRoute = featureComponent<{ eid: string }>("service", "VolumeRoute", true);
const LogExplorer = featureComponent<{ environmentId: string }>("service", "LogExplorer");
const ObservabilityRoute = featureComponent<{ eid: string }>("service", "ObservabilityRoute");
const InstallSettings = featureComponent("settings", "InstallSettings");
const AccountPage = featureComponent("settings", "AccountPage");
const ProjectSettingsPage = featureComponent("settings", "ProjectSettingsPage");

/** Passes the open environment to a WP8b screen; waits until it is known. */
function WithEnv({ C, prop = "eid" }: { C: ComponentType<any>; prop?: string }) {
  const { eid } = useProject();
  return eid ? <C key={eid} {...{ [prop]: eid }} /> : null;
}

export default function App() {
  useEffect(() => { applyTheme(); return watchSystemTheme(); }, []);
  return (
    <BrowserRouter>
      <SessionProvider>
        <Suspense fallback={<div className="flex h-full items-center justify-center"><Spinner /></div>}>
          <Routes>
            <Route path="/setup" element={<SetupPage />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/forgot" element={<ForgotPage />} />
            <Route path="/reset/:token" element={<ResetPage />} />
            <Route path="/verify/:token" element={<VerifyPage />} />
            <Route path="/invite/:token" element={<InvitePage />} />
            <Route path="/cli-auth" element={<RequireAuth><CliAuthPage /></RequireAuth>} />
            <Route path="/" element={<RequireAuth><Dashboard /></RequireAuth>} />
            <Route path="/new" element={<RequireAuth><NewPage /></RequireAuth>} />
            <Route path="/new/template" element={<RequireAuth><TemplateList /></RequireAuth>} />
            <Route path="/new/template/:id" element={<RequireAuth><TemplateDetailPage /></RequireAuth>} />
            <Route path="/new/import/railway" element={<RequireAuth><RailwayImport /></RequireAuth>} />
            <Route path="/project/:pid" element={<RequireAuth><ProjectLayout /></RequireAuth>}>
              <Route index element={null} />
              <Route path="service/:sid/deployment/:did/:tab?" element={<WithEnv C={ServiceRoute} />} />
              <Route path="service/:sid/:tab?" element={<WithEnv C={ServiceRoute} />} />
              <Route path="volume/:vid" element={<WithEnv C={VolumeRoute} />} />
              <Route path="logs" element={<WithEnv C={LogExplorer} prop="environmentId" />} />
              <Route path="observability" element={<WithEnv C={ObservabilityRoute} />} />
              <Route path="settings/:section?" element={<ProjectSettingsPage />} />
            </Route>
            <Route path="/settings/:section?" element={<RequireAuth><InstallSettings /></RequireAuth>} />
            <Route path="/account/:section?" element={<RequireAuth><AccountPage /></RequireAuth>} />
            <Route path="*" element={<RequireAuth><Dashboard /></RequireAuth>} />
          </Routes>
        </Suspense>
        <Toaster />
      </SessionProvider>
    </BrowserRouter>
  );
}
