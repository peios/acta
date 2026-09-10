import catalogue from "../../../internal/tasks/properties.json";
export type TaskProperty = keyof typeof catalogue;
export const taskProperties: {
  value: TaskProperty;
  label: string;
  icon: string;
}[] = [
  { value: "priority", label: "Priority", icon: "M4 16v-4m5 4V8m5 8V4" },
  { value: "type", label: "Type", icon: "M3 4h8l6 6-7 7-7-7ZM7 8h.01" },
  { value: "size", label: "Size", icon: "M3 6h14v8H3ZM7 6v4m4-4v3m3-3v4" },
];
export function isTaskProperty(value: string): value is TaskProperty {
  return Object.hasOwn(catalogue, value);
}
export const propertyOptions = (name: TaskProperty) => catalogue[name];
export const propertyLabel = (name: TaskProperty, value: string | undefined) =>
  catalogue[name].find((o) => o.value === value)?.label ?? "None";
