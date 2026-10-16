import { useId } from "react";

type Props = {
  title: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
};

export default function SettingSwitch({
  title,
  description,
  checked,
  disabled,
  onChange,
}: Props & { description: string }) {
  const hint = useId();
  return (
    <div className="setting-row">
      <span>
        <strong>{title}</strong>
        <small id={hint}>{description}</small>
      </span>
      <label className="switch-control">
        <input
          className="toggle-input"
          role="switch"
          type="checkbox"
          aria-label={title}
          aria-describedby={hint}
          checked={checked}
          disabled={disabled}
          onChange={(event) => onChange(event.target.checked)}
        />
      </label>
    </div>
  );
}

export function InlineCheckbox({
  title,
  label = title,
  checked,
  disabled,
  onChange,
}: Props & { label?: string }) {
  return (
    <div className="inline-check">
      <label className="checkbox-control">
        <input
          type="checkbox"
          aria-label={label}
          checked={checked}
          disabled={disabled}
          onChange={(event) => onChange(event.target.checked)}
        />
      </label>
      <span>{title}</span>
    </div>
  );
}
