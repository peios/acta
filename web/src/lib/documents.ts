export type DocumentVersion = {
  file_id: string;
  document_id: string;
  revision: number;
  title: string;
  filename: string;
  media_type: string;
  size: number;
  sha256: string;
  created_by: string;
  created_at: string;
};
export type TaskDocument = DocumentVersion & {
  id: string;
  task_id: string;
  can_write: boolean;
};
export type DocumentPage = { documents: TaskDocument[]; cursor?: string };
export type DocumentHistory = { versions: DocumentVersion[]; before?: number };
export const documentLimit = 20 * 1024 * 1024;
export function documentSize(size: number) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
export function documentPreview(type: string, size: number) {
  const mime = type.split(";")[0];
  if (["image/png", "image/jpeg", "image/gif", "image/webp"].includes(mime))
    return "image";
  if (mime === "application/pdf") return "pdf";
  if (size <= 1024 * 1024 && mime === "text/markdown") return "markdown";
  if (size <= 1024 * 1024 && mime === "text/plain") return "text";
  return "download";
}
export function documentURL(version: DocumentVersion) {
  return `/api/documents/${encodeURIComponent(version.document_id)}/versions/${version.revision}/file`;
}
