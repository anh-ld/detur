import { createDetourNativeIntentHandler } from "@swmansion/react-native-detour/expo-router";

// Hosts app claims from OS. SDK default matcher *.godetour.link; self-host links never match, list own domain(s). dev: localhost. deploy: short-link domain, e.g. links.example.com
const hosts = (process.env.EXPO_PUBLIC_DETOUR_HOSTS ?? "localhost")
  .split(",")
  .map((host) => host.trim())
  .filter(Boolean);

// Resolve mode (config present): matched link runs universal-link-click (click report) + resolve-short (short-link expansion), maps to app route.
export const redirectSystemPath = createDetourNativeIntentHandler({
  fallbackPath: "",
  hosts,
  config: {
    apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
    appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
    timeoutMs: 1200,
  },
});