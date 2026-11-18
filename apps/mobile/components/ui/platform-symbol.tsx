import { Ionicons } from "@expo/vector-icons";
import { Image as ExpoImage } from "expo-image";
import { Platform, type ColorValue } from "react-native";
import type { PlatformSymbolName } from "@/lib/platform-symbols";
import { resolvePlatformSymbol } from "@/lib/platform-symbols";

type PlatformSymbolProps = {
  name: PlatformSymbolName;
  size: number;
  tintColor: ColorValue;
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
        style={{ width: size, height: size, color: tintColor }}
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
