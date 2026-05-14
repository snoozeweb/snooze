export type Widget = {
  uid?: string;
  name: string;
  widget_type?: string;
  config?: Record<string, unknown>;
  comment?: string;
};
