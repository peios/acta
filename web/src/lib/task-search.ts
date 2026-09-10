export type TaskSearchResult = {
  archived: boolean;
  id: string;
  reference: string;
  title: string;
  status: { id: string; name: string; board?: string };
  workspace_id: string;
  workspace_slug: string;
  workspace_name: string;
  ancestors: {
    id: string;
    reference: string;
    title: string;
  }[];
  source: string;
  comment_id?: string;
  excerpt: { text: string; match?: boolean }[];
};
export type TaskSearchPage = {
  tasks: TaskSearchResult[];
  more: boolean;
  cursor: string;
};
