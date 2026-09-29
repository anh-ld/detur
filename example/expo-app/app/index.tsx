import {
  DetourAnalytics,
  DetourEventNames,
  useDetourContext,
} from "@swmansion/react-native-detour";
import { Pressable, StyleSheet, Text, View } from "react-native";

// Home screen. Exercises patched endpoints: match-link (DetourProvider -> getDeferredLink, first launch), analytics/event + retention (DetourAnalytics buttons), resolve-short + universal-link-click (+native-intent.tsx, self-host link opens app).
export default function Home() {
  const { isLinkProcessed, link, clearLink } = useDetourContext();

  return (
    <View style={styles.container}>
      <Text style={styles.title}>detur example</Text>

      {!isLinkProcessed ? (
        <Text style={styles.muted}>Checking deferred link…</Text>
      ) : link ? (
        <View style={styles.card}>
          <Text style={styles.match}>Deferred link matched</Text>
          <Text>url: {typeof link.url === "string" ? link.url : link.url.toString()}</Text>
          <Text>route: {link.route}</Text>
          <Text>type: {link.type}</Text>
          <Pressable onPress={clearLink}>
            <Text style={styles.linkText}>Clear link</Text>
          </Pressable>
        </View>
      ) : (
        <Text style={styles.muted}>
          No match — organic install (server 404 → SDK returns null)
        </Text>
      )}

      <Pressable
        style={styles.button}
        onPress={() => DetourAnalytics.logEvent(DetourEventNames.Login, { source: "home" })}
      >
        <Text style={styles.buttonText}>Log event (analytics/event)</Text>
      </Pressable>
      <Pressable style={styles.button} onPress={() => DetourAnalytics.logRetention("day_1")}>
        <Text style={styles.buttonText}>Log retention (analytics/retention)</Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    gap: 12,
    padding: 24,
  },
  title: { fontSize: 22, fontWeight: "700" },
  card: { alignItems: "center", gap: 4 },
  match: { color: "#15803d", fontWeight: "700" },
  muted: { color: "#6b7280" },
  linkText: { color: "#2563eb", marginTop: 8 },
  button: {
    backgroundColor: "#111827",
    borderRadius: 8,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  buttonText: { color: "#ffffff", fontWeight: "600" },
});