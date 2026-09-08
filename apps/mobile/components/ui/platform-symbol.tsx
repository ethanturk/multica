import { Ionicons } from "@expo/vector-icons";
import { Image as ExpoImage } from "expo-image";
import { Platform } from "react-native";
import type { PlatformSymbolName } from "@/lib/platform-symbols";
import { resolvePlatformSymbol } from "@/lib/platform-symbols";

type PlatformSymbolProps = {
  name: PlatformSymbolName;
  size: number;
  tintColor: string;
};

export function PlatformSymbol({
  name,
  size,
  tintColor,
}: PlatformSymbolProps) {
  if (Platform.OS === "ios") {
    return (
      <ExpoImage
        source={`sf:${name}`}
        tintColor={tintColor}
        style={{ width: size, height: size }}
      />
    );
  }

  return (
    <Ionicons
      name={resolvePlatformSymbol(name)}
      size={size}
      color={tintColor}
    />
  );
}
