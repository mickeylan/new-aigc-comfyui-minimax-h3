import React from "react";

/**
 * Minimal stand-ins for the ArtCraft UI packages. The canvas port only needs
 * these components' behaviour, not their look: ComfyStudio supplies the theme.
 */

export const Tooltip = ({
  children,
  content,
}: {
  children: React.ReactNode;
  content?: React.ReactNode;
  [key: string]: unknown;
}) => {
  const title = typeof content === "string" ? content : undefined;
  return <span title={title}>{children}</span>;
};

export const DynamicIcon = ({
  icon: Icon,
  ...rest
}: {
  icon: React.ComponentType<Record<string, unknown>>;
  [key: string]: unknown;
}) => <Icon {...rest} />;

export type ActionId = string;

export const formatBinding = (_binding: unknown) => "";

export const useResolvedKeybinds = () => ({
  forAction: (_action: ActionId): unknown => "",
});

export const isActionReminderOpen = {
  value: false,
  subscribe: (_listener: (value: boolean) => void) => () => {},
};

export const showActionReminder = (options: {
  title?: string;
  message?: React.ReactNode;
  onPrimaryAction?: () => void;
}) => {
  if (typeof window === "undefined") return;
  if (window.confirm(options.title || "确定要继续吗？")) {
    options.onPrimaryAction?.();
  }
};

export const Button = ({
  children,
  className,
  type = "button",
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) => (
  <button type={type} className={className} {...rest}>
    {children}
  </button>
);

export const clickOnEnterOrSpace = (event: React.KeyboardEvent) => {
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    (event.currentTarget as HTMLElement).click();
  }
};