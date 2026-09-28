import { createDetourNativeIntentHandler } from "@swmansion/react-native-detour/expo-router";

// Hosts this app claims from the OS. SDK default matcher is *.godetour.link —
// self-host links never match, so list your own domain(s) here.
// dev: localhost. deploy: your short-link domain, e.g. links.example.com
const hosts = (process.env.EXPO_PUBLIC_DETOUR_HOSTS ?? "localhost")
  .split(",")
  .map((host) => host.trim())
  .filter(Boolean);

// Resolve mode (config present): every matched link runs
// universal-link-click (click report) + resolve-short (short-link expansion),
// then maps to an app route.
export const redirectSystemPath = createDetourNativeIntentHandler({
  fallbackPath: "",
  hosts,
  config: {
    apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
    appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
    timeoutMs: 1200,
  },
});