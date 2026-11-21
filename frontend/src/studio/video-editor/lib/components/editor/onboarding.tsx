"use client";

import { ArrowRightIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "../ui/button";
import { Dialog, DialogBody, DialogContent, DialogTitle } from "../ui/dialog";

// Local-storage hook mirroring opencut-classic's services/storage/use-local-storage.
// Inlined here so the editor lib doesn't drag in a host-only services tree.
function useLocalStorage<T>({
  key,
  defaultValue,
}: {
  key: string;
  defaultValue: T;
}): [
  T,
  ({ value }: { value: T | ((previousValue: T) => T) }) => void,
  boolean,
] {
  const [value, setValue] = useState<T>(defaultValue);
  const [isReady, setIsReady] = useState(false);
  const valueRef = useRef(defaultValue);

  // avoid hydration mismatch by reading after mount
  useEffect(() => {
    try {
      const storedValue = localStorage.getItem(key);
      if (storedValue !== null) {
        const parsedValue = JSON.parse(storedValue) as T;
        valueRef.current = parsedValue;
        setValue(parsedValue);
      }
    } catch {
      // localstorage might be unavailable
    }
    setIsReady(true);
  }, [key]);

  // sync to localstorage after hydration
  useEffect(() => {
    if (!isReady) return;

    try {
      localStorage.setItem(key, JSON.stringify(value));
    } catch {
      // localstorage might be full or disabled
    }
  }, [key, value, isReady]);

  const setValueWithCallback = useCallback(
    ({ value: nextValue }: { value: T | ((previousValue: T) => T) }) => {
      const resolvedValue =
        typeof nextValue === "function"
          ? (nextValue as (previousValue: T) => T)(valueRef.current)
          : nextValue;

      valueRef.current = resolvedValue;
      setValue(resolvedValue);
    },
    [],
  );

  return [value, setValueWithCallback, isReady];
}

export function Onboarding() {
  const [step, setStep] = useState(0);
  const [hasSeenOnboarding, setHasSeenOnboarding] = useLocalStorage({
    key: "hasSeenOnboarding",
    defaultValue: false,
  });

  const isOpen = !hasSeenOnboarding;

  const handleNext = () => {
    setStep(step + 1);
  };

  const handleClose = () => {
    setHasSeenOnboarding({ value: true });
  };

  const getStepTitle = () => {
    switch (step) {
      case 0:
        return "欢迎使用剪辑器";
      case 1:
        return "早期预览版 · 界面可能还有粗糙之处";
      case 2:
        return "开始创作吧！";
      default:
        return "剪辑器新手指引";
    }
  };

  const renderStepContent = () => {
    switch (step) {
      case 0:
        return (
          <div className="space-y-5">
            <div className="space-y-3">
              <Title title="欢迎使用剪辑器" />
              <Description description="把素材拖到时间线上，拖动播放头即可预览；右侧面板可以调属性、动画和特效。" />
            </div>
            <NextButton onClick={handleNext}>下一步</NextButton>
          </div>
        );
      case 1:
        return (
          <div className="space-y-5">
            <div className="space-y-3">
              <Title title={getStepTitle()} />
              <Description description="这个剪辑器仍在开发中。" />
              <Description description="很多功能还在完善，看起来怪的地方可能真的是缺陷。记得随时保存。" />
            </div>
            <NextButton onClick={handleNext}>下一步</NextButton>
          </div>
        );
      case 2:
        return (
          <div className="space-y-5">
            <div className="space-y-3">
              <Title title={getStepTitle()} />
              <Description description="快捷键在项目菜单里：点「快捷键」可以查看或重新绑定。" />
            </div>
            <NextButton onClick={handleClose}>完成</NextButton>
          </div>
        );
      default:
        return null;
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-[425px]">
        <DialogTitle>
          <span className="sr-only">{getStepTitle()}</span>
        </DialogTitle>
        <DialogBody>{renderStepContent()}</DialogBody>
      </DialogContent>
    </Dialog>
  );
}

function Title({ title }: { title: string }) {
  return <h2 className="text-lg font-bold md:text-xl">{title}</h2>;
}

function Description({ description }: { description: string }) {
  return (
    <div className="text-muted-foreground">
      <p className="mb-0">{description}</p>
    </div>
  );
}

function NextButton({
  children,
  onClick,
}: {
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <Button onClick={onClick} variant="default" className="w-full">
      {children}
      <ArrowRightIcon className="size-4" />
    </Button>
  );
}
