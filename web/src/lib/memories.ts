export type Memory = {
  id: string;
  scope: string;
  scope_id: string;
  key: string;
  summary: string;
  content?: string;
  revision: number;
  can_write: boolean;
  updated_at: string;
};
