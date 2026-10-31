import {
  useId,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
} from "react";
import type { LucideIcon } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

export function PolicyLabel({
  icon: Icon,
  children,
}: {
  icon: LucideIcon;
  children: ReactNode;
}) {
  return (
    <span className="policy-field-label">
      <Icon size={15} strokeWidth={1.75} aria-hidden="true" focusable="false" />
      {children}
    </span>
  );
}

export function PolicyRetentionSlider({
  label,
  icon,
  value,
  onChange,
  disabled,
}: {
  label: string;
  icon: LucideIcon;
  value: number;
  onChange: (value: number) => void;
  disabled?: boolean;
}) {
  const id = useId();
  // Keep existing policies above the usual range editable without clamping them.
  const maximum = useRef(100);
  maximum.current = Math.max(maximum.current, value);
  const progress = ((value - 1) / (maximum.current - 1)) * 100;
  return (
    <div className="policy-retention-field">
      <div className="policy-retention-heading">
        <label htmlFor={id}>
          <PolicyLabel icon={icon}>{label}</PolicyLabel>
        </label>
        <output htmlFor={id}>{value} 份</output>
      </div>
      <input
        id={id}
        type="range"
        className="policy-retention-slider"
        min={1}
        max={maximum.current}
        step={1}
        value={value}
        aria-label={label}
        aria-valuetext={`${value} 份`}
        disabled={disabled}
        style={{ "--retention-progress": `${progress}%` } as CSSProperties}
        onChange={(event) => onChange(Number(event.target.value))}
      />
      <div className="policy-retention-scale" aria-hidden="true">
        <span>1 份</span>
        <span>{maximum.current} 份</span>
      </div>
    </div>
  );
}

export function PolicySelect({
  label,
  icon,
  value,
  options,
  onChange,
  disabled,
}: {
  label: string;
  icon: LucideIcon;
  value: string;
  options: [string, string][];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const id = useId();
  const [open, setOpen] = useState(false);
  return (
    <div className="policy-select-field">
      <label htmlFor={id}>
        <PolicyLabel icon={icon}>{label}</PolicyLabel>
      </label>
      <Select
        value={value}
        onValueChange={onChange}
        disabled={disabled}
        open={open}
        onOpenChange={setOpen}
      >
        <SelectTrigger
          id={id}
          aria-label={label}
          className="policy-select-trigger"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent
          className="policy-select-menu"
          onEscapeKeyDown={(event) => {
            // Keep the same Escape event from dismissing the surrounding dialog.
            event.preventDefault();
            event.stopPropagation();
            setOpen(false);
          }}
        >
          {options.map(([value, text]) => (
            <SelectItem key={value} value={value}>
              {text}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
