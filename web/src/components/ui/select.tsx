import * as React from "react";
import { Select as KumoSelect } from "@cloudflare/kumo/components/select";

type SelectRootProps = Omit<
  React.ComponentProps<typeof KumoSelect>,
  "children" | "placeholder" | "value" | "defaultValue" | "onValueChange"
>;

type SelectContextValue = {
  placeholder?: string;
  setPlaceholder: (value?: string) => void;
};

const SelectContext = React.createContext<SelectContextValue | null>(null);

export function Select({
  children,
  placeholder,
  value,
  defaultValue,
  onValueChange,
  ...props
}: SelectRootProps & {
  children?: React.ReactNode;
  placeholder?: string;
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
}) {
  const [contextPlaceholder, setPlaceholder] = React.useState<string | undefined>(placeholder);
  const options = collectOptions(children);
  const effectivePlaceholder = contextPlaceholder ?? findPlaceholder(children);
  const accessibleLabel =
    (props as { "aria-label"?: string })["aria-label"] ?? effectivePlaceholder ?? "Select";

  return (
    <SelectContext.Provider value={{ placeholder: contextPlaceholder, setPlaceholder }}>
      <KumoSelect
        placeholder={effectivePlaceholder}
        value={value}
        defaultValue={defaultValue}
        onValueChange={(next) => onValueChange?.(String(next))}
        aria-label={accessibleLabel}
        {...props}
      >
        {options}
      </KumoSelect>
    </SelectContext.Provider>
  );
}

export function SelectTrigger({ children }: { children?: React.ReactNode }) {
  return <>{children}</>;
}

export function SelectContent({ children }: { children?: React.ReactNode }) {
  return <>{children}</>;
}

export function SelectValue({ placeholder }: { placeholder?: string }) {
  const context = React.useContext(SelectContext);
  React.useEffect(() => {
    if (placeholder) {
      context?.setPlaceholder(placeholder);
    }
  }, [context, placeholder]);
  return null;
}

export function SelectItem({
  children,
  value,
  disabled,
  className
}: {
  children: React.ReactNode;
  value: unknown;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <KumoSelect.Option value={value} disabled={disabled} className={className}>
      {children}
    </KumoSelect.Option>
  );
}

export const SelectGroup = KumoSelect.Group;
export const SelectLabel = KumoSelect.GroupLabel;
export const SelectSeparator = KumoSelect.Separator;
export const SelectScrollUpButton = () => null;
export const SelectScrollDownButton = () => null;

function collectOptions(children: React.ReactNode): React.ReactNode[] {
  const options: React.ReactNode[] = [];
  React.Children.forEach(children, (child) => {
    if (!React.isValidElement(child)) {
      return;
    }
    if (child.type === SelectItem || child.type === SelectGroup) {
      options.push(child);
      return;
    }
    if (child.props && typeof child.props === "object" && "children" in child.props) {
      options.push(...collectOptions((child.props as { children?: React.ReactNode }).children));
    }
  });
  return options;
}

function findPlaceholder(children: React.ReactNode): string | undefined {
  let placeholder: string | undefined;
  React.Children.forEach(children, (child) => {
    if (placeholder || !React.isValidElement(child)) {
      return;
    }
    if (child.type === SelectValue) {
      placeholder = (child.props as { placeholder?: string }).placeholder;
      return;
    }
    if (child.props && typeof child.props === "object" && "children" in child.props) {
      placeholder = findPlaceholder((child.props as { children?: React.ReactNode }).children);
    }
  });
  return placeholder;
}
