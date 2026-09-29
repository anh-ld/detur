import { DetourProvider } from "@swmansion/react-native-detour";
import { Stack } from "expo-router";

// App identity: create app in detur portal, set .env from .env.example.
const config = {
  appID: process.env.EXPO_PUBLIC_DETOUR_APP_ID ?? "",
  apiKey: process.env.EXPO_PUBLIC_DETOUR_API_KEY ?? "",
  // deferred-only: app/+native-intent.tsx already resolves runtime/initial links; provider only runs deferred match on first launch.
  linkProcessingMode: "deferred-only",
} as const;

export default function RootLayout() {
  return (
    <DetourProvider config={config}>
      <Stack />
    </DetourProvider>
  );
}