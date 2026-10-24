// SPDX-License-Identifier: AGPL-3.0-only

// The activity panel is owned by the project shell; the palette and the top bar talk to it
// through one window event so neither imports the other.
export const ACTIVITY_EVENT = "5050:activity-toggle";
export const ACTIVITY_KEY = "5050.activity";
export const toggleActivity = () => window.dispatchEvent(new Event(ACTIVITY_EVENT));
