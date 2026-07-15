export interface ActionSheetOption {
  label: string;
  onPress?: () => void;
  style?: "default" | "cancel" | "destructive";
}

export interface ActionSheetConfig {
  title?: string;
  message?: string;
  options: ActionSheetOption[];
  onDismiss?: () => void;
}

export interface ActionSheetRuntime {
  ActionSheetIOS: {
    showActionSheetWithOptions: (
      config: {
        title?: string;
        message?: string;
        options: string[];
        cancelButtonIndex?: number;
        destructiveButtonIndex?: number;
      },
      callback: (selectedIndex: number) => void,
    ) => void;
  };
  Alert: {
    alert: (
      title: string,
      message: string | undefined,
      buttons: Array<{
        text: string;
        style?: "default" | "cancel" | "destructive";
        onPress?: () => void;
      }>,
      options?: { cancelable?: boolean; onDismiss?: () => void },
    ) => void;
  };
  Platform: { OS: string };
}

interface AndroidActionSheetPage {
  buttons: ActionSheetOption[];
}

const ANDROID_PAGE_SIZE = 2;

export function buildAndroidActionSheetPages(
  options: ActionSheetOption[],
): AndroidActionSheetPage[] {
  const actionOptions = options.filter((option) => option.style !== "cancel");
  const cancelOption = options.find((option) => option.style === "cancel");
  const pages: AndroidActionSheetPage[] = [];

  for (let index = 0; index < actionOptions.length; index += ANDROID_PAGE_SIZE) {
    const pageOptions = actionOptions.slice(index, index + ANDROID_PAGE_SIZE);
    const hasMore = index + ANDROID_PAGE_SIZE < actionOptions.length;
    const buttons = pageOptions.map((option) => ({ ...option }));

    if (hasMore) {
      buttons.push({ label: "More" });
    } else if (cancelOption) {
      buttons.push({
        label: cancelOption.label,
        onPress: cancelOption.onPress,
        style: "cancel",
      });
    }

    pages.push({ buttons });
  }

  if (pages.length === 0) {
    pages.push({
      buttons: cancelOption
        ? [
            {
              label: cancelOption.label,
              onPress: cancelOption.onPress,
              style: "cancel",
            },
          ]
        : [{ label: "OK", style: "cancel" }],
    });
  }

  return pages;
}

export function showPlatformActionSheetWithRuntime(
  runtime: ActionSheetRuntime,
  {
    title,
    message,
    options,
    onDismiss,
  }: ActionSheetConfig,
) {
  const { ActionSheetIOS, Alert, Platform } = runtime;

  if (Platform.OS === "ios") {
    const labels = options.map((option) => option.label);
    const cancelButtonIndex = options.findIndex(
      (option) => option.style === "cancel",
    );
    const destructiveButtonIndex = options.findIndex(
      (option) => option.style === "destructive",
    );

    ActionSheetIOS.showActionSheetWithOptions(
      {
        title,
        message,
        options: labels,
        ...(cancelButtonIndex >= 0 ? { cancelButtonIndex } : {}),
        ...(destructiveButtonIndex >= 0 ? { destructiveButtonIndex } : {}),
      },
      (selectedIndex) => {
        options[selectedIndex]?.onPress?.();
        onDismiss?.();
      },
    );
    return;
  }

  const pages = buildAndroidActionSheetPages(options);

  const presentPage = (pageIndex: number) => {
    const page = pages[pageIndex]!;

    const buttons = page.buttons.map((button) => {
      const isMoreButton =
        button.label === "More" && pageIndex < pages.length - 1;

      return {
        text: button.label,
        style: button.style,
        onPress: () => {
          if (isMoreButton) {
            presentPage(pageIndex + 1);
            return;
          }
          button.onPress?.();
          onDismiss?.();
        },
      };
    });

    Alert.alert(title ?? "", message, buttons, {
      cancelable: true,
      onDismiss,
    });
  };

  presentPage(0);
}

/* v8 ignore next -- the mobile Vitest lane is Node-only and cannot parse
   react-native's Flow entrypoint; the public behavior is covered through
   showPlatformActionSheetWithRuntime using injected runtimes. */
function getNativeActionSheetRuntime(): ActionSheetRuntime {
  return require("react-native");
}

export function showPlatformActionSheet({
  title,
  message,
  options,
  onDismiss,
}: ActionSheetConfig, runtime = getNativeActionSheetRuntime()) {
  showPlatformActionSheetWithRuntime(runtime, {
    title,
    message,
    options,
    onDismiss,
  });
}
