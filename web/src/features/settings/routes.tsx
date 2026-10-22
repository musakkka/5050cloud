// SPDX-License-Identifier: AGPL-3.0-only

// Route elements owned by WP8c. The app router (src/main.tsx, WP8a) mounts:
//   <Route path="/settings/:section?" element={<InstallSettings />} />
//   <Route path="/account/:section?" element={<AccountPage />} />
//   <Route path="settings/:section?" element={<ProjectSettingsPage />} />  (inside /project/:pid)
export { default as InstallSettings } from "./InstallSettings";
export { default as AccountPage } from "../account/AccountPage";
export { default as ProjectSettingsPage } from "../project-settings/ProjectSettingsPage";
